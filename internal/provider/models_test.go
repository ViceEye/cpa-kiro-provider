package provider

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

func TestModelsForAuthPersistsRefreshAndDiscoversModels(t *testing.T) {
	originalConfig := loadedConfig()
	originalHTTP := hostHTTPDoCall
	t.Cleanup(func() {
		configValue.Store(originalConfig)
		hostHTTPDoCall = originalHTTP
	})
	configValue.Store(pluginConfig{
		ImportMode:        "copy",
		ModelPrefix:       "nexus/",
		DesktopRefreshURL: "https://fixture.invalid/refresh",
		ModelDiscoveryURL: "https://fixture.invalid/models",
	})
	hostHTTPDoCall = func(req hostHTTPRequest) (hostHTTPResponse, error) {
		switch req.URL {
		case "https://fixture.invalid/refresh":
			return hostHTTPResponse{StatusCode: http.StatusOK, Body: []byte(`{"accessToken":"refreshed-access","refreshToken":"rotated-refresh","expiresIn":3600}`)}, nil
		default:
			if req.Method != http.MethodPost || req.URL != "https://fixture.invalid/models/" {
				t.Fatalf("discovery request = %s %s", req.Method, req.URL)
			}
			if got := req.Headers.Get("Authorization"); got != "Bearer refreshed-access" {
				t.Fatalf("discovery authorization = %q", got)
			}
			switch got := req.Headers.Get("X-Amz-Target"); got {
			case "AmazonCodeWhispererService.ListAvailableProfiles":
				return hostHTTPResponse{StatusCode: http.StatusOK, Body: []byte(`{"profiles":[{"arn":"arn:aws:codewhisperer:us-east-1:000000000000:profile/fixture"}]}`)}, nil
			case "AmazonCodeWhispererService.ListAvailableModels":
				return hostHTTPResponse{StatusCode: http.StatusOK, Body: []byte(`{"models":[{"modelId":"fixture-model","modelName":"Fixture Model","description":"Fixture description","rateMultiplier":1.5,"rateUnit":"Credit","supportedInputTypes":["TEXT","IMAGE"],"tokenLimits":{"maxInputTokens":272000,"maxOutputTokens":64000}}]}`)}, nil
			default:
				t.Fatalf("discovery target = %q", got)
			}
			return hostHTTPResponse{}, nil
		}
	}

	storage, errMarshal := json.Marshal(credential{
		Mode:         "copy",
		AccessToken:  "expired-access",
		RefreshToken: "old-refresh",
		ExpiresAt:    "2000-01-01T00:00:00Z",
		SourcePath:   "/fixture/account.json",
	})
	if errMarshal != nil {
		t.Fatal(errMarshal)
	}
	rawRequest, _ := json.Marshal(authModelRequest{AuthID: "kiro-existing", StorageJSON: storage})
	rawResponse, errModels := modelsForAuth(rawRequest)
	if errModels != nil {
		t.Fatal(errModels)
	}
	var env envelope
	if errUnmarshal := json.Unmarshal(rawResponse, &env); errUnmarshal != nil {
		t.Fatal(errUnmarshal)
	}
	var response modelResponse
	if errUnmarshal := json.Unmarshal(env.Result, &response); errUnmarshal != nil {
		t.Fatal(errUnmarshal)
	}
	if len(response.Models) != 1 || response.Models[0].ID != "nexus/fixture-model" {
		t.Fatalf("models = %#v", response.Models)
	}
	if response.Models[0].DisplayName != "Fixture Model" || response.Models[0].InputTokenLimit != 272000 || response.Models[0].OutputTokenLimit != 64000 {
		t.Fatalf("model metadata = %#v", response.Models[0])
	}
	if response.Models[0].Description != "Fixture description (1.5 Credit)" {
		t.Fatalf("model description = %q", response.Models[0].Description)
	}
	// The update leaves ID/FileName empty so the host keeps the existing record
	// identity, and the stored JSON must not adopt the host record ID.
	if response.AuthUpdate.ID != "" || response.AuthUpdate.FileName != "" {
		t.Fatalf("auth update identity = %q/%q, want empty", response.AuthUpdate.ID, response.AuthUpdate.FileName)
	}
	updated, errCredential := decodeCredential(response.AuthUpdate.StorageJSON)
	if errCredential != nil {
		t.Fatal(errCredential)
	}
	if updated.AuthID == "kiro-existing" {
		t.Fatalf("stored auth_id adopted the host record ID: %q", updated.AuthID)
	}
	if updated.AccessToken != "refreshed-access" || updated.RefreshToken != "rotated-refresh" || updated.ProfileARN == "" {
		t.Fatalf("updated credential = %#v", updated)
	}
}

func TestModelsForAuthKeepsCredentialIDWhenProfileIsDiscovered(t *testing.T) {
	originalConfig := loadedConfig()
	originalHTTP := hostHTTPDoCall
	t.Cleanup(func() {
		configValue.Store(originalConfig)
		hostHTTPDoCall = originalHTTP
	})
	configValue.Store(pluginConfig{ModelDiscoveryURL: "https://fixture.invalid/service"})
	hostHTTPDoCall = func(req hostHTTPRequest) (hostHTTPResponse, error) {
		switch req.Headers.Get("X-Amz-Target") {
		case "AmazonCodeWhispererService.ListAvailableProfiles":
			return hostHTTPResponse{StatusCode: http.StatusOK, Body: []byte(`{"profiles":[{"arn":"arn:aws:codewhisperer:us-east-1:000000000000:profile/fixture"}]}`)}, nil
		case "AmazonCodeWhispererService.ListAvailableModels":
			return hostHTTPResponse{StatusCode: http.StatusOK, Body: []byte(`{"models":[{"modelId":"fixture-model"}]}`)}, nil
		default:
			t.Fatalf("unexpected target %q", req.Headers.Get("X-Amz-Target"))
			return hostHTTPResponse{}, nil
		}
	}

	legacy := credential{
		AccessToken: "fixture-access", RefreshToken: "fixture-refresh", SourcePath: "/credentials/account.json",
		ExpiresAt: time.Now().UTC().Add(time.Hour).Format(time.RFC3339),
	}
	storage, _ := json.Marshal(legacy)
	wantID := credentialID(legacy)
	rawRequest, _ := json.Marshal(authModelRequest{StorageJSON: storage})
	rawResponse, errModels := modelsForAuth(rawRequest)
	if errModels != nil {
		t.Fatal(errModels)
	}
	var env envelope
	_ = json.Unmarshal(rawResponse, &env)
	var response modelResponse
	_ = json.Unmarshal(env.Result, &response)
	// Wire identity stays empty (the host keeps its record identity), but the
	// stored credential must retain its own content-derived identity.
	if response.AuthUpdate.ID != "" || response.AuthUpdate.FileName != "" {
		t.Fatalf("wire auth identity = %q/%q, want empty", response.AuthUpdate.ID, response.AuthUpdate.FileName)
	}
	updated, errCredential := decodeCredential(response.AuthUpdate.StorageJSON)
	if errCredential != nil || updated.ProfileARN == "" || updated.AuthID != wantID || credentialID(updated) != wantID {
		t.Fatalf("profile was not stored: %#v, err=%v", updated, errCredential)
	}
	reparsed, errAuth := authDataFromCredential(updated)
	if errAuth != nil || reparsed.ID != wantID || reparsed.FileName != wantID+".json" {
		t.Fatalf("reparsed auth identity changed: %#v, err=%v", reparsed, errAuth)
	}
}
