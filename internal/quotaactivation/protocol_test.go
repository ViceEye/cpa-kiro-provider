package quotaactivation

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestParseAuthMaterialReadsNestedCodexFields(t *testing.T) {
	material, err := ParseAuthMaterial([]byte(`{
        "token_data": {"access_token":"access-token","account_id":"account-1"},
        "project_id":"project-1"
    }`))
	if err != nil {
		t.Fatal(err)
	}
	if material.AccessToken != "access-token" || material.AccountID != "account-1" || material.ProjectID != "project-1" {
		t.Fatalf("material = %#v", material)
	}
}

func TestBuildCodexProtocolUsesNativeActivationShape(t *testing.T) {
	request, err := BuildCodexProtocol(AuthMaterial{AccessToken: "access", AccountID: "account"}, "nexus/gpt-5.4-mini", "ping")
	if err != nil {
		t.Fatal(err)
	}
	if request.Method != http.MethodPost || request.URL != CodexActivationURL {
		t.Fatalf("request = %#v", request)
	}
	if request.Headers.Get("Authorization") != "Bearer access" || request.Headers.Get("Chatgpt-Account-Id") != "account" {
		t.Fatalf("headers = %#v", request.Headers)
	}
	var body struct {
		Model string `json:"model"`
		Input []struct {
			Type    string `json:"type"`
			Role    string `json:"role"`
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"input"`
		Store  bool `json:"store"`
		Stream bool `json:"stream"`
	}
	if errDecode := json.Unmarshal(request.Body, &body); errDecode != nil {
		t.Fatal(errDecode)
	}
	if body.Model != "gpt-5.4-mini" || len(body.Input) != 1 || body.Input[0].Type != "message" || body.Input[0].Role != "user" || body.Input[0].Content[0].Type != "input_text" || !body.Stream || body.Store {
		t.Fatalf("codex body = %#v", body)
	}
}

func TestBuildAntigravityProtocolUsesNativeActivationShape(t *testing.T) {
	request, err := BuildAntigravityProtocol(AuthMaterial{AccessToken: "access", ProjectID: "project"}, "gemini-3-flash", "ping")
	if err != nil {
		t.Fatal(err)
	}
	if request.Method != http.MethodPost || request.URL != AntigravityActivationURL {
		t.Fatalf("request = %#v", request)
	}
	var body struct {
		Project string `json:"project"`
		Model   string `json:"model"`
		Request struct {
			Contents []struct {
				Role  string `json:"role"`
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"contents"`
		} `json:"request"`
	}
	if errDecode := json.Unmarshal(request.Body, &body); errDecode != nil {
		t.Fatal(errDecode)
	}
	if body.Project != "projects/project" || body.Model != "gemini-3-flash" || len(body.Request.Contents) != 1 || body.Request.Contents[0].Role != "user" || body.Request.Contents[0].Parts[0].Text != "ping" {
		t.Fatalf("antigravity body = %#v", body)
	}
}

func TestEvaluateActivationResponsesRequiresStructure(t *testing.T) {
	if ok, _ := EvaluateCodexActivationSuccess(http.StatusOK, []byte(`{"ok":true}`)); ok {
		t.Fatal("Codex accepted an unstructured success")
	}
	if ok, _ := EvaluateCodexActivationSuccess(http.StatusOK, []byte("data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_1\"}}\n")); !ok {
		t.Fatal("Codex rejected a structured SSE response")
	}
	if ok, _ := EvaluateAntigravityActivationSuccess(http.StatusOK, []byte(`{"response":{"candidates":[{}]}}`)); !ok {
		t.Fatal("Antigravity rejected a structured response")
	}
	if ok, _ := EvaluateAntigravityActivationSuccess(http.StatusOK, []byte(`{"response":{"candidates":[]}}`)); ok {
		t.Fatal("Antigravity accepted an empty response")
	}
}
