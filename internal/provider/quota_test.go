package provider

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/url"
	"testing"
	"time"
)

func TestLoadKiroQuotasReturnsSanitizedUsage(t *testing.T) {
	originalConfig := loadedConfig()
	originalCallHost := callHostCall
	originalHTTP := hostHTTPDoCall
	t.Cleanup(func() {
		configValue.Store(originalConfig)
		callHostCall = originalCallHost
		hostHTTPDoCall = originalHTTP
	})
	configValue.Store(pluginConfig{ImportMode: "copy", UsageURL: "https://fixture.invalid/quota"})

	storage, errMarshal := json.Marshal(credential{
		Type:         providerID,
		Version:      1,
		Mode:         "copy",
		AuthType:     "aws_sso_oidc",
		AccessToken:  "fixture-access",
		RefreshToken: "fixture-refresh",
		ProfileARN:   "arn:aws:codewhisperer:us-east-1:123456789012:profile/fixture",
		APIRegion:    "us-east-1",
		SSORegion:    "eu-west-1",
		ExpiresAt:    time.Now().UTC().Add(time.Hour).Format(time.RFC3339),
	})
	if errMarshal != nil {
		t.Fatal(errMarshal)
	}
	callHostCall = func(method string, payload any) (json.RawMessage, error) {
		switch method {
		case "host.auth.list":
			return json.RawMessage(`{"files":[{"auth_index":"auth-1","name":"kiro-fixture.json","type":"nexus","provider":"nexus","label":"Fixture"}]}`), nil
		case "host.auth.get":
			result, _ := json.Marshal(hostAuthGetResponse{AuthIndex: "auth-1", Name: "kiro-fixture.json", JSON: storage})
			return result, nil
		default:
			t.Fatalf("unexpected host callback %q", method)
			return nil, nil
		}
	}
	hostHTTPDoCall = func(req hostHTTPRequest) (hostHTTPResponse, error) {
		if req.Method != http.MethodGet {
			t.Fatalf("quota request = %s %s", req.Method, req.URL)
		}
		parsed, errParse := url.Parse(req.URL)
		if errParse != nil || parsed.Scheme != "https" || parsed.Host != "fixture.invalid" || parsed.Path != "/quota" {
			t.Fatalf("quota URL = %q, err = %v", req.URL, errParse)
		}
		query := parsed.Query()
		if query.Get("origin") != "AI_EDITOR" || query.Get("resourceType") != "AGENTIC_REQUEST" || query.Get("profileArn") != "arn:aws:codewhisperer:us-east-1:123456789012:profile/fixture" {
			t.Fatalf("quota query = %#v", query)
		}
		if req.Headers.Get("Authorization") != "Bearer fixture-access" {
			t.Fatalf("quota authorization missing")
		}
		if req.Headers.Get("X-Amz-Target") != "" || req.Headers.Get("X-Amz-User-Agent") == "" || len(req.Body) != 0 {
			t.Fatalf("quota headers/body = %#v / %q", req.Headers, req.Body)
		}
		return hostHTTPResponse{StatusCode: http.StatusOK, Body: []byte(`{
          "daysUntilReset":0,
          "nextDateReset":1788220800,
          "subscriptionInfo":{"subscriptionTitle":"KIRO PRO+","type":"Q_DEVELOPER_STANDALONE_PRO_PLUS"},
          "overageConfiguration":{"overageStatus":"ENABLED"},
          "usageBreakdownList":[{
            "resourceType":"CREDIT","displayName":"Credit","unit":"INVOCATIONS","currency":"USD",
            "currentUsageWithPrecision":2165.69,"usageLimitWithPrecision":2000,
            "currentOveragesWithPrecision":165.69,"overageCapWithPrecision":10000,
            "overageRate":0.04,"overageCharges":6.6276,"nextDateReset":1788220800
          }]
        }`)}, nil
	}

	accounts, errQuota := loadKiroQuotas("callback-1")
	if errQuota != nil {
		t.Fatal(errQuota)
	}
	if len(accounts) != 1 || accounts[0].Status != "ok" || accounts[0].Subscription != "KIRO PRO+" {
		t.Fatalf("accounts = %#v", accounts)
	}
	if len(accounts[0].Usage) != 1 || accounts[0].Usage[0].CurrentUsage != 2165.69 || accounts[0].Usage[0].Remaining != 0 {
		t.Fatalf("usage = %#v", accounts[0].Usage)
	}
	if accounts[0].Usage[0].UsagePercent <= 100 || accounts[0].Usage[0].OverageCharges <= 0 {
		t.Fatalf("usage percentage/overage = %#v", accounts[0].Usage[0])
	}
}

func TestKiroUsageEndpointOmitsMissingProfileARN(t *testing.T) {
	endpoint, errEndpoint := kiroUsageEndpoint("", "us-east-1", "")
	if errEndpoint != nil {
		t.Fatal(errEndpoint)
	}
	parsed, errParse := url.Parse(endpoint)
	if errParse != nil {
		t.Fatal(errParse)
	}
	query := parsed.Query()
	if parsed.Host != "q.us-east-1.amazonaws.com" || parsed.Path != "/getUsageLimits" || query.Get("origin") != "AI_EDITOR" || query.Get("resourceType") != "AGENTIC_REQUEST" {
		t.Fatalf("usage endpoint = %q", endpoint)
	}
	if _, exists := query["profileArn"]; exists {
		t.Fatalf("empty profileArn was included: %#v", query)
	}
}

func TestLoadKiroQuotaDiscoversAndPersistsMissingProfile(t *testing.T) {
	originalConfig := loadedConfig()
	originalCallHost := callHostCall
	originalHTTP := hostHTTPDoCall
	t.Cleanup(func() {
		configValue.Store(originalConfig)
		callHostCall = originalCallHost
		hostHTTPDoCall = originalHTTP
	})
	configValue.Store(pluginConfig{ModelDiscoveryURL: "https://fixture.invalid/service", UsageURL: "https://fixture.invalid/quota"})

	storage, _ := json.Marshal(credential{
		Type: providerID, Version: 1, Mode: "copy", AccessToken: "fixture-access", RefreshToken: "fixture-refresh",
		APIRegion: "us-east-1", ExpiresAt: time.Now().UTC().Add(time.Hour).Format(time.RFC3339),
	})
	savedProfile := ""
	callHostCall = func(method string, payload any) (json.RawMessage, error) {
		switch method {
		case "host.auth.get":
			result, _ := json.Marshal(hostAuthGetResponse{AuthIndex: "auth-1", Name: "kiro-legacy.json", JSON: storage})
			return result, nil
		case "host.auth.save":
			raw, _ := json.Marshal(payload)
			var save struct {
				Name string          `json:"name"`
				JSON json.RawMessage `json:"json"`
			}
			if errDecode := json.Unmarshal(raw, &save); errDecode != nil {
				t.Fatal(errDecode)
			}
			var stored credential
			if errDecode := json.Unmarshal(save.JSON, &stored); errDecode != nil {
				t.Fatal(errDecode)
			}
			if save.Name != "kiro-legacy.json" {
				t.Fatalf("saved auth name = %q", save.Name)
			}
			savedProfile = stored.ProfileARN
			return json.RawMessage(`{}`), nil
		default:
			t.Fatalf("unexpected host callback %q", method)
			return nil, nil
		}
	}
	profileARN := "arn:aws:codewhisperer:us-east-1:000000000000:profile/discovered"
	hostHTTPDoCall = func(req hostHTTPRequest) (hostHTTPResponse, error) {
		if req.Headers.Get("X-Amz-Target") == "AmazonCodeWhispererService.ListAvailableProfiles" {
			return hostHTTPResponse{StatusCode: http.StatusOK, Body: []byte(`{"profiles":[{"arn":"` + profileARN + `"}]}`)}, nil
		}
		parsed, _ := url.Parse(req.URL)
		if req.Method != http.MethodGet || parsed.Query().Get("profileArn") != profileARN {
			t.Fatalf("quota request = %#v", req)
		}
		return hostHTTPResponse{StatusCode: http.StatusOK, Body: []byte(`{"subscriptionInfo":{},"overageConfiguration":{},"usageBreakdownList":[]}`)}, nil
	}

	account := quotaAccount{AuthIndex: "auth-1", Name: "kiro-legacy.json"}
	if errLoad := loadKiroQuotaAccount(&account, "callback-1"); errLoad != nil {
		t.Fatal(errLoad)
	}
	if savedProfile != profileARN {
		t.Fatalf("persisted profile was not updated")
	}
}

func TestManagementRegistrationMetadata(t *testing.T) {
	rawRegistration, errRegistration := registration(nil)
	if errRegistration != nil {
		t.Fatal(errRegistration)
	}
	var registrationEnvelope envelope
	if errUnmarshal := json.Unmarshal(rawRegistration, &registrationEnvelope); errUnmarshal != nil {
		t.Fatal(errUnmarshal)
	}
	var registrationResult map[string]any
	if errUnmarshal := json.Unmarshal(registrationEnvelope.Result, &registrationResult); errUnmarshal != nil {
		t.Fatal(errUnmarshal)
	}
	metadata, _ := registrationResult["metadata"].(map[string]any)
	capabilities, _ := registrationResult["capabilities"].(map[string]any)
	if metadata["Name"] != "Nexus" {
		t.Fatalf("Name = %#v", metadata["Name"])
	}
	if metadata["Logo"] != nexusLogoPath {
		t.Fatalf("Logo = %#v", metadata["Logo"])
	}
	if capabilities["management_api"] != true {
		t.Fatalf("management_api = %#v", capabilities["management_api"])
	}

	rawManagement, errManagement := registerManagement()
	if errManagement != nil {
		t.Fatal(errManagement)
	}
	var managementEnvelope envelope
	if errUnmarshal := json.Unmarshal(rawManagement, &managementEnvelope); errUnmarshal != nil {
		t.Fatal(errUnmarshal)
	}
	if !json.Valid(managementEnvelope.Result) {
		t.Fatalf("management registration is invalid JSON: %s", managementEnvelope.Result)
	}
	var managementResult struct {
		Resources []struct {
			Path string `json:"Path"`
		} `json:"Resources"`
	}
	if errUnmarshal := json.Unmarshal(managementEnvelope.Result, &managementResult); errUnmarshal != nil {
		t.Fatal(errUnmarshal)
	}
	foundLogo := false
	for _, resource := range managementResult.Resources {
		foundLogo = foundLogo || resource.Path == "icon.svg"
	}
	if !foundLogo {
		t.Fatalf("icon resource missing: %#v", managementResult.Resources)
	}
}

func TestNexusLogoResource(t *testing.T) {
	response := handleManagementResponse(t, managementRequest{Method: http.MethodGet, Path: nexusLogoPath})
	if response.StatusCode != http.StatusOK {
		t.Fatalf("StatusCode = %d", response.StatusCode)
	}
	if response.Headers.Get("Content-Type") != "image/svg+xml" {
		t.Fatalf("Content-Type = %q", response.Headers.Get("Content-Type"))
	}
	if !bytes.Equal(response.Body, nexusLogoSVG) {
		t.Fatal("logo response does not match embedded nexus.svg")
	}
}
