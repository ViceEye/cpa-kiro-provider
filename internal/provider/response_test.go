package provider

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"hash/crc32"
	"testing"

	"github.com/ViceEye/cpa-provider-nexus/internal/eventstream"
)

func TestCompletionStreamFramesAreCanonicalJSON(t *testing.T) {
	acc := newCompletionAccumulator("fixture-model")
	frames := acc.streamFrames(eventstream.Event{Type: "content", Content: "hello"})
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
	if finish := acc.finishFrame(); bytes.HasPrefix(finish, []byte("data:")) || !json.Valid(finish) {
		t.Fatalf("finish frame is not canonical JSON: %q", finish)
	}
}

func TestConvertNonStreamResponse(t *testing.T) {
	raw := append(responseEventFrame(t, map[string]any{"content": "answer"}), responseEventFrame(t, map[string]any{"usage": 2})...)
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

func TestConvertNonStreamResponseMergesRepeatedToolMetadata(t *testing.T) {
	raw := append(responseEventFrame(t, map[string]any{"name": "shell", "toolUseId": "call_1", "input": ""}), responseEventFrame(t, map[string]any{"name": "shell", "toolUseId": "call_1", "input": `{"cm`})...)
	raw = append(raw, responseEventFrame(t, map[string]any{"name": "shell", "toolUseId": "call_1", "input": `d":"echo ok"}`, "stop": true})...)
	response, errConvert := convertNonStreamResponse(raw, "claude-sonnet-5")
	if errConvert != nil {
		t.Fatal(errConvert)
	}
	var object map[string]any
	if errJSON := json.Unmarshal(response, &object); errJSON != nil {
		t.Fatal(errJSON)
	}
	message := object["choices"].([]any)[0].(map[string]any)["message"].(map[string]any)
	calls := message["tool_calls"].([]any)
	if len(calls) != 1 {
		t.Fatalf("tool call count = %d, response=%s", len(calls), response)
	}
	function := calls[0].(map[string]any)["function"].(map[string]any)
	if function["arguments"] != `{"cmd":"echo ok"}` {
		t.Fatalf("tool arguments = %#v", function["arguments"])
	}
}

func responseEventFrame(t *testing.T, payload any) []byte {
	t.Helper()
	body, errJSON := json.Marshal(payload)
	if errJSON != nil {
		t.Fatal(errJSON)
	}
	headers := responseEventHeader(":message-type", "event")
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

func responseEventHeader(name, value string) []byte {
	header := append([]byte{byte(len(name))}, name...)
	header = append(header, 7, byte(len(value)>>8), byte(len(value)))
	return append(header, value...)
}
