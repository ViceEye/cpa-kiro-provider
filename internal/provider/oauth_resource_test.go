package provider

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

const resourceRedirectBase = "https://cpa.example/v0/resource/plugins/cpa-provider-nexus/oauth"

func TestPublicBrowserCallbackAcceptsCodeAndPortalPath(t *testing.T) {
	originalConfig := loadedConfig()
	t.Cleanup(func() { configValue.Store(originalConfig) })
	configValue.Store(pluginConfig{LoginMode: defaultLoginMode, BrowserRedirectURI: resourceRedirectBase})

	for _, path := range []string{
		"/v0/resource/plugins/cpa-provider-nexus/oauth",
		"/v0/resource/plugins/cpa-provider-nexus/oauth/signin/callback",
	} {
		state := randomID()
		storeBrowserLoginSession(browserLoginState{
			Version: 1, LoginMode: defaultLoginMode, State: state, CodeVerifier: "verifier",
			RedirectURI: resourceRedirectBase, TokenURL: defaultTokenURL, ExpiresAt: time.Now().UTC().Add(time.Minute).Format(time.RFC3339),
		})
		t.Cleanup(func() { clearBrowserLoginSession(state) })

		response := handleManagementResponse(t, managementRequest{
			Method: http.MethodGet,
			Path:   path,
			Query:  map[string][]string{"state": {state}, "code": {"fixture-code"}},
		})
		if response.StatusCode != http.StatusOK || !strings.Contains(string(response.Body), "Kiro authorization received") {
			t.Fatalf("resource response for %s = status %d body %s", path, response.StatusCode, response.Body)
		}
		session, exists := browserLoginSessionForState(state)
		if !exists || session.Callback == nil || session.Callback.Code != "fixture-code" {
			t.Fatalf("callback was not stored for %s: %#v", path, session)
		}
	}
}

func TestPublicBrowserCallbackStoresOAuthError(t *testing.T) {
	originalConfig := loadedConfig()
	t.Cleanup(func() { configValue.Store(originalConfig) })
	configValue.Store(pluginConfig{LoginMode: defaultLoginMode, BrowserRedirectURI: resourceRedirectBase})
	state := resourceBrowserSession(t, time.Now().UTC().Add(time.Minute))

	response := handleManagementResponse(t, managementRequest{
		Method: http.MethodGet,
		Path:   "/v0/resource/plugins/cpa-provider-nexus/oauth/signin/callback",
		Query:  map[string][]string{"state": {state}, "error": {"access_denied"}},
	})
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("OAuth error response status = %d body=%s", response.StatusCode, response.Body)
	}
	session, _ := browserLoginSessionForState(state)
	if session.Callback == nil || session.Callback.Error != "access_denied" {
		t.Fatalf("OAuth error was not stored: %#v", session.Callback)
	}
}

func TestPublicOrganizationCallbackRedirectsAndPolls(t *testing.T) {
	originalConfig := loadedConfig()
	originalHTTP := hostHTTPDoCall
	t.Cleanup(func() {
		configValue.Store(originalConfig)
		hostHTTPDoCall = originalHTTP
	})
	configValue.Store(pluginConfig{
		LoginMode: defaultLoginMode, BrowserRedirectURI: resourceRedirectBase,
		APIRegion: "us-east-1", ModelDiscoveryURL: "https://service.fixture.invalid",
	})

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
				t.Fatalf("organization registration = %#v", payload)
			}
			return hostHTTPResponse{StatusCode: http.StatusOK, Body: []byte(`{"clientId":"resource-client","clientSecret":"resource-secret"}`)}, nil
		case "https://oidc.eu-west-1.amazonaws.com/device_authorization":
			return hostHTTPResponse{StatusCode: http.StatusOK, Body: []byte(`{"deviceCode":"resource-device","userCode":"RESO-URCE","verificationUri":"https://device.sso.eu-west-1.amazonaws.com/","verificationUriComplete":"https://device.sso.eu-west-1.amazonaws.com/?user_code=RESO-URCE","expiresIn":600,"interval":1}`)}, nil
		case "https://oidc.eu-west-1.amazonaws.com/token":
			return hostHTTPResponse{StatusCode: http.StatusOK, Body: []byte(`{"accessToken":"resource-access","refreshToken":"resource-refresh","expiresIn":3600}`)}, nil
		case "https://service.fixture.invalid/":
			return hostHTTPResponse{StatusCode: http.StatusOK, Body: []byte(`{"profiles":[{"arn":"arn:aws:codewhisperer:us-east-1:000000000000:profile/resource"}]}`)}, nil
		default:
			t.Fatalf("unexpected resource OAuth request: %#v", req)
			return hostHTTPResponse{}, nil
		}
	}

	start := startBrowserLoginForTest(t)
	callback := managementRequest{
		Method:         http.MethodGet,
		Path:           "/v0/resource/plugins/cpa-provider-nexus/oauth/signin/callback",
		HostCallbackID: "resource-callback",
		Query: map[string][]string{
			"state": {start.State}, "login_option": {"awsidc"},
			"issuer_url": {"https://example.awsapps.com/start"}, "idc_region": {"eu-west-1"},
		},
	}
	response := handleManagementResponse(t, callback)
	if response.StatusCode != http.StatusFound || response.Headers.Get("Location") != "https://device.sso.eu-west-1.amazonaws.com/?user_code=RESO-URCE" {
		t.Fatalf("organization resource response = status %d headers %#v body=%s", response.StatusCode, response.Headers, response.Body)
	}
	if response.Headers.Get("Cache-Control") != "no-store" || response.Headers.Get("Referrer-Policy") != "no-referrer" {
		t.Fatalf("organization resource security headers = %#v", response.Headers)
	}

	duplicate := handleManagementResponse(t, callback)
	if duplicate.StatusCode != http.StatusConflict || requests != 2 {
		t.Fatalf("duplicate callback = status %d requests %d", duplicate.StatusCode, requests)
	}

	setNextDeviceLoginPoll(start.State, time.Now().UTC().Add(-time.Second))
	pollRaw, _ := json.Marshal(authLoginPollRequest{
		Provider: providerID, State: start.State, Metadata: start.Metadata, HostCallbackID: "resource-poll",
	})
	pollEnvelopeRaw, errPoll := pollLogin(pollRaw)
	if errPoll != nil {
		t.Fatal(errPoll)
	}
	var pollEnvelope envelope
	_ = json.Unmarshal(pollEnvelopeRaw, &pollEnvelope)
	var poll authLoginPollResponse
	_ = json.Unmarshal(pollEnvelope.Result, &poll)
	var stored credential
	_ = json.Unmarshal(poll.Auth.StorageJSON, &stored)
	if poll.Status != "success" || stored.RefreshToken != "resource-refresh" || stored.Label != "Kiro Organization" || stored.ProfileARN == "" {
		t.Fatalf("organization resource poll = %#v stored=%#v", poll, stored)
	}
	if requests != 4 {
		t.Fatalf("organization request count = %d, want 4", requests)
	}
}

func TestPublicBuilderIDCallbackUsesDefaultIssuer(t *testing.T) {
	originalConfig := loadedConfig()
	originalHTTP := hostHTTPDoCall
	t.Cleanup(func() {
		configValue.Store(originalConfig)
		hostHTTPDoCall = originalHTTP
	})
	configValue.Store(pluginConfig{LoginMode: defaultLoginMode, BrowserRedirectURI: resourceRedirectBase, SSORegion: "us-east-1"})

	hostHTTPDoCall = func(req hostHTTPRequest) (hostHTTPResponse, error) {
		switch req.URL {
		case "https://oidc.us-east-1.amazonaws.com/client/register":
			var payload map[string]any
			_ = json.Unmarshal(req.Body, &payload)
			if payload["issuerUrl"] != defaultSSOStartURL {
				t.Fatalf("Builder ID issuer = %#v", payload["issuerUrl"])
			}
			return hostHTTPResponse{StatusCode: http.StatusOK, Body: []byte(`{"clientId":"builder-client","clientSecret":"builder-secret"}`)}, nil
		case "https://oidc.us-east-1.amazonaws.com/device_authorization":
			return hostHTTPResponse{StatusCode: http.StatusOK, Body: []byte(`{"deviceCode":"builder-device","userCode":"BUIL-DER1","verificationUri":"https://device.sso.us-east-1.amazonaws.com/","expiresIn":600,"interval":5}`)}, nil
		default:
			t.Fatalf("unexpected Builder ID request: %#v", req)
			return hostHTTPResponse{}, nil
		}
	}
	state := resourceBrowserSession(t, time.Now().UTC().Add(time.Minute))
	response := handleManagementResponse(t, managementRequest{
		Method: http.MethodGet, Path: "/v0/resource/plugins/cpa-provider-nexus/oauth",
		Query: map[string][]string{"state": {state}, "login_option": {"builderid"}},
	})
	if response.StatusCode != http.StatusFound || response.Headers.Get("Location") != "https://device.sso.us-east-1.amazonaws.com/" {
		t.Fatalf("Builder ID response = status %d headers %#v", response.StatusCode, response.Headers)
	}
}

func TestPublicCallbackRejectsInvalidInputs(t *testing.T) {
	originalConfig := loadedConfig()
	originalHTTP := hostHTTPDoCall
	t.Cleanup(func() {
		configValue.Store(originalConfig)
		hostHTTPDoCall = originalHTTP
	})
	configValue.Store(pluginConfig{LoginMode: defaultLoginMode, BrowserRedirectURI: resourceRedirectBase})
	hostHTTPDoCall = func(req hostHTTPRequest) (hostHTTPResponse, error) {
		switch req.URL {
		case "https://oidc.eu-west-1.amazonaws.com/client/register":
			return hostHTTPResponse{StatusCode: http.StatusOK, Body: []byte(`{"clientId":"unsafe-client","clientSecret":"unsafe-secret"}`)}, nil
		case "https://oidc.eu-west-1.amazonaws.com/device_authorization":
			return hostHTTPResponse{StatusCode: http.StatusOK, Body: []byte(`{"deviceCode":"unsafe-device","userCode":"UNSA-FE00","verificationUriComplete":"https://s3.amazonaws.com/device","expiresIn":600,"interval":5}`)}, nil
		default:
			t.Fatalf("unexpected invalid-input request: %#v", req)
			return hostHTTPResponse{}, nil
		}
	}

	unknown := handleManagementResponse(t, managementRequest{
		Method: http.MethodGet, Path: "/v0/resource/plugins/cpa-provider-nexus/oauth",
		Query: map[string][]string{"state": {randomID()}, "code": {"code"}},
	})
	if unknown.StatusCode != http.StatusBadRequest {
		t.Fatalf("unknown state status = %d", unknown.StatusCode)
	}

	expiredState := resourceBrowserSession(t, time.Now().UTC().Add(-time.Second))
	expired := handleManagementResponse(t, managementRequest{
		Method: http.MethodGet, Path: "/v0/resource/plugins/cpa-provider-nexus/oauth",
		Query: map[string][]string{"state": {expiredState}, "code": {"code"}},
	})
	if expired.StatusCode != http.StatusBadRequest {
		t.Fatalf("expired state status = %d", expired.StatusCode)
	}

	badIssuerState := resourceBrowserSession(t, time.Now().UTC().Add(time.Minute))
	badIssuer := handleManagementResponse(t, managementRequest{
		Method: http.MethodGet, Path: "/v0/resource/plugins/cpa-provider-nexus/oauth",
		Query: map[string][]string{
			"state": {badIssuerState}, "login_option": {"awsidc"},
			"issuer_url": {"https://evil.example/start"}, "idc_region": {"eu-west-1"},
		},
	})
	if badIssuer.StatusCode != http.StatusBadRequest {
		t.Fatalf("bad issuer status = %d", badIssuer.StatusCode)
	}

	badRegionState := resourceBrowserSession(t, time.Now().UTC().Add(time.Minute))
	badRegion := handleManagementResponse(t, managementRequest{
		Method: http.MethodGet, Path: "/v0/resource/plugins/cpa-provider-nexus/oauth",
		Query: map[string][]string{
			"state": {badRegionState}, "login_option": {"awsidc"},
			"issuer_url": {"https://example.awsapps.com/start"}, "idc_region": {"not-a-region"},
		},
	})
	if badRegion.StatusCode != http.StatusBadRequest {
		t.Fatalf("bad region status = %d", badRegion.StatusCode)
	}

	unsafeState := resourceBrowserSession(t, time.Now().UTC().Add(time.Minute))
	unsafe := handleManagementResponse(t, managementRequest{
		Method: http.MethodGet, Path: "/v0/resource/plugins/cpa-provider-nexus/oauth",
		Query: map[string][]string{
			"state": {unsafeState}, "login_option": {"awsidc"},
			"issuer_url": {"https://example.awsapps.com/start"}, "idc_region": {"eu-west-1"},
		},
	})
	if unsafe.StatusCode != http.StatusBadGateway {
		t.Fatalf("unsafe verification status = %d body=%s", unsafe.StatusCode, unsafe.Body)
	}
	session, _ := browserLoginSessionForState(unsafeState)
	if session.Device != nil || session.Continuing {
		t.Fatalf("unsafe verification left continuation state: %#v", session)
	}

	wrongHostState := resourceBrowserSession(t, time.Now().UTC().Add(time.Minute))
	wrongHost, _ := url.Parse("https://evil.example/signin/callback?state=" + wrongHostState + "&code=code")
	if _, errCallback := processBrowserCallback(wrongHost, ""); pluginHTTPStatus(errCallback) != http.StatusBadRequest {
		t.Fatalf("wrong callback host error = %v", errCallback)
	}

	wrongPathState := resourceBrowserSession(t, time.Now().UTC().Add(time.Minute))
	wrongPath, _ := url.Parse(resourceRedirectBase + "/other?state=" + wrongPathState + "&code=code")
	if _, errCallback := processBrowserCallback(wrongPath, ""); pluginHTTPStatus(errCallback) != http.StatusBadRequest {
		t.Fatalf("wrong callback path error = %v", errCallback)
	}

	duplicateState := resourceBrowserSession(t, time.Now().UTC().Add(time.Minute))
	duplicate, _ := url.Parse(resourceRedirectBase + "?state=" + duplicateState + "&code=first")
	if _, errCallback := processBrowserCallback(duplicate, ""); errCallback != nil {
		t.Fatal(errCallback)
	}
	duplicate.RawQuery = "state=" + duplicateState + "&code=second"
	if _, errCallback := processBrowserCallback(duplicate, ""); pluginHTTPStatus(errCallback) != http.StatusConflict {
		t.Fatalf("duplicate callback error = %v", errCallback)
	}
	deviceAfterCode, _ := url.Parse(resourceRedirectBase + "?state=" + duplicateState + "&login_option=builderid")
	if _, errCallback := processBrowserCallback(deviceAfterCode, ""); pluginHTTPStatus(errCallback) != http.StatusConflict {
		t.Fatalf("device continuation after code error = %v", errCallback)
	}
}

func TestUnifiedPortalRemainsDefaultWithConfiguredOrganization(t *testing.T) {
	originalConfig := loadedConfig()
	t.Cleanup(func() { configValue.Store(originalConfig) })
	configValue.Store(pluginConfig{
		LoginMode: defaultLoginMode, BrowserRedirectURI: resourceRedirectBase,
		SSOStartURL: "https://example.awsapps.com/start", SSORegion: "eu-west-1",
	})
	start := startBrowserLoginForTest(t)
	loginURL, errParse := url.Parse(start.URL)
	if errParse != nil || loginURL.Host != "app.kiro.dev" || loginURL.Query().Get("redirect_uri") != resourceRedirectBase {
		t.Fatalf("unified portal URL = %q err=%v", start.URL, errParse)
	}
}

func TestBrowserRedirectURIValidation(t *testing.T) {
	for input, valid := range map[string]bool{
		"http://localhost:3128": true,
		"http://127.0.0.1:3128": true,
		resourceRedirectBase:    true,
		"http://cpa.example/v0/resource/plugins/cpa-provider-nexus/oauth":              false,
		"https://user:secret@cpa.example/v0/resource/plugins/cpa-provider-nexus/oauth": false,
		"javascript:alert(1)": false,
	} {
		_, errParse := parseBrowserRedirectURI(input)
		if (errParse == nil) != valid {
			t.Fatalf("parseBrowserRedirectURI(%q) error=%v, valid=%v", input, errParse, valid)
		}
	}
}

func resourceBrowserSession(t *testing.T, expiresAt time.Time) string {
	t.Helper()
	state := randomID()
	storeBrowserLoginSession(browserLoginState{
		Version: 1, LoginMode: defaultLoginMode, State: state, CodeVerifier: "verifier",
		RedirectURI: resourceRedirectBase, TokenURL: defaultTokenURL, APIRegion: defaultRegion,
		ExpiresAt: expiresAt.Format(time.RFC3339),
	})
	t.Cleanup(func() {
		clearBrowserLoginSession(state)
		clearDeviceLoginPoll(state)
	})
	return state
}

func startBrowserLoginForTest(t *testing.T) authLoginStartResponse {
	t.Helper()
	raw, errStart := startLogin([]byte(`{"Provider":"nexus","host_callback_id":"start-callback"}`))
	if errStart != nil {
		t.Fatal(errStart)
	}
	var env envelope
	if errDecode := json.Unmarshal(raw, &env); errDecode != nil || !env.OK {
		t.Fatalf("start login envelope = %s err=%v", raw, errDecode)
	}
	var start authLoginStartResponse
	if errDecode := json.Unmarshal(env.Result, &start); errDecode != nil {
		t.Fatal(errDecode)
	}
	t.Cleanup(func() {
		clearBrowserLoginSession(start.State)
		clearDeviceLoginPoll(start.State)
	})
	return start
}

func handleManagementResponse(t *testing.T, req managementRequest) managementResponse {
	t.Helper()
	raw, errHandle := handleManagement(mustJSON(req))
	if errHandle != nil {
		t.Fatal(errHandle)
	}
	var env envelope
	if errDecode := json.Unmarshal(raw, &env); errDecode != nil || !env.OK {
		t.Fatalf("management envelope = %s err=%v", raw, errDecode)
	}
	var response managementResponse
	if errDecode := json.Unmarshal(env.Result, &response); errDecode != nil {
		t.Fatal(errDecode)
	}
	return response
}
