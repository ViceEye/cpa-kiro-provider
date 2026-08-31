package cline

import (
	"bytes"
	"encoding/json"
	"fmt"
)

const (
	defaultSSEMaxLineBytes  = 16 << 20
	defaultSSEMaxEventBytes = 16 << 20
)

type sseRecord struct {
	Event string
	ID    string
	Data  []byte
}

// sseRecordParser implements the wire-level SSE framing rules. Cline also has
// a deployed line-delimited JSON variant, so complete JSON data lines are
// dispatched immediately when no explicit event field is active.
type sseRecordParser struct {
	pending        []byte
	data           []byte
	hasData        bool
	event          string
	lastID         string
	firstLine      bool
	finished       bool
	skipLF         bool
	maxLineBytes   int
	maxEventBytes  int
	allowJSONLines bool
}

func newSSERecordParser() *sseRecordParser {
	return newSSERecordParserWithLimit(defaultSSEMaxEventBytes)
}

func newSSERecordParserWithLimit(limit int) *sseRecordParser {
	if limit <= 0 {
		limit = defaultSSEMaxEventBytes
	}
	return &sseRecordParser{
		firstLine:      true,
		maxLineBytes:   defaultSSEMaxLineBytes,
		maxEventBytes:  limit,
		allowJSONLines: true,
	}
}

func (p *sseRecordParser) Feed(payload []byte) ([]sseRecord, error) {
	if p == nil || len(payload) == 0 {
		return nil, nil
	}
	if p.finished {
		return nil, fmt.Errorf("SSE parser already finished")
	}
	p.pending = append(p.pending, payload...)
	if p.skipLF {
		if len(p.pending) > 0 && p.pending[0] == '\n' {
			p.pending = p.pending[1:]
		}
		p.skipLF = false
	}
	var out []sseRecord
	for {
		index, width := sseLineEnd(p.pending)
		if index < 0 {
			if len(p.pending) > 0 && p.pending[len(p.pending)-1] == '\r' {
				index = len(p.pending) - 1
				width = 1
				p.skipLF = true
			} else {
				if len(p.pending) > p.maxLineBytes {
					return out, fmt.Errorf("SSE line exceeds %d bytes", p.maxLineBytes)
				}
				break
			}
		}
		line := bytes.Clone(p.pending[:index])
		p.pending = p.pending[index+width:]
		records, err := p.processLine(line)
		if err != nil {
			return out, err
		}
		out = append(out, records...)
	}
	return out, nil
}

func (p *sseRecordParser) Finish() ([]sseRecord, error) {
	if p == nil || p.finished {
		return nil, nil
	}
	p.finished = true
	var out []sseRecord
	// Compatibility mode may receive a final JSON line without a trailing LF.
	// Incomplete standard SSE records remain buffered and are discarded.
	if len(p.pending) > 0 && p.allowJSONLines {
		line := bytes.Clone(p.pending)
		p.pending = nil
		records, err := p.processLine(line)
		if err != nil {
			return nil, err
		}
		out = append(out, records...)
	}
	p.pending = nil
	p.skipLF = false
	p.data = nil
	p.hasData = false
	p.event = ""
	return out, nil
}

func (p *sseRecordParser) processLine(line []byte) ([]sseRecord, error) {
	if len(line) > p.maxLineBytes {
		return nil, fmt.Errorf("SSE line exceeds %d bytes", p.maxLineBytes)
	}
	if p.firstLine {
		p.firstLine = false
		line = bytes.TrimPrefix(line, []byte{0xef, 0xbb, 0xbf})
	}
	if len(line) == 0 {
		return p.dispatch(), nil
	}
	if line[0] == ':' {
		return nil, nil
	}

	field, value := splitSSEField(line)
	switch string(field) {
	case "event":
		p.event = string(value)
	case "data":
		if err := p.appendData(value); err != nil {
			return nil, err
		}
		if p.allowJSONLines && p.event == "" && isCompleteSSEData(p.data) {
			return p.dispatch(), nil
		}
	case "id":
		if !bytes.Contains(value, []byte{0}) {
			p.lastID = string(value)
		}
	case "retry":
		// Retry is meaningful to reconnecting clients, not to this one-shot relay.
	}
	return nil, nil
}

func (p *sseRecordParser) appendData(value []byte) error {
	nextSize := len(p.data) + len(value)
	if p.hasData {
		nextSize++
	}
	if nextSize > p.maxEventBytes {
		return fmt.Errorf("SSE event data exceeds %d bytes", p.maxEventBytes)
	}
	if p.hasData {
		p.data = append(p.data, '\n')
	}
	p.data = append(p.data, value...)
	p.hasData = true
	return nil
}

func (p *sseRecordParser) dispatch() []sseRecord {
	if !p.hasData {
		p.event = ""
		return nil
	}
	record := sseRecord{
		Event: p.event,
		ID:    p.lastID,
		Data:  bytes.Clone(p.data),
	}
	p.data = nil
	p.hasData = false
	p.event = ""
	return []sseRecord{record}
}

func splitSSEField(line []byte) ([]byte, []byte) {
	index := bytes.IndexByte(line, ':')
	if index < 0 {
		return line, nil
	}
	value := line[index+1:]
	if len(value) > 0 && value[0] == ' ' {
		value = value[1:]
	}
	return line[:index], value
}

func isCompleteSSEData(data []byte) bool {
	trimmed := bytes.TrimSpace(data)
	if bytes.Equal(trimmed, []byte("[DONE]")) {
		return true
	}
	if len(trimmed) == 0 || (trimmed[0] != '{' && trimmed[0] != '[') {
		return false
	}
	return json.Valid(trimmed)
}

func sseLineEnd(buffer []byte) (index, width int) {
	for index, value := range buffer {
		switch value {
		case '\n':
			return index, 1
		case '\r':
			if index+1 == len(buffer) {
				return -1, 0
			}
			if buffer[index+1] == '\n' {
				return index, 2
			}
			return index, 1
		}
	}
	return -1, 0
}
