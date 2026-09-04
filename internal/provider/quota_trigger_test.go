package provider

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestNormalizeQuotaTriggerScheduleRequiresModel(t *testing.T) {
	_, err := normalizeQuotaTriggerSchedule(quotaTriggerSchedule{
		Provider:  "antigravity",
		AuthIndex: "auth-1",
		Time:      "08:30",
		Timezone:  "Asia/Shanghai",
	})
	if err == nil || err.Error() != "缺少模型名称" {
		t.Fatalf("err = %v", err)
	}
}

func TestNormalizeQuotaTriggerModelRemovesProviderPrefix(t *testing.T) {
	for input, expected := range map[string]string{
		"nexus/gpt-5.4-mini":       "gpt-5.4-mini",
		"codex/gpt-5.4-mini":       "gpt-5.4-mini",
		"antigravity/gemini-flash": "gemini-flash",
	} {
		if actual := normalizeQuotaTriggerModel(input); actual != expected {
			t.Errorf("normalizeQuotaTriggerModel(%q) = %q, want %q", input, actual, expected)
		}
	}
}

func TestQuotaTriggerDueAtUsesConfiguredTimezone(t *testing.T) {
	schedule := quotaTriggerSchedule{Provider: "codex", AuthIndex: "auth-1", Time: "08:30", Timezone: "Asia/Shanghai", Model: "gpt-5.4-mini"}
	now := time.Date(2026, time.September, 4, 0, 30, 0, 0, time.UTC)
	due, dateKey, ok := quotaTriggerDueAt(schedule, now)
	if !ok || dateKey != "2026-09-04" || due.Hour() != 8 || due.Minute() != 30 {
		t.Fatalf("due = %v date = %q ok = %v", due, dateKey, ok)
	}
}

func TestExecuteQuotaTriggerCodexUsesSelectedCredential(t *testing.T) {
	originalCall := callHostCall
	originalHTTP := hostHTTPDoCall
	t.Cleanup(func() {
		callHostCall = originalCall
		hostHTTPDoCall = originalHTTP
	})

	storage := []byte(`{"type":"codex","access_token":"fixture-access","account_id":"account-1"}`)
	callHostCall = func(method string, payload any) (json.RawMessage, error) {
		switch method {
		case "host.auth.get":
			result, _ := json.Marshal(hostAuthGetResponse{AuthIndex: "auth-1", Name: "codex.json", JSON: storage})
			return result, nil
		case "host.auth.get_runtime":
			return json.RawMessage(`{"auth":{"disabled":false}}`), nil
		default:
			t.Fatalf("unexpected host callback %q", method)
			return nil, nil
		}
	}
	hostHTTPDoCall = func(req hostHTTPRequest) (hostHTTPResponse, error) {
		if req.Method != http.MethodPost || req.URL != "https://chatgpt.com/backend-api/codex/responses" {
			t.Fatalf("request = %#v", req)
		}
		if req.Headers.Get("Authorization") != "Bearer fixture-access" || req.Headers.Get("Chatgpt-Account-Id") != "account-1" {
			t.Fatalf("headers = %#v", req.Headers)
		}
		if !strings.Contains(string(req.Body), `"model":"gpt-5.4-mini"`) || !strings.Contains(string(req.Body), `"stream":true`) {
			t.Fatalf("body = %s", req.Body)
		}
		return hostHTTPResponse{StatusCode: http.StatusOK, Body: []byte("data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_1\"}}\n")}, nil
	}

	errTrigger := executeQuotaTrigger(quotaTriggerSchedule{
		ID: "qt-test", Provider: "codex", AuthIndex: "auth-1", Model: "gpt-5.4-mini",
		Time: "08:30", Timezone: "UTC", Enabled: true,
	}, "callback-1")
	if errTrigger != nil {
		t.Fatal(errTrigger)
	}
}

func TestExecuteQuotaTriggerAntigravityUsesProjectAndModel(t *testing.T) {
	originalCall := callHostCall
	originalHTTP := hostHTTPDoCall
	t.Cleanup(func() {
		callHostCall = originalCall
		hostHTTPDoCall = originalHTTP
	})

	storage := []byte(`{"type":"antigravity","access_token":"fixture-access","project_id":"project-1"}`)
	callHostCall = func(method string, payload any) (json.RawMessage, error) {
		switch method {
		case "host.auth.get":
			result, _ := json.Marshal(hostAuthGetResponse{AuthIndex: "auth-1", Name: "antigravity.json", JSON: storage})
			return result, nil
		case "host.auth.get_runtime":
			return json.RawMessage(`{"auth":{"disabled":false}}`), nil
		default:
			t.Fatalf("unexpected host callback %q", method)
			return nil, nil
		}
	}
	hostHTTPDoCall = func(req hostHTTPRequest) (hostHTTPResponse, error) {
		if req.Method != http.MethodPost || req.URL != "https://daily-cloudcode-pa.googleapis.com/v1internal:generateContent" {
			t.Fatalf("request = %#v", req)
		}
		if req.Headers.Get("Authorization") != "Bearer fixture-access" {
			t.Fatalf("headers = %#v", req.Headers)
		}
		if !strings.Contains(string(req.Body), `"project":"projects/project-1"`) || !strings.Contains(string(req.Body), `"model":"gemini-3-flash"`) {
			t.Fatalf("body = %s", req.Body)
		}
		return hostHTTPResponse{StatusCode: http.StatusOK, Body: []byte(`{"response":{"candidates":[{}]}}`)}, nil
	}

	errTrigger := executeQuotaTrigger(quotaTriggerSchedule{
		ID: "qt-test", Provider: "antigravity", AuthIndex: "auth-1", Model: "gemini-3-flash",
		Time: "08:30", Timezone: "UTC", Enabled: true,
	}, "callback-1")
	if errTrigger != nil {
		t.Fatal(errTrigger)
	}
}
