package eventstream

import (
	"encoding/binary"
	"encoding/json"
	"hash/crc32"
	"strings"
	"testing"
)

func TestEventStreamFragmentationToolsAndUsage(t *testing.T) {
	stream := append(eventFrame(t, map[string]any{"content": "hello "}), eventFrame(t, map[string]any{"content": "world"})...)
	stream = append(stream, eventFrame(t, map[string]any{"name": "lookup", "toolUseId": "call_1", "input": map[string]any{}})...)
	stream = append(stream, eventFrame(t, map[string]any{"input": "{\"q\":"})...)
	stream = append(stream, eventFrame(t, map[string]any{"input": "\"test\"}"})...)
	stream = append(stream, eventFrame(t, map[string]any{"stop": true})...)
	stream = append(stream, eventFrame(t, map[string]any{"usage": 3, "contextUsagePercentage": 12.5})...)
	parser := &Parser{}
	var events []Event
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
	var content strings.Builder
	var toolInput string
	var usage int64
	var contextUse float64
	for _, event := range events {
		switch event.Type {
		case "content":
			content.WriteString(event.Content)
		case "tool_stop":
			toolInput = event.ToolInput
		case "usage":
			usage = event.Usage
		case "context_usage":
			contextUse = event.ContextUse
		}
	}
	if content.String() != "hello world" || usage != 3 || contextUse != 12.5 {
		t.Fatalf("content=%q usage=%d context=%v", content.String(), usage, contextUse)
	}
	if toolInput != `{"q":"test"}` {
		t.Fatalf("tool arguments = %q", toolInput)
	}
}

func TestEventStreamRepeatedToolMetadataStaysOneCall(t *testing.T) {
	stream := append(eventFrame(t, map[string]any{"name": "shell", "toolUseId": "call_1", "input": ""}), eventFrame(t, map[string]any{"name": "shell", "toolUseId": "call_1", "input": `{"cm`})...)
	stream = append(stream, eventFrame(t, map[string]any{"name": "shell", "toolUseId": "call_1", "input": `d":"echo ok"}`, "stop": true})...)
	parser := &Parser{}
	events, errFeed := parser.Feed(stream)
	if errFeed != nil {
		t.Fatal(errFeed)
	}
	starts := 0
	var input string
	for _, event := range events {
		switch event.Type {
		case "tool_start":
			starts++
		case "tool_input":
			input += event.ToolInput
		}
	}
	if starts != 1 || input != `{"cmd":"echo ok"}` {
		t.Fatalf("tool starts=%d input=%q events=%#v", starts, input, events)
	}
}

func TestEventStreamObjectInputReplacesInsteadOfConcatenating(t *testing.T) {
	stream := eventFrame(t, map[string]any{"name": "shell", "toolUseId": "c1", "input": map[string]any{"cmd": "echo"}})
	stream = append(stream, eventFrame(t, map[string]any{"toolUseId": "c1", "input": map[string]any{"cmd": "echo ok"}, "stop": true})...)
	parser := &Parser{}
	events, errFeed := parser.Feed(stream)
	if errFeed != nil {
		t.Fatal(errFeed)
	}
	var stop Event
	inputDeltas := 0
	for _, event := range events {
		switch event.Type {
		case "tool_input":
			inputDeltas++
		case "tool_stop":
			stop = event
		}
	}
	if stop.ToolInput != `{"cmd":"echo ok"}` {
		t.Fatalf("tool_stop input = %q, want the last whole object", stop.ToolInput)
	}
	if !stop.ReplacesInput {
		t.Fatalf("tool_stop should be marked as replacing input: %#v", stop)
	}
	if inputDeltas != 0 {
		t.Fatalf("object input must not emit deltas, got %d", inputDeltas)
	}
}

func TestEventStreamStringInputStillConcatenates(t *testing.T) {
	stream := eventFrame(t, map[string]any{"name": "shell", "toolUseId": "c1", "input": `{"cm`})
	stream = append(stream, eventFrame(t, map[string]any{"toolUseId": "c1", "input": `d":"echo ok"}`, "stop": true})...)
	parser := &Parser{}
	events, errFeed := parser.Feed(stream)
	if errFeed != nil {
		t.Fatal(errFeed)
	}
	for _, event := range events {
		if event.Type != "tool_stop" {
			continue
		}
		if event.ToolInput != `{"cmd":"echo ok"}` {
			t.Fatalf("tool_stop input = %q", event.ToolInput)
		}
		if event.ReplacesInput {
			t.Fatalf("streamed string input must not be marked as replacing: %#v", event)
		}
	}
}

func TestEventStreamRejectsCorruptAndTruncatedFrames(t *testing.T) {
	frame := eventFrame(t, map[string]any{"content": "hello"})
	corrupt := append([]byte(nil), frame...)
	corrupt[len(corrupt)-5] ^= 0xff
	if _, errFeed := (&Parser{}).Feed(corrupt); errFeed == nil {
		t.Fatal("corrupt CRC was accepted")
	}
	parser := &Parser{}
	if _, errFeed := parser.Feed(frame[:len(frame)-1]); errFeed != nil {
		t.Fatalf("partial frame rejected too early: %v", errFeed)
	}
	if _, errFinish := parser.Finish(); errFinish == nil {
		t.Fatal("truncated frame was accepted")
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
