package provider

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

func TestExecuteKiroNonStreamDiscoversProfileBeforePayload(t *testing.T) {
	originalHTTP := hostHTTPDoCall
	originalConfig := loadedConfig()
	t.Cleanup(func() {
		hostHTTPDoCall = originalHTTP
		configValue.Store(originalConfig)
	})
	configValue.Store(pluginConfig{RuntimeBaseURL: "https://runtime.fixture.invalid", ModelDiscoveryURL: "https://service.fixture.invalid"})
	profileARN := "arn:aws:codewhisperer:us-east-1:000000000000:profile/fixture"
	calls := 0
	hostHTTPDoCall = func(req hostHTTPRequest) (hostHTTPResponse, error) {
		calls++
		if req.Headers.Get("X-Amz-Target") == "AmazonCodeWhispererService.ListAvailableProfiles" {
			return hostHTTPResponse{StatusCode: http.StatusOK, Body: []byte(`{"profiles":[{"arn":"` + profileARN + `"}]}`)}, nil
		}
		payload := decodeRequestBody(t, req.Body)
		if payload["profileArn"] != profileARN {
			t.Fatalf("runtime payload = %#v", payload)
		}
		return hostHTTPResponse{StatusCode: http.StatusOK, Body: responseEventFrame(t, map[string]any{"content": "ok"})}, nil
	}

	storage, _ := json.Marshal(credential{AccessToken: "fixture-access", RefreshToken: "fixture-refresh", ExpiresAt: time.Now().UTC().Add(time.Hour).Format(time.RFC3339)})
	payload := []byte(`{"model":"nexus/fixture-model","messages":[{"role":"user","content":"hello"}]}`)
	_, cred, _, _, errExecute := executeKiroNonStream(executorRequest{Model: "nexus/fixture-model", Payload: payload, StorageJSON: storage})
	if errExecute != nil || cred.ProfileARN != profileARN || calls != 2 {
		t.Fatalf("execution result = cred:%#v calls:%d err:%v", cred, calls, errExecute)
	}
}

func TestPrepareKiroStreamDiscoversProfileBeforePayload(t *testing.T) {
	originalHTTP := hostHTTPDoCall
	originalStream := hostHTTPDoStreamCall
	originalConfig := loadedConfig()
	t.Cleanup(func() {
		hostHTTPDoCall = originalHTTP
		hostHTTPDoStreamCall = originalStream
		configValue.Store(originalConfig)
	})
	configValue.Store(pluginConfig{RuntimeBaseURL: "https://runtime.fixture.invalid", ModelDiscoveryURL: "https://service.fixture.invalid"})
	profileARN := "arn:aws:codewhisperer:us-east-1:000000000000:profile/fixture"
	hostHTTPDoCall = func(req hostHTTPRequest) (hostHTTPResponse, error) {
		return hostHTTPResponse{StatusCode: http.StatusOK, Body: []byte(`{"profiles":[{"arn":"` + profileARN + `"}]}`)}, nil
	}
	hostHTTPDoStreamCall = func(req hostHTTPRequest) (hostHTTPStreamResponse, error) {
		payload := decodeRequestBody(t, req.Body)
		if payload["profileArn"] != profileARN {
			t.Fatalf("stream runtime payload = %#v", payload)
		}
		return hostHTTPStreamResponse{StatusCode: http.StatusOK, StreamID: "fixture-stream"}, nil
	}

	storage, _ := json.Marshal(credential{AccessToken: "fixture-access", RefreshToken: "fixture-refresh", ExpiresAt: time.Now().UTC().Add(time.Hour).Format(time.RFC3339)})
	payload := []byte(`{"model":"nexus/fixture-model","messages":[{"role":"user","content":"hello"}],"stream":true}`)
	response, _, _, errPrepare := prepareKiroStream(executorRequest{Model: "nexus/fixture-model", Payload: payload, StorageJSON: storage})
	if errPrepare != nil || response.StreamID != "fixture-stream" {
		t.Fatalf("stream prepare = %#v, err=%v", response, errPrepare)
	}
}
