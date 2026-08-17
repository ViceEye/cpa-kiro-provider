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
