package cline

import (
	"encoding/json"
	"strings"
	"testing"
)

func decodeChatFrame(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	line := strings.TrimSpace(string(raw))
	if !strings.HasPrefix(line, "data:") {
		t.Fatalf("frame = %q, want data line", line)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(strings.TrimPrefix(line, "data:"))), &payload); err != nil {
		t.Fatalf("decode frame %q: %v", line, err)
	}
	return payload
}

func TestResponsesToChatSSETextAndUsage(t *testing.T) {
	relay := newSSERelay("nexus/z-ai/glm-5.3-flash")
	frames, err := relay.Feed([]byte("data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_text\",\"created_at\":123,\"model\":\"z-ai/glm-5.3-flash\"}}\n" +
		"data: {\"type\":\"response.output_text.delta\",\"delta\":\"成\"}\n" +
		"data: {\"type\":\"response.output_text.delta\",\"delta\":\"功\"}\n" +
		"data: {\"type\":\"response.completed\",\"response\":{\"usage\":{\"input_tokens\":10,\"output_tokens\":2,\"total_tokens\":12}}}\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(frames) != 3 {
		t.Fatalf("frames = %d, want text + text + finish", len(frames))
	}

	first := decodeChatFrame(t, frames[0])
	if got := first["model"]; got != "nexus/z-ai/glm-5.3-flash" {
		t.Fatalf("model = %v", got)
	}
	choices := first["choices"].([]any)
	delta := choices[0].(map[string]any)["delta"].(map[string]any)
	if delta["content"] != "成" {
		t.Fatalf("first content = %v", delta["content"])
	}

	last := decodeChatFrame(t, frames[2])
	lastChoice := last["choices"].([]any)[0].(map[string]any)
	if lastChoice["finish_reason"] != "stop" {
		t.Fatalf("finish_reason = %v", lastChoice["finish_reason"])
	}
	usage := last["usage"].(map[string]any)
	if usage["prompt_tokens"] != float64(10) || usage["completion_tokens"] != float64(2) {
		t.Fatalf("usage = %#v", usage)
	}
}

func TestResponsesToChatSSECustomToolBuffersInput(t *testing.T) {
	relay := newSSERelay("nexus/z-ai/glm-5.3-flash")
	first, err := relay.Feed([]byte("data: {\"type\":\"response.output_item.added\",\"output_index\":1,\"item\":{\"id\":\"ctc_1\",\"type\":\"custom_tool_call\",\"call_id\":\"call_exec\",\"name\":\"exec\"}}\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 1 {
		t.Fatalf("initial tool frames = %d, want 1", len(first))
	}

	partial := []byte("data: {\"type\":\"response.custom_tool_call_input.delta\",\"item_id\":\"ctc_1\",\"delta\":\"return ")
	frames, err := relay.Feed(partial)
	if err != nil {
		t.Fatal(err)
	}
	if len(frames) != 0 {
		t.Fatalf("partial line frames = %d, want 0", len(frames))
	}
	frames, err = relay.Feed([]byte("1;\"}\n" +
		"data: {\"type\":\"response.custom_tool_call_input.done\",\"item_id\":\"ctc_1\",\"input\":\"return 1;\"}\n" +
		"data: {\"type\":\"response.output_item.done\",\"output_index\":1,\"item\":{\"id\":\"ctc_1\",\"type\":\"custom_tool_call\",\"call_id\":\"call_exec\",\"name\":\"exec\",\"input\":\"return 1;\"}}\n" +
		"data: {\"type\":\"response.completed\",\"response\":{}}\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(frames) != 2 {
		t.Fatalf("completed tool frames = %d, want arguments + finish", len(frames))
	}

	toolFrame := decodeChatFrame(t, frames[0])
	toolCall := toolFrame["choices"].([]any)[0].(map[string]any)["delta"].(map[string]any)["tool_calls"].([]any)[0].(map[string]any)
	function := toolCall["function"].(map[string]any)
	if function["arguments"] != `{"input":"return 1;"}` {
		t.Fatalf("custom tool arguments = %v", function["arguments"])
	}

	finish := decodeChatFrame(t, frames[1])
	if finish["choices"].([]any)[0].(map[string]any)["finish_reason"] != "tool_calls" {
		t.Fatalf("tool finish = %#v", finish)
	}
}

func TestClineSSERelayBuffersSplitUTF8AndJSON(t *testing.T) {
	relay := newSSERelay("nexus/test")
	payload := []byte("data: {\"data\":{\"choices\":[{\"delta\":{\"content\":\"成\"}}]}}\n")
	marker := []byte("成")
	cut := strings.Index(string(payload), string(marker)) + 1
	first, err := relay.Feed(payload[:cut])
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 0 {
		t.Fatalf("split UTF-8 prefix frames = %d, want 0", len(first))
	}
	second, err := relay.Feed(payload[cut:])
	if err != nil {
		t.Fatal(err)
	}
	if len(second) != 1 {
		t.Fatalf("split UTF-8 suffix frames = %d, want 1", len(second))
	}
	choice := decodeChatFrame(t, second[0])["choices"].([]any)[0].(map[string]any)
	if choice["delta"].(map[string]any)["content"] != "成" {
		t.Fatalf("content = %#v", choice["delta"])
	}
}

func TestNormalizeClineResponseConvertsResponsesBody(t *testing.T) {
	raw := []byte(`{"data":{"id":"resp_body","object":"response","model":"z-ai/glm-5.3-flash","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"完成"}]},{"type":"custom_tool_call","call_id":"call_exec","name":"exec","input":"return 1;"}],"usage":{"input_tokens":10,"output_tokens":4,"total_tokens":14}}}`)
	got, err := normalizeClineResponse(raw, "nexus/z-ai/glm-5.3-flash")
	if err != nil {
		t.Fatal(err)
	}
	var response map[string]any
	if err := json.Unmarshal(got, &response); err != nil {
		t.Fatal(err)
	}
	if response["object"] != "chat.completion" {
		t.Fatalf("object = %v", response["object"])
	}
	message := response["choices"].([]any)[0].(map[string]any)["message"].(map[string]any)
	if message["content"] != nil || message["role"] != "assistant" {
		t.Fatalf("message = %#v", message)
	}
	toolCalls := message["tool_calls"].([]any)
	arguments := toolCalls[0].(map[string]any)["function"].(map[string]any)["arguments"].(string)
	if arguments != `{"input":"return 1;"}` {
		t.Fatalf("arguments = %s", arguments)
	}
}

func TestResponsesToChatSSEDefersEmptyToolNameUntilDone(t *testing.T) {
	relay := newSSERelay("nexus/test")
	feed := func(raw string) [][]byte {
		frames, err := relay.Feed([]byte("data: " + raw + "\n"))
		if err != nil {
			t.Fatal(err)
		}
		return frames
	}

	frames := feed(`{"type":"response.output_item.added","output_index":1,"item":{"id":"fc_1","type":"function_call","call_id":"call_1","name":""}}`)
	if len(frames) != 0 {
		t.Fatalf("empty-name start frames = %d, want 0", len(frames))
	}
	frames = feed(`{"type":"response.function_call_arguments.delta","output_index":1,"item_id":"fc_1","delta":"{\"x\":1}"}`)
	if len(frames) != 0 {
		t.Fatalf("buffered argument frames = %d, want 0", len(frames))
	}
	frames = feed(`{"type":"response.output_item.done","output_index":1,"item":{"id":"fc_1","type":"function_call","call_id":"call_1","name":"exec","arguments":"{\"x\":1}"}}`)
	if len(frames) != 2 {
		t.Fatalf("late-name frames = %d, want initial + arguments", len(frames))
	}
	initial := decodeChatFrame(t, frames[0])
	toolCall := initial["choices"].([]any)[0].(map[string]any)["delta"].(map[string]any)["tool_calls"].([]any)[0].(map[string]any)
	if got := toolCall["function"].(map[string]any)["name"]; got != "exec" {
		t.Fatalf("tool name = %#v, want exec", got)
	}
	argumentsFrame := decodeChatFrame(t, frames[1])
	argumentsCall := argumentsFrame["choices"].([]any)[0].(map[string]any)["delta"].(map[string]any)["tool_calls"].([]any)[0].(map[string]any)
	if got := argumentsCall["function"].(map[string]any)["arguments"]; got != `{"x":1}` {
		t.Fatalf("buffered arguments = %#v, want JSON arguments", got)
	}
}

func TestResponsesToChatSSESuppressesUnresolvedEmptyToolName(t *testing.T) {
	relay := newSSERelay("nexus/test")
	feed := func(raw string) [][]byte {
		frames, err := relay.Feed([]byte("data: " + raw + "\n"))
		if err != nil {
			t.Fatal(err)
		}
		return frames
	}

	if frames := feed(`{"type":"response.output_item.added","output_index":1,"item":{"id":"fc_1","type":"function_call","call_id":"call_1","name":""}}`); len(frames) != 0 {
		t.Fatalf("empty-name start frames = %d, want 0", len(frames))
	}
	frames := feed(`{"type":"response.completed","response":{"output":[{"id":"fc_1","type":"function_call","call_id":"call_1","name":"","arguments":"{}"}]}}`)
	if len(frames) != 1 {
		t.Fatalf("unresolved empty-name frames = %d, want finish only", len(frames))
	}
	finish := decodeChatFrame(t, frames[0])
	choice := finish["choices"].([]any)[0].(map[string]any)
	if choice["finish_reason"] != "stop" {
		t.Fatalf("finish_reason = %#v, want stop", choice["finish_reason"])
	}
	if strings.Contains(string(frames[0]), `"name":""`) {
		t.Fatalf("finish frame contains an empty tool name: %s", frames[0])
	}
}

func TestNormalizeClineResponseOmitsEmptyToolName(t *testing.T) {
	raw := []byte(`{"id":"resp_bad_tool","object":"response","model":"nexus/test","output":[{"type":"function_call","call_id":"call_bad","name":"","arguments":"{}"}]}`)
	got, err := normalizeClineResponse(raw, "nexus/test")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(got), `"name":""`) {
		t.Fatalf("non-stream response contains an empty tool name: %s", got)
	}
	var response map[string]any
	if err := json.Unmarshal(got, &response); err != nil {
		t.Fatal(err)
	}
	message := response["choices"].([]any)[0].(map[string]any)["message"].(map[string]any)
	if _, exists := message["tool_calls"]; exists {
		t.Fatalf("invalid tool call was not omitted: %#v", message)
	}
}
