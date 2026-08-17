package provider

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestEnsureProfileARNUsesAWSJSONAndSelectsFirstValidProfile(t *testing.T) {
	originalHTTP := hostHTTPDoCall
	originalConfig := loadedConfig()
	t.Cleanup(func() {
		hostHTTPDoCall = originalHTTP
		configValue.Store(originalConfig)
	})
	configValue.Store(pluginConfig{ModelDiscoveryURL: "https://fixture.invalid/service"})

	hostHTTPDoCall = func(req hostHTTPRequest) (hostHTTPResponse, error) {
		if req.Method != http.MethodPost || req.URL != "https://fixture.invalid/service/" {
			t.Fatalf("profile request = %s %s", req.Method, req.URL)
		}
		if req.Headers.Get("Content-Type") != "application/x-amz-json-1.0" || req.Headers.Get("X-Amz-Target") != "AmazonCodeWhispererService.ListAvailableProfiles" {
			t.Fatalf("profile headers = %#v", req.Headers)
		}
		if req.Headers.Get("Authorization") != "Bearer fixture-access" || string(req.Body) != "{}" {
			t.Fatalf("profile authorization/body = %q / %s", req.Headers.Get("Authorization"), req.Body)
		}
		return hostHTTPResponse{StatusCode: http.StatusOK, Body: []byte(`{"profiles":[{}, {"arn":"arn:aws:codewhisperer:us-east-1:000000000000:profile/first"}, {"arn":"arn:aws:codewhisperer:us-east-1:000000000000:profile/second"}]}`)}, nil
	}

	cred, discovered, errProfile := ensureProfileARN(credential{AccessToken: "fixture-access", APIRegion: "us-east-1"}, "callback-1")
	if errProfile != nil {
		t.Fatal(errProfile)
	}
	if !discovered || cred.ProfileARN != "arn:aws:codewhisperer:us-east-1:000000000000:profile/first" {
		t.Fatalf("discovered credential = %#v, discovered=%v", cred, discovered)
	}
}

func TestEnsureProfileARNSkipsDiscoveryWhenPresent(t *testing.T) {
	originalHTTP := hostHTTPDoCall
	t.Cleanup(func() { hostHTTPDoCall = originalHTTP })
	hostHTTPDoCall = func(req hostHTTPRequest) (hostHTTPResponse, error) {
		t.Fatalf("unexpected profile request: %#v", req)
		return hostHTTPResponse{}, nil
	}

	want := "arn:aws:codewhisperer:us-east-1:000000000000:profile/existing"
	cred, discovered, errProfile := ensureProfileARN(credential{AccessToken: "fixture-access", ProfileARN: want}, "callback-1")
	if errProfile != nil || discovered || cred.ProfileARN != want {
		t.Fatalf("existing profile result = %#v, discovered=%v, err=%v", cred, discovered, errProfile)
	}
}

func TestEnsureProfileARNRejectsEmptyProfileList(t *testing.T) {
	originalHTTP := hostHTTPDoCall
	t.Cleanup(func() { hostHTTPDoCall = originalHTTP })
	hostHTTPDoCall = func(req hostHTTPRequest) (hostHTTPResponse, error) {
		return hostHTTPResponse{StatusCode: http.StatusOK, Body: []byte(`{"profiles":[]}`)}, nil
	}
	_, _, errProfile := ensureProfileARN(credential{AccessToken: "fixture-access"}, "callback-1")
	if errProfile == nil || errProfile.Error() != "Kiro profile discovery returned no available profile" {
		t.Fatalf("empty profile error = %v", errProfile)
	}
}

func decodeRequestBody(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	var result map[string]any
	if errDecode := json.Unmarshal(raw, &result); errDecode != nil {
		t.Fatal(errDecode)
	}
	return result
}
