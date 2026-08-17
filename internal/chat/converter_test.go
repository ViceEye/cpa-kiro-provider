package chat

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestBuildKiroPayloadWithHistoryImageAndTools(t *testing.T) {
	raw := []byte(`{
      "model":"kiro/claude-sonnet-4-5-20250514",
      "messages":[
        {"role":"system","content":"Follow the test policy."},
        {"role":"user","content":[{"type":"text","text":"inspect image"},{"type":"image_url","image_url":{"url":"data:image/png;base64,aW1hZ2U="}}]},
        {"role":"assistant","content":"","tool_calls":[{"id":"call_1","type":"function","function":{"name":"lookup","arguments":"{\"q\":\"test\"}"}}]},
        {"role":"tool","tool_call_id":"call_1","content":"result"}
      ],
      "tools":[{"type":"function","function":{"name":"lookup","description":"Lookup data","parameters":{"type":"object","properties":{"q":{"type":"string"}}}}}]
    }`)
	payload, model, errBuild := BuildPayload(raw, "kiro/claude-sonnet-4-5-20250514", "arn:fake")
	if errBuild != nil {
		t.Fatalf("buildKiroPayload() error = %v", errBuild)
	}
	if model != "claude-sonnet-4.5" {
		t.Fatalf("model = %q", model)
	}
	var object map[string]any
	if errJSON := json.Unmarshal(payload, &object); errJSON != nil {
		t.Fatal(errJSON)
	}
	state := object["conversationState"].(map[string]any)
	history := state["history"].([]any)
	if len(history) != 2 {
		t.Fatalf("history length = %d, want 2", len(history))
	}
	first := history[0].(map[string]any)["userInputMessage"].(map[string]any)
	if first["images"] == nil {
		t.Fatalf("image missing from Kiro history: %s", payload)
	}
	if first["content"] != "Follow the test policy.\n\ninspect image" {
		t.Fatalf("system prompt not merged: %#v", first["content"])
	}
	assistant := history[1].(map[string]any)["assistantResponseMessage"].(map[string]any)
	if assistant["toolUses"] == nil {
		t.Fatalf("tool use missing: %s", payload)
	}
	toolUse := assistant["toolUses"].([]any)[0].(map[string]any)
	input := toolUse["input"].(map[string]any)
	if input["q"] != "test" {
		t.Fatalf("tool input was not decoded as JSON: %#v", toolUse["input"])
	}
	current := state["currentMessage"].(map[string]any)["userInputMessage"].(map[string]any)
	context := current["userInputMessageContext"].(map[string]any)
	if context["toolResults"] == nil || context["tools"] == nil {
		t.Fatalf("tool context incomplete: %s", payload)
	}
}

func TestNormalizeConversationRepairsRoles(t *testing.T) {
	messages := normalizeConversation([]normalizedMessage{{Role: "assistant", Text: "a"}, {Role: "user", Text: "b"}, {Role: "user", Text: "c"}})
	for index, message := range messages {
		want := "user"
		if index%2 == 1 {
			want = "assistant"
		}
		if message.Role != want {
			t.Fatalf("message %d role = %q, want %q", index, message.Role, want)
		}
	}
}

func TestNormalizeModelName(t *testing.T) {
	cases := map[string]string{"kiro/claude-haiku-4-5": "claude-haiku-4.5", "claude-3-7-sonnet-20250219": "claude-3.7-sonnet", "auto": "auto"}
	for input, want := range cases {
		if got := NormalizeModelName(input); got != want {
			t.Errorf("normalizeModelName(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestBuildKiroPayloadTrimsOversizedHistory(t *testing.T) {
	request := chatRequest{Model: "kiro/claude-opus-4.5"}
	system, _ := json.Marshal("keep-system-policy")
	request.Messages = append(request.Messages, chatMessage{Role: "system", Content: system})
	for index := 0; index < 180; index++ {
		role := "user"
		if index%2 == 1 {
			role = "assistant"
		}
		content, _ := json.Marshal(fmt.Sprintf("message-%d-%s", index, strings.Repeat("x", 7000)))
		request.Messages = append(request.Messages, chatMessage{Role: role, Content: content})
	}
	current, _ := json.Marshal("keep-current-message")
	request.Messages = append(request.Messages, chatMessage{Role: "user", Content: current})
	raw, _ := json.Marshal(request)

	payload, _, errBuild := BuildPayload(raw, request.Model, "arn:fake")
	if errBuild != nil {
		t.Fatalf("buildKiroPayload() error = %v", errBuild)
	}
	if len(payload) > maxKiroPayloadBytes {
		t.Fatalf("payload size = %d, limit = %d", len(payload), maxKiroPayloadBytes)
	}
	var object map[string]any
	if errJSON := json.Unmarshal(payload, &object); errJSON != nil {
		t.Fatal(errJSON)
	}
	state := object["conversationState"].(map[string]any)
	history := state["history"].([]any)
	if len(history) >= len(request.Messages)-2 || len(history)%2 != 0 {
		t.Fatalf("trimmed history length = %d", len(history))
	}
	first := history[0].(map[string]any)["userInputMessage"].(map[string]any)
	if !strings.HasPrefix(first["content"].(string), "keep-system-policy\n\n") {
		t.Fatalf("system prompt was not preserved after trimming: %#v", first["content"])
	}
	currentMessage := state["currentMessage"].(map[string]any)["userInputMessage"].(map[string]any)
	if currentMessage["content"] != "keep-current-message" {
		t.Fatalf("current message was not preserved: %#v", currentMessage["content"])
	}
}

func TestMarshalKiroPayloadDoesNotLeaveLeadingToolResult(t *testing.T) {
	history := []any{
		map[string]any{"userInputMessage": map[string]any{"content": strings.Repeat("x", 350000)}},
		map[string]any{"assistantResponseMessage": map[string]any{"content": "", "toolUses": []any{map[string]any{"toolUseId": "call-1", "name": "tool", "input": map[string]any{}}}}},
		map[string]any{"userInputMessage": map[string]any{"content": "result", "userInputMessageContext": map[string]any{"toolResults": []map[string]any{{"toolUseId": "call-1"}}}}},
		map[string]any{"assistantResponseMessage": map[string]any{"content": "done"}},
	}
	payload := map[string]any{"conversationState": map[string]any{
		"history": history,
		"currentMessage": map[string]any{"userInputMessage": map[string]any{
			"content": strings.Repeat("y", 300000), "modelId": "fixture", "origin": "AI_EDITOR",
		}},
	}}
	encoded, errMarshal := marshalKiroPayload(payload, "keep-system-policy")
	if errMarshal != nil {
		t.Fatalf("marshalKiroPayload() error = %v", errMarshal)
	}
	if len(encoded) > maxKiroPayloadBytes {
		t.Fatalf("payload size = %d, limit = %d", len(encoded), maxKiroPayloadBytes)
	}
	state := payload["conversationState"].(map[string]any)
	if remaining, exists := state["history"]; exists && historyStartsWithToolResult(remaining.([]any)) {
		t.Fatalf("trimmed history starts with an orphaned tool result: %#v", remaining)
	}
	current := state["currentMessage"].(map[string]any)["userInputMessage"].(map[string]any)
	if !strings.HasPrefix(current["content"].(string), "keep-system-policy\n\n") {
		t.Fatalf("system prompt was not moved to current message: %#v", current["content"])
	}
}
