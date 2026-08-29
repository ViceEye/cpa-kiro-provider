package chat

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"
)

func TestNormalizeImageResizesValidPNG(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 2400, 1200))
	for y := 0; y < 1200; y++ {
		for x := 0; x < 2400; x++ {
			src.Set(x, y, color.RGBA{R: uint8(x % 255), G: uint8(y % 255), B: 80, A: 255})
		}
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, src); err != nil {
		t.Fatal(err)
	}
	format, data := normalizeImage("image/png", encodeBase64(encoded.Bytes()))
	if format != "png" {
		t.Fatalf("format = %q, want png", format)
	}
	decoded, err := decodeBase64(data)
	if err != nil {
		t.Fatal(err)
	}
	img, _, err := image.Decode(bytes.NewReader(decoded))
	if err != nil {
		t.Fatal(err)
	}
	if max(img.Bounds().Dx(), img.Bounds().Dy()) > maxImageDimension {
		t.Fatalf("longest edge = %d, want <= %d", max(img.Bounds().Dx(), img.Bounds().Dy()), maxImageDimension)
	}
}

func TestBuildPayloadDropsOversizedImageInsteadOfFailing(t *testing.T) {
	blob := strings.Repeat("A", maxKiroPayloadBytes+40000)
	raw := []byte(`{"model":"kiro/claude-sonnet-5","messages":[{"role":"user","content":[
      {"type":"text","text":"what is in this screenshot"},
      {"type":"image","source":{"media_type":"image/png","data":"` + blob + `"}}
    ]}]}`)

	payload, _, errBuild := BuildPayload(raw, "kiro/claude-sonnet-5", "arn:fake")
	if errBuild != nil {
		t.Fatalf("oversized image should degrade, not fail: %v", errBuild)
	}
	if len(payload) > maxKiroPayloadBytes {
		t.Fatalf("payload size = %d, limit = %d", len(payload), maxKiroPayloadBytes)
	}

	var object map[string]any
	if errJSON := json.Unmarshal(payload, &object); errJSON != nil {
		t.Fatal(errJSON)
	}
	input := object["conversationState"].(map[string]any)["currentMessage"].(map[string]any)["userInputMessage"].(map[string]any)
	if _, exists := input["images"]; exists {
		t.Fatalf("oversized image survived: %#v", input["images"])
	}
	content := input["content"].(string)
	if !strings.Contains(content, "what is in this screenshot") {
		t.Fatalf("prompt text was lost: %q", content)
	}
	if !strings.Contains(content, "image was dropped") {
		t.Fatalf("no notice that the image was dropped: %q", content)
	}
}

func TestBuildPayloadKeepsSmallImage(t *testing.T) {
	raw := []byte(`{"model":"kiro/claude-sonnet-5","messages":[{"role":"user","content":[
      {"type":"text","text":"tiny"},
      {"type":"image","source":{"media_type":"image/png","data":"QUJD"}}
    ]}]}`)
	payload, _, errBuild := BuildPayload(raw, "kiro/claude-sonnet-5", "arn:fake")
	if errBuild != nil {
		t.Fatal(errBuild)
	}
	if !strings.Contains(string(payload), "QUJD") {
		t.Fatalf("small image was dropped: %s", payload)
	}
	if strings.Contains(string(payload), "image was dropped") {
		t.Fatalf("small image should not trigger the drop notice")
	}
}

func TestBuildPayloadTruncatesOversizedToolResult(t *testing.T) {
	blob := strings.Repeat("L", maxKiroPayloadBytes+40000)
	raw := []byte(`{"model":"kiro/claude-sonnet-5","messages":[
      {"role":"user","content":"run it"},
      {"role":"assistant","content":"","tool_calls":[{"id":"c1","type":"function","function":{"name":"lookup","arguments":"{}"}}]},
      {"role":"tool","tool_call_id":"c1","content":"` + blob + `"}
    ],"tools":[{"type":"function","function":{"name":"lookup","parameters":{"type":"object"}}}]}`)

	payload, _, errBuild := BuildPayload(raw, "kiro/claude-sonnet-5", "arn:fake")
	if errBuild != nil {
		t.Fatalf("oversized tool result should degrade, not fail: %v", errBuild)
	}
	if len(payload) > maxKiroPayloadBytes {
		t.Fatalf("payload size = %d, limit = %d", len(payload), maxKiroPayloadBytes)
	}
	if !strings.Contains(string(payload), "truncated") {
		t.Fatalf("no truncation notice in payload")
	}
}

func TestBuildPayloadSurvivesOversizedImageInHistory(t *testing.T) {
	blob := strings.Repeat("H", maxKiroPayloadBytes+40000)
	raw := []byte(`{"model":"kiro/claude-sonnet-5","messages":[
      {"role":"user","content":[
        {"type":"text","text":"old screenshot"},
        {"type":"image","source":{"media_type":"image/png","data":"` + blob + `"}}
      ]},
      {"role":"assistant","content":"I see it"},
      {"role":"user","content":"now what"}
    ]}`)

	payload, _, errBuild := BuildPayload(raw, "kiro/claude-sonnet-5", "arn:fake")
	if errBuild != nil {
		t.Fatalf("oversized history image should be trimmed away, not fail: %v", errBuild)
	}
	if len(payload) > maxKiroPayloadBytes {
		t.Fatalf("payload size = %d, limit = %d", len(payload), maxKiroPayloadBytes)
	}
	if !strings.Contains(string(payload), "now what") {
		t.Fatalf("current message was lost: %s", payload[:min(400, len(payload))])
	}
}

// Trimming only removes history, so it can leave the current message holding a
// tool result whose declaring assistant turn was dropped. Kiro rejects that
// with "unexpected tool_use_id found in tool_result blocks".
func TestBuildPayloadDemotesToolResultOrphanedByTrimming(t *testing.T) {
	pad := strings.Repeat("p", 60000)
	var messages []string
	for index := 0; index < 12; index++ {
		id := "call_" + string(rune('a'+index))
		messages = append(messages,
			`{"role":"user","content":"ask-`+pad+`"}`,
			`{"role":"assistant","content":"","tool_calls":[{"id":"`+id+`","type":"function","function":{"name":"lookup","arguments":"{}"}}]}`,
			`{"role":"tool","tool_call_id":"`+id+`","content":"res-`+pad+`"}`,
		)
	}
	messages = append(messages,
		`{"role":"assistant","content":"","tool_calls":[{"id":"call_final","type":"function","function":{"name":"lookup","arguments":"{}"}}]}`,
		`{"role":"tool","tool_call_id":"call_final","content":"final result"}`,
	)
	raw := []byte(`{"model":"kiro/claude-sonnet-5","messages":[` + strings.Join(messages, ",") + `],"tools":[{"type":"function","function":{"name":"lookup","parameters":{"type":"object"}}}]}`)

	payload, _, errBuild := BuildPayload(raw, "kiro/claude-sonnet-5", "arn:fake")
	if errBuild != nil {
		t.Fatal(errBuild)
	}

	var object map[string]any
	if errJSON := json.Unmarshal(payload, &object); errJSON != nil {
		t.Fatal(errJSON)
	}
	state := object["conversationState"].(map[string]any)
	history, _ := state["history"].([]any)
	current := state["currentMessage"].(map[string]any)["userInputMessage"].(map[string]any)
	context, _ := current["userInputMessageContext"].(map[string]any)

	declared := map[string]bool{}
	if len(history) > 0 {
		last := history[len(history)-1].(map[string]any)
		assistant, _ := last["assistantResponseMessage"].(map[string]any)
		uses, _ := assistant["toolUses"].([]any)
		for _, use := range uses {
			declared[use.(map[string]any)["toolUseId"].(string)] = true
		}
	}
	results, _ := context["toolResults"].([]any)
	for _, result := range results {
		id := result.(map[string]any)["toolUseId"].(string)
		if !declared[id] {
			t.Fatalf("tool result %q has no tool_use in the last history entry", id)
		}
	}
	if !strings.Contains(current["content"].(string), "final result") {
		t.Fatalf("demoted tool result text was lost: %q", current["content"])
	}
}
