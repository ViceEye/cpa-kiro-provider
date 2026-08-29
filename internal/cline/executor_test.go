package cline

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestUnwrapDataEnvelope(t *testing.T) {
	wrapped := []byte(`{"data":{"choices":[{"message":{"content":"成功"}}]}}`)
	inner, err := unwrapDataEnvelope(wrapped)
	if err != nil {
		t.Fatal(err)
	}
	var probe struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(inner, &probe); err != nil {
		t.Fatal(err)
	}
	if len(probe.Choices) != 1 || probe.Choices[0].Message.Content != "成功" {
		t.Fatalf("unwrapped payload = %s", inner)
	}
}

func TestUnwrapDataEnvelopePassesThrough(t *testing.T) {
	plain := []byte(`{"choices":[{"message":{"content":"hi"}}]}`)
	inner, err := unwrapDataEnvelope(plain)
	if err != nil {
		t.Fatal(err)
	}
	var probe struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(inner, &probe); err != nil {
		t.Fatal(err)
	}
	if len(probe.Choices) != 1 || probe.Choices[0].Message.Content != "hi" {
		t.Fatalf("plain payload mangled: %s", inner)
	}
}

func TestUpstreamPayloadStripsPrefix(t *testing.T) {
	payload := []byte(`{"model":"kiro/z-ai/glm-5.3-flash","messages":[{"role":"user","content":"hi"}]}`)
	upstream, err := upstreamPayload(payload, "kiro/z-ai/glm-5.3-flash")
	if err != nil {
		t.Fatal(err)
	}
	var body map[string]any
	if err := json.Unmarshal(upstream, &body); err != nil {
		t.Fatal(err)
	}
	if body["model"] != "z-ai/glm-5.3-flash" {
		t.Fatalf("model = %v, want upstream id", body["model"])
	}
}

func TestRelaySSEUnwrapsEnvelope(t *testing.T) {
	in := "data: {\"data\":{\"choices\":[{\"delta\":{\"content\":\"成\"}}]}}\ndata: [DONE]\n"
	out := string(relaySSE([]byte(in)))
	if !strings.Contains(out, `"delta":{"content":"成"}`) {
		t.Fatalf("relayed SSE missing unwrapped delta: %s", out)
	}
	if !strings.Contains(out, "data: [DONE]") {
		t.Fatalf("relayed SSE lost DONE marker: %s", out)
	}
	if strings.Contains(out, `"data":{"data":`) {
		t.Fatalf("relayed SSE still wrapped: %s", out)
	}
}


