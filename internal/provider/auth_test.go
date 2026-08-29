package provider

import (
	"encoding/json"
	"testing"
)

func TestParseAuthSingleAccountUsesHostFileIdentity(t *testing.T) {
	raw, _ := json.Marshal(credential{RefreshToken: "fixture-refresh", AccessToken: "fixture-access"})
	rawRequest, _ := json.Marshal(authParseRequest{Provider: providerID, FileName: "kiro-fixture.json", RawJSON: raw})
	rawResponse, errParse := parseAuth(rawRequest)
	if errParse != nil {
		t.Fatal(errParse)
	}
	var env envelope
	if json.Unmarshal(rawResponse, &env) != nil || !env.OK {
		t.Fatalf("parse envelope = %#v", env)
	}
	var response authParseResponse
	if json.Unmarshal(env.Result, &response) != nil || !response.Handled {
		t.Fatalf("parse response = %#v", response)
	}
	if response.Auth.ID != "" || response.Auth.FileName != "kiro-fixture.json" {
		t.Fatalf("single-account identity = %q/%q, want empty/file name", response.Auth.ID, response.Auth.FileName)
	}
}

func TestParseAuthMultiAccountKeepsDistinctContentIDs(t *testing.T) {
	accounts := []credential{
		{RefreshToken: "fixture-refresh-1", AccessToken: "a1", ProfileARN: "arn:aws:codewhisperer:us-east-1:0:profile/one"},
		{RefreshToken: "fixture-refresh-2", AccessToken: "a2", ProfileARN: "arn:aws:codewhisperer:us-east-1:0:profile/two"},
	}
	raw, _ := json.Marshal(accounts)
	rawRequest, _ := json.Marshal(authParseRequest{Provider: providerID, FileName: "kiro-multi.json", RawJSON: raw})
	rawResponse, errParse := parseAuth(rawRequest)
	if errParse != nil {
		t.Fatal(errParse)
	}
	var env envelope
	if json.Unmarshal(rawResponse, &env) != nil || !env.OK {
		t.Fatalf("parse envelope = %#v", env)
	}
	var response authParseResponse
	if json.Unmarshal(env.Result, &response) != nil {
		t.Fatalf("parse response = %#v", response)
	}
	if len(response.Auths) != 2 {
		t.Fatalf("auths = %#v", response.Auths)
	}
	if response.Auths[0].ID == "" || response.Auths[1].ID == "" || response.Auths[0].ID == response.Auths[1].ID {
		t.Fatalf("multi-account IDs collide or empty: %q / %q", response.Auths[0].ID, response.Auths[1].ID)
	}
}

func TestRefreshAuthPreservesUnknownStoredFields(t *testing.T) {
	originalConfig := loadedConfig()
	originalHTTP := hostHTTPDoCall
	t.Cleanup(func() {
		configValue.Store(originalConfig)
		hostHTTPDoCall = originalHTTP
	})
	configValue.Store(pluginConfig{ImportMode: "copy", DesktopRefreshURL: "https://fixture.invalid/refresh"})
	hostHTTPDoCall = func(req hostHTTPRequest) (hostHTTPResponse, error) {
		return hostHTTPResponse{StatusCode: 200, Body: []byte(`{"accessToken":"refreshed-access","refreshToken":"rotated-refresh","expiresIn":3600}`)}, nil
	}

	stored := map[string]any{
		"type":          providerID,
		"version":       1,
		"access_token":  "expired-access",
		"refresh_token": "old-refresh",
		"profile_arn":   "arn:aws:codewhisperer:us-east-1:000000000000:profile/fixture",
		"expires_at":    "2000-01-01T00:00:00Z",
		"disabled":      true,
		"priority":      float64(7),
		"note":          "fixture note",
	}
	storage, _ := json.Marshal(stored)
	rawRequest, _ := json.Marshal(authRefreshRequest{AuthID: "kiro-fixture.json", StorageJSON: storage})
	rawResponse, errRefresh := refreshAuth(rawRequest)
	if errRefresh != nil {
		t.Fatal(errRefresh)
	}
	var env envelope
	if json.Unmarshal(rawResponse, &env) != nil || !env.OK {
		t.Fatalf("refresh envelope = %#v", env)
	}
	var response authRefreshResponse
	if json.Unmarshal(env.Result, &response) != nil {
		t.Fatalf("refresh response = %#v", response)
	}
	if response.Auth.ID != "" || response.Auth.FileName != "" {
		t.Fatalf("wire identity = %q/%q, want empty", response.Auth.ID, response.Auth.FileName)
	}
	var merged map[string]any
	if json.Unmarshal(response.Auth.StorageJSON, &merged) != nil {
		t.Fatal("storage is not a JSON object")
	}
	if merged["disabled"] != true || merged["priority"] != float64(7) || merged["note"] != "fixture note" {
		t.Fatalf("unknown stored fields lost: %#v", merged)
	}
	if merged["access_token"] != "refreshed-access" {
		t.Fatalf("credential fields not refreshed: %#v", merged)
	}
}
