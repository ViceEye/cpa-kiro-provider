package cline

import (
	"strings"
	"testing"
)

func TestSSEParserAggregatesMultilineDataAndFields(t *testing.T) {
	parser := newSSERecordParser()
	records, err := parser.Feed([]byte("id: 42\r\nevent: message\r\ndata: {\"a\":\r\ndata: 1}\r\n\r\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 {
		t.Fatalf("records = %d, want 1", len(records))
	}
	if records[0].Event != "message" || records[0].ID != "42" || string(records[0].Data) != "{\"a\":\n1}" {
		t.Fatalf("record = %#v", records[0])
	}
}

func TestSSEParserHandlesSplitCRLFAndUTF8Bytes(t *testing.T) {
	parser := newSSERecordParser()
	encoded := []byte("data: \"成\"\r")
	cut := strings.Index(string(encoded), "成") + 1
	first, err := parser.Feed(encoded[:cut])
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 0 {
		t.Fatalf("records before CRLF = %d, want 0", len(first))
	}
	second, err := parser.Feed(append(encoded[cut:], []byte("\n\r\n")...))
	if err != nil {
		t.Fatal(err)
	}
	if len(second) != 1 || string(second[0].Data) != `"成"` {
		t.Fatalf("records = %#v", second)
	}
}

func TestSSEParserHandlesBOMAndCROnlyLines(t *testing.T) {
	parser := newSSERecordParser()
	payload := append([]byte{0xef, 0xbb, 0xbf}, []byte("event: message\rdata: {}\r\r")...)
	records, err := parser.Feed(payload)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || records[0].Event != "message" || string(records[0].Data) != "{}" {
		t.Fatalf("records = %#v", records)
	}
}

func TestSSEParserRejectsFeedAfterFinish(t *testing.T) {
	parser := newSSERecordParser()
	if _, err := parser.Finish(); err != nil {
		t.Fatal(err)
	}
	if _, err := parser.Feed([]byte("data: {}\n")); err == nil {
		t.Fatal("expected feed-after-finish error")
	}
}

func TestSSEParserSupportsClineJSONLinesWithoutBlankSeparator(t *testing.T) {
	parser := newSSERecordParser()
	records, err := parser.Feed([]byte("data: {\"one\":1}\ndata: [DONE]\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 2 || string(records[0].Data) != `{"one":1}` || string(records[1].Data) != "[DONE]" {
		t.Fatalf("records = %#v", records)
	}
}

func TestSSEParserDiscardsIncompleteStandardRecordAtEOF(t *testing.T) {
	parser := newSSERecordParser()
	if _, err := parser.Feed([]byte("event: message\ndata: {\"partial\":")); err != nil {
		t.Fatal(err)
	}
	records, err := parser.Finish()
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 0 {
		t.Fatalf("records at EOF = %#v, want none", records)
	}
}

func TestSSEParserEnforcesEventDataLimit(t *testing.T) {
	parser := newSSERecordParserWithLimit(8)
	parser.allowJSONLines = false
	_, err := parser.Feed([]byte("data: 1234\n" + "data: 5678\n\n"))
	if err == nil || !strings.Contains(err.Error(), "event data exceeds 8 bytes") {
		t.Fatalf("error = %v", err)
	}
}
