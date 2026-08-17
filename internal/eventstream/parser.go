package eventstream

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"hash/crc32"
	"strings"

	"github.com/ViceEye/cpa-kiro-provider/internal/jsonx"
)

type Event struct {
	Type       string
	Content    string
	ToolUseID  string
	ToolName   string
	ToolInput  string
	Usage      int64
	ContextUse float64
	Message    string
}

type Parser struct {
	buffer          []byte
	lastContent     string
	currentToolID   string
	currentToolName string
	currentToolArgs strings.Builder
}

func (p *Parser) Feed(chunk []byte) ([]Event, error) {
	p.buffer = append(p.buffer, chunk...)
	var events []Event
	for {
		if len(p.buffer) < 12 {
			break
		}
		totalLength := int(binary.BigEndian.Uint32(p.buffer[0:4]))
		headersLength := int(binary.BigEndian.Uint32(p.buffer[4:8]))
		if totalLength < 16 || totalLength > 16*1024*1024 || headersLength < 0 || headersLength > totalLength-16 {
			return events, fmt.Errorf("invalid AWS Event Stream frame lengths: total=%d headers=%d", totalLength, headersLength)
		}
		if len(p.buffer) < totalLength {
			break
		}
		frame := p.buffer[:totalLength]
		preludeCRC := binary.BigEndian.Uint32(frame[8:12])
		if crc32.ChecksumIEEE(frame[:8]) != preludeCRC {
			return events, fmt.Errorf("invalid AWS Event Stream prelude CRC")
		}
		messageCRC := binary.BigEndian.Uint32(frame[totalLength-4:])
		if crc32.ChecksumIEEE(frame[:totalLength-4]) != messageCRC {
			return events, fmt.Errorf("invalid AWS Event Stream message CRC")
		}
		headers, errHeaders := parseEventHeaders(frame[12 : 12+headersLength])
		if errHeaders != nil {
			return events, errHeaders
		}
		payload := frame[12+headersLength : totalLength-4]
		parsed, errPayload := p.parsePayload(headers, payload)
		if errPayload != nil {
			return events, errPayload
		}
		events = append(events, parsed...)
		p.buffer = p.buffer[totalLength:]
	}
	return events, nil
}

func (p *Parser) Finish() ([]Event, error) {
	if len(p.buffer) != 0 {
		return nil, fmt.Errorf("truncated AWS Event Stream frame: %d buffered bytes", len(p.buffer))
	}
	if p.currentToolID != "" || p.currentToolName != "" {
		event := Event{Type: "tool_stop", ToolUseID: p.currentToolID, ToolName: p.currentToolName, ToolInput: p.currentToolArgs.String()}
		p.currentToolID = ""
		p.currentToolName = ""
		p.currentToolArgs.Reset()
		return []Event{event}, nil
	}
	return nil, nil
}

func parseEventHeaders(raw []byte) (map[string]any, error) {
	headers := make(map[string]any)
	for offset := 0; offset < len(raw); {
		nameLength := int(raw[offset])
		offset++
		if offset+nameLength+1 > len(raw) {
			return nil, fmt.Errorf("truncated AWS Event Stream header name")
		}
		name := string(raw[offset : offset+nameLength])
		offset += nameLength
		headerType := raw[offset]
		offset++
		switch headerType {
		case 0:
			headers[name] = true
		case 1:
			headers[name] = false
		case 2:
			if offset+1 > len(raw) {
				return nil, fmt.Errorf("truncated byte header")
			}
			headers[name] = int8(raw[offset])
			offset++
		case 3:
			if offset+2 > len(raw) {
				return nil, fmt.Errorf("truncated int16 header")
			}
			headers[name] = int16(binary.BigEndian.Uint16(raw[offset:]))
			offset += 2
		case 4:
			if offset+4 > len(raw) {
				return nil, fmt.Errorf("truncated int32 header")
			}
			headers[name] = int32(binary.BigEndian.Uint32(raw[offset:]))
			offset += 4
		case 5, 8:
			if offset+8 > len(raw) {
				return nil, fmt.Errorf("truncated int64 header")
			}
			headers[name] = int64(binary.BigEndian.Uint64(raw[offset:]))
			offset += 8
		case 6, 7:
			if offset+2 > len(raw) {
				return nil, fmt.Errorf("truncated variable header")
			}
			length := int(binary.BigEndian.Uint16(raw[offset:]))
			offset += 2
			if offset+length > len(raw) {
				return nil, fmt.Errorf("truncated variable header value")
			}
			if headerType == 7 {
				headers[name] = string(raw[offset : offset+length])
			} else {
				headers[name] = append([]byte(nil), raw[offset:offset+length]...)
			}
			offset += length
		case 9:
			if offset+16 > len(raw) {
				return nil, fmt.Errorf("truncated UUID header")
			}
			headers[name] = append([]byte(nil), raw[offset:offset+16]...)
			offset += 16
		default:
			return nil, fmt.Errorf("unsupported AWS Event Stream header type %d", headerType)
		}
	}
	return headers, nil
}

func (p *Parser) parsePayload(headers map[string]any, payload []byte) ([]Event, error) {
	messageType, _ := headers[":message-type"].(string)
	if messageType == "exception" || messageType == "error" {
		var object map[string]any
		_ = json.Unmarshal(payload, &object)
		message := jsonx.String(object, "message", "Message")
		if message == "" {
			message = string(payload)
		}
		return []Event{{Type: "error", Message: message}}, nil
	}
	if len(payload) == 0 {
		return nil, nil
	}
	var object map[string]any
	if errJSON := json.Unmarshal(payload, &object); errJSON != nil {
		return nil, fmt.Errorf("decode AWS Event Stream JSON payload: %w", errJSON)
	}
	var events []Event
	if content := jsonx.Text(object, "content"); content != "" && content != p.lastContent {
		p.lastContent = content
		events = append(events, Event{Type: "content", Content: content})
	}
	if name := jsonx.String(object, "name"); name != "" {
		if p.currentToolName != "" {
			events = append(events, p.finishTool())
		}
		p.currentToolName = name
		p.currentToolID = jsonx.String(object, "toolUseId")
		if input, exists := object["input"]; exists {
			p.currentToolArgs.WriteString(jsonFragment(input))
		}
		events = append(events, Event{Type: "tool_start", ToolUseID: p.currentToolID, ToolName: name})
	}
	if input, exists := object["input"]; exists && jsonx.String(object, "name") == "" && p.currentToolName != "" {
		p.currentToolArgs.WriteString(jsonFragment(input))
		events = append(events, Event{Type: "tool_input", ToolUseID: p.currentToolID, ToolInput: jsonFragment(input)})
	}
	if stop, _ := object["stop"].(bool); stop && p.currentToolName != "" {
		events = append(events, p.finishTool())
	}
	if usage, okUsage := jsonx.Number(object["usage"]); okUsage {
		events = append(events, Event{Type: "usage", Usage: int64(usage)})
	}
	if contextUse, okContext := jsonx.Number(object["contextUsagePercentage"]); okContext {
		events = append(events, Event{Type: "context_usage", ContextUse: contextUse})
	}
	return events, nil
}

func (p *Parser) finishTool() Event {
	event := Event{Type: "tool_stop", ToolUseID: p.currentToolID, ToolName: p.currentToolName, ToolInput: p.currentToolArgs.String()}
	p.currentToolID = ""
	p.currentToolName = ""
	p.currentToolArgs.Reset()
	return event
}

func jsonFragment(value any) string {
	if text, okText := value.(string); okText {
		return text
	}
	encoded, _ := json.Marshal(value)
	if string(encoded) == "{}" {
		return ""
	}
	return string(encoded)
}
