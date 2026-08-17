package main

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"hash/crc32"
	"testing"
)

func TestCompletionStreamFramesAreCanonicalJSON(t *testing.T) {
	acc := newCompletionAccumulator("fixture-model")
	frames := acc.streamFrames(kiroEvent{Type: "content", Content: "hello"})
	if len(frames) != 1 {
		t.Fatalf("stream frame count = %d", len(frames))
	}
	if bytes.HasPrefix(frames[0], []byte("data:")) || bytes.Contains(frames[0], []byte("[DONE]")) {
		t.Fatalf("plugin emitted HTTP SSE framing: %q", frames[0])
	}
	var object map[string]any
	if err := json.Unmarshal(frames[0], &object); err != nil {
		t.Fatalf("stream frame is not JSON: %v", err)
	}
	if object["object"] != "chat.completion.chunk" {
		t.Fatalf("stream object = %#v", object["object"])
	}
	finish := acc.finishFrame()
	if bytes.HasPrefix(finish, []byte("data:")) || !json.Valid(finish) {
		t.Fatalf("finish frame is not canonical JSON: %q", finish)
	}
}

func TestEventStreamFragmentationToolsAndUsage(t *testing.T) {
	stream := append(eventFrame(t, map[string]any{"content": "hello "}), eventFrame(t, map[string]any{"content": "world"})...)
	stream = append(stream, eventFrame(t, map[string]any{"name": "lookup", "toolUseId": "call_1", "input": map[string]any{}})...)
	stream = append(stream, eventFrame(t, map[string]any{"input": "{\"q\":"})...)
	stream = append(stream, eventFrame(t, map[string]any{"input": "\"test\"}"})...)
	stream = append(stream, eventFrame(t, map[string]any{"stop": true})...)
	stream = append(stream, eventFrame(t, map[string]any{"usage": 3, "contextUsagePercentage": 12.5})...)
	parser := &eventStreamParser{}
	var events []kiroEvent
	for offset := 0; offset < len(stream); {
		size := 1 + offset%11
		if offset+size > len(stream) {
			size = len(stream) - offset
		}
		parsed, errFeed := parser.Feed(stream[offset : offset+size])
		if errFeed != nil {
			t.Fatalf("Feed() at %d: %v", offset, errFeed)
		}
		events = append(events, parsed...)
		offset += size
	}
	tail, errFinish := parser.Finish()
	if errFinish != nil {
		t.Fatal(errFinish)
	}
	events = append(events, tail...)
	acc := newCompletionAccumulator("claude-sonnet-4.5")
	for _, event := range events {
		acc.apply(event)
	}
	if acc.Content.String() != "hello world" || len(acc.ToolCalls) != 1 || acc.Usage != 3 || acc.ContextUse != 12.5 {
		t.Fatalf("accumulator = %#v content=%q", acc, acc.Content.String())
	}
	function := acc.ToolCalls[0]["function"].(map[string]any)
	if function["arguments"] != `{"q":"test"}` {
		t.Fatalf("tool arguments = %#v", function["arguments"])
	}
}

func TestEventStreamRejectsCorruptAndTruncatedFrames(t *testing.T) {
	frame := eventFrame(t, map[string]any{"content": "hello"})
	corrupt := append([]byte(nil), frame...)
	corrupt[len(corrupt)-5] ^= 0xff
	if _, errFeed := (&eventStreamParser{}).Feed(corrupt); errFeed == nil {
		t.Fatal("corrupt CRC was accepted")
	}
	parser := &eventStreamParser{}
	if _, errFeed := parser.Feed(frame[:len(frame)-1]); errFeed != nil {
		t.Fatalf("partial frame rejected too early: %v", errFeed)
	}
	if _, errFinish := parser.Finish(); errFinish == nil {
		t.Fatal("truncated frame was accepted")
	}
}

func TestConvertNonStreamResponse(t *testing.T) {
	raw := append(eventFrame(t, map[string]any{"content": "answer"}), eventFrame(t, map[string]any{"usage": 2})...)
	response, errConvert := convertNonStreamResponse(raw, "claude-sonnet-4.5")
	if errConvert != nil {
		t.Fatalf("convertNonStreamResponse() error = %v", errConvert)
	}
	var object map[string]any
	if json.Unmarshal(response, &object) != nil {
		t.Fatalf("invalid JSON response: %s", response)
	}
	choices := object["choices"].([]any)
	message := choices[0].(map[string]any)["message"].(map[string]any)
	if message["content"] != "answer" {
		t.Fatalf("content = %#v", message["content"])
	}
}

func eventFrame(t *testing.T, payload any) []byte {
	t.Helper()
	body, errJSON := json.Marshal(payload)
	if errJSON != nil {
		t.Fatal(errJSON)
	}
	headers := append(eventStringHeader(":message-type", "event"), eventStringHeader(":event-type", "assistantResponseEvent")...)
	total := 16 + len(headers) + len(body)
	frame := make([]byte, total)
	binary.BigEndian.PutUint32(frame[0:4], uint32(total))
	binary.BigEndian.PutUint32(frame[4:8], uint32(len(headers)))
	binary.BigEndian.PutUint32(frame[8:12], crc32.ChecksumIEEE(frame[:8]))
	copy(frame[12:], headers)
	copy(frame[12+len(headers):], body)
	binary.BigEndian.PutUint32(frame[total-4:], crc32.ChecksumIEEE(frame[:total-4]))
	return frame
}

func eventStringHeader(name, value string) []byte {
	header := []byte{byte(len(name))}
	header = append(header, []byte(name)...)
	header = append(header, 7, byte(len(value)>>8), byte(len(value)))
	header = append(header, []byte(value)...)
	return header
}
