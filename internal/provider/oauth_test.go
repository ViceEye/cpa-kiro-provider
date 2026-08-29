package provider

import (
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestBrowserLoginStartAndPoll(t *testing.T) {
	originalHTTP := hostHTTPDoCall
	originalConfig := loadedConfig()
	t.Cleanup(func() {
		hostHTTPDoCall = originalHTTP
		configValue.Store(originalConfig)
	})
	configValue.Store(pluginConfig{ImportMode: "reference", LoginMode: "kiro-browser", ModelPrefix: "kiro/", APIRegion: "eu-west-1"})

	requests := 0
	hostHTTPDoCall = func(req hostHTTPRequest) (hostHTTPResponse, error) {
		requests++
		if req.Method != http.MethodPost || req.URL != defaultTokenURL {
			t.Fatalf("unexpected token request: %#v", req)
		}
		if req.Headers.Get("Content-Type") != "application/json" || req.Headers.Get("User-Agent") != "Kiro-CLI" {
			t.Fatalf("unexpected token headers: %#v", req.Headers)
		}
		var payload map[string]string
		if errDecode := json.Unmarshal(req.Body, &payload); errDecode != nil {
			t.Fatal(errDecode)
		}
		if payload["code"] != "browser-code" || payload["code_verifier"] == "" || payload["redirect_uri"] != defaultRedirectURI {
			t.Fatalf("unexpected token payload: %#v", payload)
		}
		return hostHTTPResponse{StatusCode: http.StatusOK, Body: []byte(`{"accessToken":"access-browser","refreshToken":"refresh-browser","profileArn":"arn:aws:codewhisperer:eu-west-1:000000000000:profile/test","expiresIn":3600}`)}, nil
	}

	startEnvelopeRaw, errStart := startLogin([]byte(`{"Provider":"kiro","host_callback_id":"callback-start"}`))
	if errStart != nil {
		t.Fatal(errStart)
	}
	var startEnvelope envelope
	if errDecode := json.Unmarshal(startEnvelopeRaw, &startEnvelope); errDecode != nil || !startEnvelope.OK {
		t.Fatalf("start envelope = %s, err = %v", startEnvelopeRaw, errDecode)
	}
	var start authLoginStartResponse
	if errDecode := json.Unmarshal(startEnvelope.Result, &start); errDecode != nil {
		t.Fatal(errDecode)
	}
	loginURL, errURL := url.Parse(start.URL)
	if errURL != nil {
		t.Fatal(errURL)
	}
	query := loginURL.Query()
	if loginURL.Scheme != "https" || loginURL.Host != "app.kiro.dev" || loginURL.Path != "/signin" || query.Get("state") != start.State || query.Get("code_challenge") == "" || query.Get("code_challenge_method") != "S256" || query.Get("redirect_uri") != defaultRedirectURI || query.Get("redirect_from") != "kirocli" {
		t.Fatalf("login URL = %s", start.URL)
	}
	if query.Get("code_verifier") != "" {
		t.Fatal("login URL must not expose the PKCE verifier")
	}

	authDir := t.TempDir()
	pollRequest, _ := json.Marshal(authLoginPollRequest{Provider: providerID, State: start.State, Metadata: start.Metadata, Host: hostConfigSummary{AuthDir: authDir}, HostCallbackID: "callback-poll"})
	pendingRaw, errPending := pollLogin(pollRequest)
	if errPending != nil {
		t.Fatal(errPending)
	}
	var pendingEnvelope envelope
	_ = json.Unmarshal(pendingRaw, &pendingEnvelope)
	var pending authLoginPollResponse
	_ = json.Unmarshal(pendingEnvelope.Result, &pending)
	if pending.Status != "pending" || requests != 0 {
		t.Fatalf("pending response = %s requests=%d", pendingRaw, requests)
	}

	callbackPath := filepath.Join(authDir, ".oauth-kiro-"+start.State+".oauth")
	callbackRaw, _ := json.Marshal(oauthCallbackPayload{Code: "browser-code", State: start.State})
	if errWrite := os.WriteFile(callbackPath, callbackRaw, 0o600); errWrite != nil {
		t.Fatal(errWrite)
	}
	pollEnvelopeRaw, errPoll := pollLogin(pollRequest)
	if errPoll != nil {
		t.Fatal(errPoll)
	}
	var pollEnvelope envelope
	if errDecode := json.Unmarshal(pollEnvelopeRaw, &pollEnvelope); errDecode != nil || !pollEnvelope.OK {
		t.Fatalf("poll envelope = %s, err = %v", pollEnvelopeRaw, errDecode)
	}
	var poll authLoginPollResponse
	if errDecode := json.Unmarshal(pollEnvelope.Result, &poll); errDecode != nil {
		t.Fatal(errDecode)
	}
	if poll.Status != "success" || poll.Auth.Provider != providerID || poll.Auth.ID == "" {
		t.Fatalf("poll response = %#v", poll)
	}
	var stored credential
	if errDecode := json.Unmarshal(poll.Auth.StorageJSON, &stored); errDecode != nil {
		t.Fatal(errDecode)
	}
	if stored.AuthType != "kiro_desktop" || stored.RefreshToken != "refresh-browser" || stored.ProfileARN == "" || stored.Mode != "copy" || stored.SSORegion != "us-east-1" || stored.APIRegion != "eu-west-1" || stored.SourceKind != "oauth_browser" {
		t.Fatalf("stored credential = %#v", stored)
	}
	if requests != 1 {
		t.Fatalf("request count = %d, want 1", requests)
	}
	if _, errStat := os.Stat(callbackPath); !os.IsNotExist(errStat) {
		t.Fatalf("callback file was not removed: %v", errStat)
	}
}

func TestDeviceLoginStartAndPoll(t *testing.T) {
	originalHTTP := hostHTTPDoCall
	originalConfig := loadedConfig()
	t.Cleanup(func() {
		hostHTTPDoCall = originalHTTP
		configValue.Store(originalConfig)
	})
	organizationStartURL := "https://example.awsapps.com/start"
	configValue.Store(pluginConfig{ImportMode: "reference", LoginMode: "aws-device", ModelPrefix: "kiro/", SSORegion: "eu-west-1", APIRegion: "us-west-2", SSOStartURL: organizationStartURL, ModelDiscoveryURL: "https://service.fixture.invalid"})

	requests := 0
	hostHTTPDoCall = func(req hostHTTPRequest) (hostHTTPResponse, error) {
		requests++
		if req.Method != http.MethodPost {
			t.Fatalf("unexpected request: %#v", req)
		}
		switch req.URL {
		case "https://oidc.eu-west-1.amazonaws.com/client/register":
			return hostHTTPResponse{StatusCode: http.StatusOK, Body: []byte(`{"clientId":"client-test","clientSecret":"secret-test","clientSecretExpiresAt":4102444800}`)}, nil
		case "https://oidc.eu-west-1.amazonaws.com/device_authorization":
			return hostHTTPResponse{StatusCode: http.StatusOK, Body: []byte(`{"deviceCode":"device-test","userCode":"ABCD-EFGH","verificationUri":"https://example.awsapps.com/start/","verificationUriComplete":"https://example.awsapps.com/start/#/device?user_code=ABCD-EFGH","expiresIn":600,"interval":5}`)}, nil
		case "https://oidc.eu-west-1.amazonaws.com/token":
			return hostHTTPResponse{StatusCode: http.StatusOK, Body: []byte(`{"accessToken":"access-test","refreshToken":"refresh-test","expiresIn":3600,"tokenType":"Bearer"}`)}, nil
		case "https://service.fixture.invalid/":
			return hostHTTPResponse{StatusCode: http.StatusOK, Body: []byte(`{"profiles":[{"arn":"arn:aws:codewhisperer:us-west-2:000000000000:profile/device"}]}`)}, nil
		default:
			t.Fatalf("unexpected URL %q", req.URL)
			return hostHTTPResponse{}, nil
		}
	}

	startEnvelopeRaw, errStart := startLogin([]byte(`{"Provider":"kiro","host_callback_id":"callback-start"}`))
	if errStart != nil {
		t.Fatal(errStart)
	}
	var startEnvelope envelope
	if errDecode := json.Unmarshal(startEnvelopeRaw, &startEnvelope); errDecode != nil || !startEnvelope.OK {
		t.Fatalf("start envelope = %s, err = %v", startEnvelopeRaw, errDecode)
	}
	var start authLoginStartResponse
	if errDecode := json.Unmarshal(startEnvelope.Result, &start); errDecode != nil {
		t.Fatal(errDecode)
	}
	if start.Provider != providerID || start.State == "" || start.URL == "" || start.ExpiresAt.Before(time.Now().UTC()) {
		t.Fatalf("start response = %#v", start)
	}

	setNextDeviceLoginPoll(start.State, time.Now().UTC().Add(-time.Second))
	pollRequest, _ := json.Marshal(authLoginPollRequest{Provider: providerID, State: start.State, Metadata: start.Metadata, HostCallbackID: "callback-poll"})
	pollEnvelopeRaw, errPoll := pollLogin(pollRequest)
	if errPoll != nil {
		t.Fatal(errPoll)
	}
	var pollEnvelope envelope
	if errDecode := json.Unmarshal(pollEnvelopeRaw, &pollEnvelope); errDecode != nil || !pollEnvelope.OK {
		t.Fatalf("poll envelope = %s, err = %v", pollEnvelopeRaw, errDecode)
	}
	var poll authLoginPollResponse
	if errDecode := json.Unmarshal(pollEnvelope.Result, &poll); errDecode != nil {
		t.Fatal(errDecode)
	}
	if poll.Status != "success" || poll.Auth.Provider != providerID || poll.Auth.ID == "" {
		t.Fatalf("poll response = %#v", poll)
	}
	var stored credential
	if errDecode := json.Unmarshal(poll.Auth.StorageJSON, &stored); errDecode != nil {
		t.Fatal(errDecode)
	}
	if stored.AuthType != "aws_sso_oidc" || stored.RefreshToken != "refresh-test" || stored.ClientID != "client-test" || stored.Mode != "copy" || stored.SSORegion != "eu-west-1" || stored.APIRegion != "us-west-2" || stored.ProfileARN == "" || stored.Label != "Kiro Organization" {
		t.Fatalf("stored credential = %#v", stored)
	}
	if requests != 4 {
		t.Fatalf("request count = %d, want 4", requests)
	}
}

func TestDeviceLoginPollPending(t *testing.T) {
	originalHTTP := hostHTTPDoCall
	t.Cleanup(func() { hostHTTPDoCall = originalHTTP })
	hostHTTPDoCall = func(req hostHTTPRequest) (hostHTTPResponse, error) {
		return hostHTTPResponse{StatusCode: http.StatusBadRequest, Body: []byte(`{"error":"authorization_pending","error_description":"Authorization is pending"}`)}, nil
	}
	state := "state-test"
	metadata := map[string]any{
		"version": 1, "state": state, "client_id": "client-test", "client_secret": "secret-test",
		"device_code": "device-test", "region": "us-east-1", "start_url": defaultSSOStartURL,
		"expires_at": time.Now().UTC().Add(time.Minute).Format(time.RFC3339), "interval": 1,
	}
	decoded, errDecode := decodeDeviceLoginState(metadata)
	if errDecode != nil || decoded.APIRegion != defaultRegion {
		t.Fatalf("decoded legacy device state = %#v, err = %v", decoded, errDecode)
	}
	setNextDeviceLoginPoll(state, time.Now().UTC().Add(-time.Second))
	raw, _ := json.Marshal(authLoginPollRequest{Provider: providerID, State: state, Metadata: metadata, HostCallbackID: "callback-poll"})
	envelopeRaw, errPoll := pollLogin(raw)
	if errPoll != nil {
		t.Fatal(errPoll)
	}
	var result envelope
	_ = json.Unmarshal(envelopeRaw, &result)
	var response authLoginPollResponse
	_ = json.Unmarshal(result.Result, &response)
	if !result.OK || response.Status != "pending" {
		t.Fatalf("response = %s", envelopeRaw)
	}
}

func TestKiroPortalOrganizationCallbackContinuesWithDeviceAuthorization(t *testing.T) {
	originalHTTP := hostHTTPDoCall
	originalConfig := loadedConfig()
	t.Cleanup(func() {
		hostHTTPDoCall = originalHTTP
		configValue.Store(originalConfig)
	})
	configValue.Store(pluginConfig{ImportMode: "copy", LoginMode: "kiro-browser", ModelPrefix: "kiro/", ModelDiscoveryURL: "https://service.fixture.invalid"})

	requests := 0
	hostHTTPDoCall = func(req hostHTTPRequest) (hostHTTPResponse, error) {
		requests++
		switch req.URL {
		case "https://oidc.eu-west-1.amazonaws.com/client/register":
			var payload map[string]any
			if errDecode := json.Unmarshal(req.Body, &payload); errDecode != nil {
				t.Fatal(errDecode)
			}
			if payload["issuerUrl"] != "https://example.awsapps.com/start" {
				t.Fatalf("registration payload = %#v", payload)
			}
			return hostHTTPResponse{StatusCode: http.StatusOK, Body: []byte(`{"clientId":"portal-client","clientSecret":"portal-secret","clientSecretExpiresAt":4102444800}`)}, nil
		case "https://oidc.eu-west-1.amazonaws.com/device_authorization":
			return hostHTTPResponse{StatusCode: http.StatusOK, Body: []byte(`{"deviceCode":"portal-device","userCode":"PORT-AL12","verificationUri":"https://device.sso.eu-west-1.amazonaws.com/","verificationUriComplete":"https://device.sso.eu-west-1.amazonaws.com/?user_code=PORT-AL12","expiresIn":600,"interval":1}`)}, nil
		case "https://oidc.eu-west-1.amazonaws.com/token":
			return hostHTTPResponse{StatusCode: http.StatusOK, Body: []byte(`{"accessToken":"portal-access","refreshToken":"portal-refresh","expiresIn":3600,"tokenType":"Bearer"}`)}, nil
		case "https://service.fixture.invalid/":
			return hostHTTPResponse{StatusCode: http.StatusOK, Body: []byte(`{"profiles":[{"arn":"arn:aws:codewhisperer:us-east-1:000000000000:profile/portal"}]}`)}, nil
		default:
			t.Fatalf("unexpected URL %q", req.URL)
			return hostHTTPResponse{}, nil
		}
	}

	startEnvelopeRaw, errStart := startLogin([]byte(`{"Provider":"kiro","host_callback_id":"callback-start"}`))
	if errStart != nil {
		t.Fatal(errStart)
	}
	var startEnvelope envelope
	if errDecode := json.Unmarshal(startEnvelopeRaw, &startEnvelope); errDecode != nil || !startEnvelope.OK {
		t.Fatalf("start envelope = %s, err = %v", startEnvelopeRaw, errDecode)
	}
	var start authLoginStartResponse
	if errDecode := json.Unmarshal(startEnvelope.Result, &start); errDecode != nil {
		t.Fatal(errDecode)
	}
	t.Cleanup(func() {
		clearBrowserLoginSession(start.State)
		clearDeviceLoginPoll(start.State)
	})

	callbackURL := "http://localhost:3128/signin/callback?login_option=awsidc&issuer_url=https%3A%2F%2Fexample.awsapps.com%2Fstart&idc_region=eu-west-1&state=" + url.QueryEscape(start.State)
	callbackBody, _ := json.Marshal(browserCallbackManagementRequest{RedirectURL: callbackURL})
	managementEnvelopeRaw, errManagement := handleManagement(mustJSON(managementRequest{
		Method: http.MethodPost, Path: "/v0/management/plugins/kiro-provider/oauth/callback", Body: callbackBody, HostCallbackID: "callback-management",
	}))
	if errManagement != nil {
		t.Fatal(errManagement)
	}
	var managementEnvelope envelope
	if errDecode := json.Unmarshal(managementEnvelopeRaw, &managementEnvelope); errDecode != nil || !managementEnvelope.OK {
		t.Fatalf("management envelope = %s, err = %v", managementEnvelopeRaw, errDecode)
	}
	var management managementResponse
	if errDecode := json.Unmarshal(managementEnvelope.Result, &management); errDecode != nil {
		t.Fatal(errDecode)
	}
	var continuation map[string]any
	if errDecode := json.Unmarshal(management.Body, &continuation); errDecode != nil {
		t.Fatal(errDecode)
	}
	if management.StatusCode != http.StatusOK || continuation["status"] != "continue" || continuation["user_code"] != "PORT-AL12" {
		t.Fatalf("continuation = status %d body %#v", management.StatusCode, continuation)
	}

	setNextDeviceLoginPoll(start.State, time.Now().UTC().Add(-time.Second))
	pollRequest, _ := json.Marshal(authLoginPollRequest{Provider: providerID, State: start.State, Metadata: start.Metadata, HostCallbackID: "callback-poll"})
	pollEnvelopeRaw, errPoll := pollLogin(pollRequest)
	if errPoll != nil {
		t.Fatal(errPoll)
	}
	var pollEnvelope envelope
	_ = json.Unmarshal(pollEnvelopeRaw, &pollEnvelope)
	var poll authLoginPollResponse
	_ = json.Unmarshal(pollEnvelope.Result, &poll)
	if poll.Status != "success" {
		t.Fatalf("poll response = %s", pollEnvelopeRaw)
	}
	var stored credential
	if errDecode := json.Unmarshal(poll.Auth.StorageJSON, &stored); errDecode != nil {
		t.Fatal(errDecode)
	}
	if stored.AuthType != "aws_sso_oidc" || stored.RefreshToken != "portal-refresh" || stored.ClientID != "portal-client" || stored.SSORegion != "eu-west-1" || stored.APIRegion != "us-east-1" || stored.ProfileARN == "" || stored.Label != "Kiro Organization" {
		t.Fatalf("stored credential = %#v", stored)
	}
	if requests != 4 {
		t.Fatalf("request count = %d, want 4", requests)
	}
}

func TestDeviceLoginProfileDiscoveryFailureKeepsValidTokens(t *testing.T) {
	originalHTTP := hostHTTPDoCall
	originalConfig := loadedConfig()
	t.Cleanup(func() {
		hostHTTPDoCall = originalHTTP
		configValue.Store(originalConfig)
	})
	configValue.Store(pluginConfig{ModelDiscoveryURL: "https://service.fixture.invalid"})
	hostHTTPDoCall = func(req hostHTTPRequest) (hostHTTPResponse, error) {
		switch req.Headers.Get("X-Amz-Target") {
		case "AmazonCodeWhispererService.ListAvailableProfiles":
			return hostHTTPResponse{StatusCode: http.StatusInternalServerError, Body: []byte(`{"message":"fixture unavailable"}`)}, nil
		default:
			return hostHTTPResponse{StatusCode: http.StatusOK, Body: []byte(`{"accessToken":"valid-access","refreshToken":"valid-refresh","expiresIn":3600}`)}, nil
		}
	}
	state := "profile-failure-state"
	metadata := map[string]any{
		"version": 1, "login_mode": "aws-device", "state": state,
		"client_id": "client", "client_secret": "secret", "device_code": "device",
		"region": "us-east-1", "api_region": "us-east-1", "start_url": defaultSSOStartURL,
		"expires_at": time.Now().UTC().Add(time.Minute).Format(time.RFC3339), "interval": 1,
	}
	setNextDeviceLoginPoll(state, time.Now().UTC().Add(-time.Second))
	rawResponse, errPoll := pollDeviceLoginRequest(authLoginPollRequest{Provider: providerID, State: state, Metadata: metadata})
	if errPoll != nil {
		t.Fatal(errPoll)
	}
	var env envelope
	_ = json.Unmarshal(rawResponse, &env)
	var response authLoginPollResponse
	_ = json.Unmarshal(env.Result, &response)
	var stored credential
	_ = json.Unmarshal(response.Auth.StorageJSON, &stored)
	if response.Status != "success" || stored.AccessToken != "valid-access" || stored.RefreshToken != "valid-refresh" || stored.ProfileARN != "" {
		t.Fatalf("login result = %#v, stored=%#v", response, stored)
	}
}

func TestIDCBrowserLoginStateDefaultsAPIRegion(t *testing.T) {
	state, errDecode := decodeIDCBrowserLoginState(map[string]any{
		"version": 1, "login_mode": "organization-browser", "state": "legacy-state",
		"code_verifier": "verifier", "redirect_uri": defaultIDCRedirectURI,
		"client_id": "client", "client_secret": "secret", "region": "eu-west-1",
		"start_url":  "https://example.awsapps.com/start",
		"expires_at": time.Now().UTC().Add(time.Minute).Format(time.RFC3339),
	})
	if errDecode != nil || state.APIRegion != defaultRegion {
		t.Fatalf("decoded legacy organization state = %#v, err = %v", state, errDecode)
	}
}

func TestNormalizeManagementPath(t *testing.T) {
	for input, expected := range map[string]string{
		"plugins/kiro-provider/quota":                         "/plugins/kiro-provider/quota",
		"/plugins/kiro-provider/quota":                        "/plugins/kiro-provider/quota",
		"/v0/management/plugins/kiro-provider/quota":          "/plugins/kiro-provider/quota",
		"/v0/management/plugins/kiro-provider/oauth/callback": "/plugins/kiro-provider/oauth/callback",
	} {
		if actual := normalizeManagementPath(input); actual != expected {
			t.Fatalf("normalizeManagementPath(%q) = %q, want %q", input, actual, expected)
		}
	}
}

func TestKiroPortalOrganizationCallbackRejectsUntrustedIssuer(t *testing.T) {
	state := browserLoginState{
		Version: 1, LoginMode: defaultLoginMode, State: "callback-state", CodeVerifier: "verifier",
		RedirectURI: defaultRedirectURI, TokenURL: defaultTokenURL, ExpiresAt: time.Now().UTC().Add(time.Minute).Format(time.RFC3339),
	}
	storeBrowserLoginSession(state)
	t.Cleanup(func() { clearBrowserLoginSession(state.State) })
	callbackBody, _ := json.Marshal(browserCallbackManagementRequest{
		RedirectURL: "http://localhost:3128/signin/callback?login_option=awsidc&issuer_url=https%3A%2F%2Fevil.example%2Fstart&idc_region=eu-west-1&state=" + state.State,
	})
	raw, errHandle := handleBrowserCallbackManagement(managementRequest{Method: http.MethodPost, Body: callbackBody})
	if errHandle != nil {
		t.Fatal(errHandle)
	}
	var result envelope
	_ = json.Unmarshal(raw, &result)
	var response managementResponse
	_ = json.Unmarshal(result.Result, &response)
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("response status = %d body=%s", response.StatusCode, response.Body)
	}
}
