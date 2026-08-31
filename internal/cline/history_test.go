package cline

import (
	"encoding/json"
	"testing"
)

func TestUpstreamPayloadSanitizesMalformedChatToolHistory(t *testing.T) {
	raw := []byte(`{"model":"nexus/test","messages":[
		{"role":"user","content":"continue"},
		{"role":"assistant","tool_calls":[
			{"id":"bad_call","type":"function","function":{"name":"","arguments":"{}"}},
			{"id":"good_call","type":"function","function":{"name":" exec ","arguments":"{}"}}
		]},
		{"role":"tool","tool_call_id":"bad_call","content":"ignored"},
		{"role":"tool","tool_call_id":"good_call","content":"kept"}
	],"tools":[
		{"type":"function","function":{"name":"","parameters":{}}},
		{"type":"function","function":{"name":" lookup ","parameters":{}}}
	]}`)

	got, err := upstreamPayload(raw, "nexus/test")
	if err != nil {
		t.Fatal(err)
	}

	var body map[string]any
	if err := json.Unmarshal(got, &body); err != nil {
		t.Fatal(err)
	}
	messages := body["messages"].([]any)
	if len(messages) != 3 {
		t.Fatalf("messages = %d, want 3: %s", len(messages), got)
	}
	assistant := messages[1].(map[string]any)
	calls := assistant["tool_calls"].([]any)
	if len(calls) != 1 {
		t.Fatalf("tool calls = %d, want 1: %s", len(calls), got)
	}
	call := calls[0].(map[string]any)
	if call["id"] != "good_call" || call["function"].(map[string]any)["name"] != "exec" {
		t.Fatalf("valid tool call = %#v", call)
	}
	toolMessage := messages[2].(map[string]any)
	if toolMessage["tool_call_id"] != "good_call" {
		t.Fatalf("tool message = %#v", toolMessage)
	}
	tools := body["tools"].([]any)
	if len(tools) != 1 || tools[0].(map[string]any)["function"].(map[string]any)["name"] != "lookup" {
		t.Fatalf("tools = %#v", tools)
	}
}

func TestUpstreamPayloadSanitizesResponsesToolHistory(t *testing.T) {
	raw := []byte(`{"model":"nexus/test","input":[
		{"type":"function_call","id":"bad_item","call_id":"bad_call","name":"","arguments":"{}"},
		{"type":"function_call_output","call_id":"bad_call","output":"ignored"},
		{"type":"function_call","id":"good_call","call_id":"good_call","name":"exec","arguments":"{}"},
		{"type":"function_call_output","call_id":"good_call","output":"kept"}
	]}`)

	got, err := upstreamPayload(raw, "nexus/test")
	if err != nil {
		t.Fatal(err)
	}
	var body map[string]any
	if err := json.Unmarshal(got, &body); err != nil {
		t.Fatal(err)
	}
	items := body["input"].([]any)
	if len(items) != 2 {
		t.Fatalf("input = %d, want 2: %s", len(items), got)
	}
	if items[0].(map[string]any)["call_id"] != "good_call" || items[1].(map[string]any)["call_id"] != "good_call" {
		t.Fatalf("input = %#v", items)
	}
}
