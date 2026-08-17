package provider

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/ViceEye/cpa-kiro-provider/internal/jsonx"
)

var deviceLoginPolls = struct {
	sync.Mutex
	next map[string]time.Time
}{next: make(map[string]time.Time)}

type browserLoginSession struct {
	LoginState browserLoginState
	Callback   *oauthCallbackPayload
	Device     *deviceLoginState
}

var browserLoginSessions = struct {
	sync.Mutex
	sessions map[string]browserLoginSession
}{sessions: make(map[string]browserLoginSession)}

var awsRegionPattern = regexp.MustCompile(`^[a-z]{2}(?:-gov)?-[a-z]+-\d+$`)

type oidcClientRegistrationResponse struct {
	ClientID              string `json:"clientId"`
	ClientSecret          string `json:"clientSecret"`
	ClientSecretExpiresAt int64  `json:"clientSecretExpiresAt"`
}

type oidcDeviceAuthorizationResponse struct {
	DeviceCode              string `json:"deviceCode"`
	UserCode                string `json:"userCode"`
	VerificationURI         string `json:"verificationUri"`
	VerificationURIComplete string `json:"verificationUriComplete"`
	ExpiresIn               int    `json:"expiresIn"`
	Interval                int    `json:"interval"`
}

type oidcTokenResponse struct {
	AccessToken  string `json:"accessToken"`
	RefreshToken string `json:"refreshToken"`
	ExpiresIn    int    `json:"expiresIn"`
	TokenType    string `json:"tokenType"`
}

func startLogin(raw []byte) ([]byte, error) {
	config := loadedConfig()
	if config.LoginMode == "aws-device" {
		return startDeviceLogin(raw)
	}
	if config.LoginMode == "organization-browser" || (config.LoginMode == defaultLoginMode && !strings.EqualFold(strings.TrimRight(config.SSOStartURL, "/"), strings.TrimRight(defaultSSOStartURL, "/"))) {
		return startIDCBrowserLogin(raw)
	}
	return startBrowserLogin(raw)
}

func pollLogin(raw []byte) ([]byte, error) {
	var req authLoginPollRequest
	if errUnmarshal := json.Unmarshal(raw, &req); errUnmarshal != nil {
		return nil, errUnmarshal
	}
	mode := strings.ToLower(strings.TrimSpace(jsonx.String(req.Metadata, "login_mode")))
	if mode == "organization-browser" {
		return pollIDCBrowserLoginRequest(req)
	}
	if mode == "aws-device" || (mode == "" && jsonx.String(req.Metadata, "device_code") != "") {
		return pollDeviceLoginRequest(req)
	}
	return pollBrowserLoginRequest(req)
}

func startIDCBrowserLogin(raw []byte) ([]byte, error) {
	var req authLoginStartRequest
	if errUnmarshal := json.Unmarshal(raw, &req); errUnmarshal != nil {
		return nil, errUnmarshal
	}
	if req.Provider != "" && !strings.EqualFold(req.Provider, providerID) {
		return errorEnvelope("invalid_provider", "Kiro login received an unexpected provider", false, http.StatusBadRequest), nil
	}
	config := loadedConfig()
	region := jsonx.NonEmpty(strings.TrimSpace(config.SSORegion), defaultRegion)
	apiRegion := configuredAPIRegion(config)
	startURL := strings.TrimRight(strings.TrimSpace(config.SSOStartURL), "/")
	if startURL == "" || strings.EqualFold(startURL, strings.TrimRight(defaultSSOStartURL, "/")) {
		return errorEnvelope("missing_organization", "Kiro organization browser login requires sso_start_url", false, http.StatusBadRequest), nil
	}
	verifier, challenge, errPKCE := generateBrowserPKCE()
	if errPKCE != nil {
		return pluginErrorEnvelope(statusError{Code: "login_random_error", Message: "Kiro IDC PKCE generation failed", HTTPStatus: http.StatusInternalServerError, Cause: errPKCE}), nil
	}
	state := randomID()
	redirectURI := defaultIDCRedirectURI
	endpoint := "https://oidc." + region + ".amazonaws.com"
	registrationBody, _ := json.Marshal(map[string]any{
		"clientName": "Kiro IDE", "clientType": oidcClientType, "scopes": defaultOIDCScopes,
		"grantTypes": []string{"authorization_code", "refresh_token"}, "redirectUris": []string{redirectURI}, "issuerUrl": startURL,
	})
	registrationHTTP, errRegistrationHTTP := hostHTTPDoCall(hostHTTPRequest{
		HostCallbackID: req.HostCallbackID, Method: http.MethodPost, URL: endpoint + "/client/register",
		Headers: http.Header{"Content-Type": []string{"application/json"}}, Body: registrationBody,
	})
	if errRegistrationHTTP != nil {
		return pluginErrorEnvelope(statusError{Code: "login_network_error", Message: "Kiro IDC client registration failed", Retryable: true, HTTPStatus: http.StatusBadGateway, Cause: errRegistrationHTTP}), nil
	}
	if registrationHTTP.StatusCode < 200 || registrationHTTP.StatusCode >= 300 {
		return pluginErrorEnvelope(upstreamStatusError("Kiro IDC client registration failed", registrationHTTP.StatusCode, registrationHTTP.Body)), nil
	}
	var registration oidcClientRegistrationResponse
	if errDecode := json.Unmarshal(registrationHTTP.Body, &registration); errDecode != nil || strings.TrimSpace(registration.ClientID) == "" || strings.TrimSpace(registration.ClientSecret) == "" {
		return errorEnvelope("invalid_login_response", "Kiro IDC client registration returned incomplete credentials", false, http.StatusBadGateway), nil
	}
	authorizeURL, _ := url.Parse(endpoint + "/authorize")
	query := authorizeURL.Query()
	query.Set("response_type", "code")
	query.Set("client_id", registration.ClientID)
	query.Set("redirect_uri", redirectURI)
	query.Set("scopes", strings.Join(defaultOIDCScopes, ","))
	query.Set("state", state)
	query.Set("code_challenge", challenge)
	query.Set("code_challenge_method", "S256")
	authorizeURL.RawQuery = query.Encode()

	expiresAt := time.Now().UTC().Add(10 * time.Minute)
	loginState := idcBrowserLoginState{
		Version: 1, LoginMode: "organization-browser", State: state, CodeVerifier: verifier,
		RedirectURI: redirectURI, ClientID: registration.ClientID, ClientSecret: registration.ClientSecret,
		Region: region, APIRegion: apiRegion, StartURL: startURL, ExpiresAt: expiresAt.Format(time.RFC3339), Scopes: append([]string(nil), defaultOIDCScopes...),
	}
	metadataRaw, _ := json.Marshal(loginState)
	var metadata map[string]any
	_ = json.Unmarshal(metadataRaw, &metadata)
	return okEnvelope(authLoginStartResponse{Provider: providerID, URL: authorizeURL.String(), State: state, ExpiresAt: expiresAt, Metadata: metadata})
}

func pollIDCBrowserLoginRequest(req authLoginPollRequest) ([]byte, error) {
	loginState, errState := decodeIDCBrowserLoginState(req.Metadata)
	if errState != nil || strings.TrimSpace(req.State) == "" || loginState.State != req.State {
		return okEnvelope(authLoginPollResponse{Status: "error", Message: "Kiro organization login state is invalid or expired"})
	}
	expiresAt, errExpiry := time.Parse(time.RFC3339, loginState.ExpiresAt)
	if errExpiry != nil || !time.Now().UTC().Before(expiresAt) {
		return okEnvelope(authLoginPollResponse{Status: "error", Message: "Kiro organization browser authorization expired"})
	}
	callback, callbackPath, pending, errCallback := readOAuthCallback(req.Host.AuthDir, req.State)
	if pending {
		return okEnvelope(authLoginPollResponse{Status: "pending", Message: "Waiting for Kiro organization authorization"})
	}
	if errCallback != nil {
		return okEnvelope(authLoginPollResponse{Status: "error", Message: errCallback.Error()})
	}
	if callback.Error != "" {
		_ = os.Remove(callbackPath)
		return okEnvelope(authLoginPollResponse{Status: "error", Message: "Kiro organization authorization failed: " + callback.Error})
	}
	if callback.State != req.State || strings.TrimSpace(callback.Code) == "" {
		_ = os.Remove(callbackPath)
		return okEnvelope(authLoginPollResponse{Status: "error", Message: "Kiro organization callback state or code is invalid"})
	}
	tokenBody, _ := json.Marshal(map[string]string{
		"clientId": loginState.ClientID, "clientSecret": loginState.ClientSecret, "code": callback.Code,
		"codeVerifier": loginState.CodeVerifier, "redirectUri": loginState.RedirectURI, "grantType": "authorization_code",
	})
	tokenHTTP, errTokenHTTP := hostHTTPDoCall(hostHTTPRequest{
		HostCallbackID: req.HostCallbackID, Method: http.MethodPost,
		URL:     "https://oidc." + loginState.Region + ".amazonaws.com/token",
		Headers: http.Header{"Content-Type": []string{"application/json"}}, Body: tokenBody,
	})
	if errTokenHTTP != nil {
		return okEnvelope(authLoginPollResponse{Status: "pending", Message: "Retrying Kiro organization token exchange"})
	}
	_ = os.Remove(callbackPath)
	if tokenHTTP.StatusCode < 200 || tokenHTTP.StatusCode >= 300 {
		return okEnvelope(authLoginPollResponse{Status: "error", Message: upstreamStatusError("Kiro organization token exchange failed", tokenHTTP.StatusCode, tokenHTTP.Body).Error()})
	}
	var token oidcTokenResponse
	if errDecode := json.Unmarshal(tokenHTTP.Body, &token); errDecode != nil || strings.TrimSpace(token.AccessToken) == "" || strings.TrimSpace(token.RefreshToken) == "" {
		return okEnvelope(authLoginPollResponse{Status: "error", Message: "Kiro organization token exchange returned incomplete credentials"})
	}
	if token.ExpiresIn <= 0 {
		token.ExpiresIn = 3600
	}
	credential := credential{
		Version: 1, AuthType: "aws_sso_oidc", Mode: "copy", SourceKind: "oauth_organization_browser",
		AccessToken: token.AccessToken, RefreshToken: token.RefreshToken, ClientID: loginState.ClientID,
		ClientSecret: loginState.ClientSecret, SSORegion: loginState.Region, APIRegion: loginState.APIRegion,
		ExpiresAt: time.Now().UTC().Add(time.Duration(token.ExpiresIn) * time.Second).Format(time.RFC3339),
		Scopes:    append([]string(nil), loginState.Scopes...), Label: "Kiro Organization",
	}
	credential, _, _ = ensureProfileARN(credential, req.HostCallbackID)
	auth, errAuth := authDataFromCredential(credential)
	if errAuth != nil {
		return nil, errAuth
	}
	return okEnvelope(authLoginPollResponse{Status: "success", Auth: auth})
}

func decodeIDCBrowserLoginState(metadata map[string]any) (idcBrowserLoginState, error) {
	var state idcBrowserLoginState
	raw, errMarshal := json.Marshal(metadata)
	if errMarshal != nil {
		return state, errMarshal
	}
	if errUnmarshal := json.Unmarshal(raw, &state); errUnmarshal != nil {
		return state, errUnmarshal
	}
	if strings.TrimSpace(state.APIRegion) == "" {
		state.APIRegion = configuredAPIRegion(loadedConfig())
	}
	if state.Version != 1 || state.LoginMode != "organization-browser" || strings.TrimSpace(state.State) == "" || strings.TrimSpace(state.CodeVerifier) == "" || strings.TrimSpace(state.ClientID) == "" || strings.TrimSpace(state.ClientSecret) == "" || strings.TrimSpace(state.Region) == "" || strings.TrimSpace(state.RedirectURI) == "" {
		return state, fmt.Errorf("incomplete organization browser login state")
	}
	return state, nil
}

func readOAuthCallback(authDir, state string) (oauthCallbackPayload, string, bool, error) {
	var callback oauthCallbackPayload
	if strings.TrimSpace(authDir) == "" {
		return callback, "", false, fmt.Errorf("CPA auth directory is unavailable for the Kiro callback")
	}
	callbackPath := filepath.Join(authDir, ".oauth-"+providerID+"-"+state+".oauth")
	callbackRaw, errRead := os.ReadFile(callbackPath)
	if os.IsNotExist(errRead) {
		return callback, callbackPath, true, nil
	}
	if errRead != nil {
		return callback, callbackPath, false, fmt.Errorf("Kiro OAuth callback could not be read")
	}
	if errDecode := json.Unmarshal(callbackRaw, &callback); errDecode != nil {
		_ = os.Remove(callbackPath)
		return callback, callbackPath, false, fmt.Errorf("Kiro OAuth callback is invalid")
	}
	callback.Code = strings.TrimSpace(callback.Code)
	callback.State = strings.TrimSpace(callback.State)
	callback.Error = strings.TrimSpace(callback.Error)
	return callback, callbackPath, false, nil
}

func startBrowserLogin(raw []byte) ([]byte, error) {
	var req authLoginStartRequest
	if errUnmarshal := json.Unmarshal(raw, &req); errUnmarshal != nil {
		return nil, errUnmarshal
	}
	if req.Provider != "" && !strings.EqualFold(req.Provider, providerID) {
		return errorEnvelope("invalid_provider", "Kiro login received an unexpected provider", false, http.StatusBadRequest), nil
	}

	verifier, challenge, errPKCE := generateBrowserPKCE()
	if errPKCE != nil {
		return pluginErrorEnvelope(statusError{Code: "login_random_error", Message: "Kiro PKCE generation failed", HTTPStatus: http.StatusInternalServerError, Cause: errPKCE}), nil
	}
	state := randomID()
	config := loadedConfig()
	redirectURI := strings.TrimSpace(config.BrowserRedirectURI)
	signInURL, errURL := url.Parse(strings.TrimSpace(config.BrowserSignInURL))
	if errURL != nil || signInURL.Scheme != "https" || signInURL.Host == "" {
		return errorEnvelope("invalid_login_config", "Kiro browser sign-in URL is invalid", false, http.StatusInternalServerError), nil
	}
	query := signInURL.Query()
	query.Set("state", state)
	query.Set("code_challenge", challenge)
	query.Set("code_challenge_method", "S256")
	query.Set("redirect_uri", redirectURI)
	query.Set("redirect_from", "kirocli")
	signInURL.RawQuery = query.Encode()

	expiresAt := time.Now().UTC().Add(10 * time.Minute)
	loginState := browserLoginState{
		Version:      1,
		LoginMode:    defaultLoginMode,
		State:        state,
		CodeVerifier: verifier,
		RedirectURI:  redirectURI,
		TokenURL:     strings.TrimSpace(config.DesktopTokenURL),
		APIRegion:    configuredAPIRegion(config),
		ExpiresAt:    expiresAt.Format(time.RFC3339),
	}
	metadataRaw, _ := json.Marshal(loginState)
	var metadata map[string]any
	_ = json.Unmarshal(metadataRaw, &metadata)
	storeBrowserLoginSession(loginState)
	return okEnvelope(authLoginStartResponse{
		Provider: providerID, URL: signInURL.String(), State: state, ExpiresAt: expiresAt, Metadata: metadata,
	})
}

func generateBrowserPKCE() (string, string, error) {
	data := make([]byte, 32)
	if _, errRead := rand.Read(data); errRead != nil {
		return "", "", errRead
	}
	verifier := base64.RawURLEncoding.EncodeToString(data)
	digest := sha256.Sum256([]byte(verifier))
	return verifier, base64.RawURLEncoding.EncodeToString(digest[:]), nil
}

func pollBrowserLoginRequest(req authLoginPollRequest) ([]byte, error) {
	loginState, errState := decodeBrowserLoginState(req.Metadata)
	if errState != nil || strings.TrimSpace(req.State) == "" || loginState.State != req.State {
		return okEnvelope(authLoginPollResponse{Status: "error", Message: "Kiro browser login state is invalid or expired"})
	}
	expiresAt, errExpiry := time.Parse(time.RFC3339, loginState.ExpiresAt)
	if errExpiry != nil || !time.Now().UTC().Before(expiresAt) {
		clearBrowserLoginSession(req.State)
		return okEnvelope(authLoginPollResponse{Status: "error", Message: "Kiro browser authorization expired"})
	}
	if deviceState, exists := browserDeviceContinuation(req.State); exists {
		metadataRaw, _ := json.Marshal(deviceState)
		var metadata map[string]any
		_ = json.Unmarshal(metadataRaw, &metadata)
		req.Metadata = metadata
		return pollDeviceLoginRequest(req)
	}
	callback, callbackPath, pending, errCallback := browserCallback(req.Host.AuthDir, req.State)
	if pending {
		return okEnvelope(authLoginPollResponse{Status: "pending", Message: "Waiting for Kiro browser authorization"})
	}
	if errCallback != nil {
		return okEnvelope(authLoginPollResponse{Status: "error", Message: errCallback.Error()})
	}
	if strings.TrimSpace(callback.Error) != "" {
		_ = os.Remove(callbackPath)
		clearBrowserLoginSession(req.State)
		return okEnvelope(authLoginPollResponse{Status: "error", Message: "Kiro authorization failed: " + strings.TrimSpace(callback.Error)})
	}
	if callback.State != req.State || strings.TrimSpace(callback.Code) == "" {
		_ = os.Remove(callbackPath)
		clearBrowserLoginSession(req.State)
		return okEnvelope(authLoginPollResponse{Status: "error", Message: "Kiro OAuth callback state or code is invalid"})
	}

	tokenBody, _ := json.Marshal(map[string]string{
		"code": callback.Code, "code_verifier": loginState.CodeVerifier, "redirect_uri": loginState.RedirectURI,
	})
	tokenHTTP, errTokenHTTP := hostHTTPDoCall(hostHTTPRequest{
		HostCallbackID: req.HostCallbackID,
		Method:         http.MethodPost,
		URL:            loginState.TokenURL,
		Headers: http.Header{
			"Content-Type": []string{"application/json"},
			"Accept":       []string{"application/json, text/plain, */*"},
			"User-Agent":   []string{"Kiro-CLI"},
		},
		Body: tokenBody,
	})
	if errTokenHTTP != nil {
		return okEnvelope(authLoginPollResponse{Status: "pending", Message: "Retrying Kiro token exchange"})
	}
	_ = os.Remove(callbackPath)
	if tokenHTTP.StatusCode < 200 || tokenHTTP.StatusCode >= 300 {
		clearBrowserLoginSession(req.State)
		return okEnvelope(authLoginPollResponse{Status: "error", Message: upstreamStatusError("Kiro browser token exchange failed", tokenHTTP.StatusCode, tokenHTTP.Body).Error()})
	}
	var token struct {
		AccessToken  string `json:"accessToken"`
		RefreshToken string `json:"refreshToken"`
		ProfileARN   string `json:"profileArn"`
		ExpiresIn    int    `json:"expiresIn"`
	}
	if errDecode := json.Unmarshal(tokenHTTP.Body, &token); errDecode != nil || strings.TrimSpace(token.AccessToken) == "" || strings.TrimSpace(token.RefreshToken) == "" {
		clearBrowserLoginSession(req.State)
		return okEnvelope(authLoginPollResponse{Status: "error", Message: "Kiro browser token exchange returned incomplete credentials"})
	}
	if token.ExpiresIn <= 0 {
		token.ExpiresIn = 3600
	}
	apiRegion := strings.TrimSpace(loginState.APIRegion)
	if apiRegion == "" {
		apiRegion = regionFromARN(token.ProfileARN)
	}
	if apiRegion == "" {
		apiRegion = defaultRegion
	}
	credential := credential{
		Version: 1, Mode: "copy", SourceKind: "oauth_browser", AccessToken: token.AccessToken,
		RefreshToken: token.RefreshToken, ProfileARN: token.ProfileARN, SSORegion: defaultRegion,
		APIRegion: apiRegion, ExpiresAt: time.Now().UTC().Add(time.Duration(token.ExpiresIn) * time.Second).Format(time.RFC3339),
		Label: "Kiro Browser Account",
	}
	credential, _, _ = ensureProfileARN(credential, req.HostCallbackID)
	auth, errAuth := authDataFromCredential(credential)
	if errAuth != nil {
		clearBrowserLoginSession(req.State)
		return nil, errAuth
	}
	clearBrowserLoginSession(req.State)
	return okEnvelope(authLoginPollResponse{Status: "success", Auth: auth})
}

func decodeBrowserLoginState(metadata map[string]any) (browserLoginState, error) {
	var state browserLoginState
	raw, errMarshal := json.Marshal(metadata)
	if errMarshal != nil {
		return state, errMarshal
	}
	if errUnmarshal := json.Unmarshal(raw, &state); errUnmarshal != nil {
		return state, errUnmarshal
	}
	if state.Version != 1 || state.LoginMode != defaultLoginMode || strings.TrimSpace(state.State) == "" || strings.TrimSpace(state.CodeVerifier) == "" || strings.TrimSpace(state.RedirectURI) == "" || strings.TrimSpace(state.TokenURL) == "" {
		return state, fmt.Errorf("incomplete browser login state")
	}
	return state, nil
}

func startDeviceLogin(raw []byte) ([]byte, error) {
	var req authLoginStartRequest
	if errUnmarshal := json.Unmarshal(raw, &req); errUnmarshal != nil {
		return nil, errUnmarshal
	}
	if req.Provider != "" && !strings.EqualFold(req.Provider, providerID) {
		return errorEnvelope("invalid_provider", "Kiro login received an unexpected provider", false, http.StatusBadRequest), nil
	}

	config := loadedConfig()
	region := jsonx.NonEmpty(strings.TrimSpace(config.SSORegion), defaultRegion)
	apiRegion := configuredAPIRegion(config)
	startURL := jsonx.NonEmpty(strings.TrimSpace(config.SSOStartURL), defaultSSOStartURL)
	state := randomID()
	loginState, loginURL, expiresAt, errStart := beginDeviceAuthorization(req.HostCallbackID, state, startURL, region, apiRegion)
	if errStart != nil {
		return pluginErrorEnvelope(errStart), nil
	}
	metadataRaw, _ := json.Marshal(loginState)
	var metadata map[string]any
	_ = json.Unmarshal(metadataRaw, &metadata)

	return okEnvelope(authLoginStartResponse{
		Provider:  providerID,
		URL:       loginURL,
		State:     state,
		ExpiresAt: expiresAt,
		Metadata:  metadata,
	})
}

func beginDeviceAuthorization(callbackID, state, startURL, region, apiRegion string) (deviceLoginState, string, time.Time, error) {
	var loginState deviceLoginState
	startURL = strings.TrimRight(strings.TrimSpace(startURL), "/")
	region = strings.ToLower(strings.TrimSpace(region))
	apiRegion = jsonx.NonEmpty(strings.ToLower(strings.TrimSpace(apiRegion)), defaultRegion)
	if errValidate := validateOrganizationParameters(startURL, region); errValidate != nil {
		return loginState, "", time.Time{}, errValidate
	}
	baseURL := "https://oidc." + region + ".amazonaws.com"
	registrationBody, _ := json.Marshal(map[string]any{
		"clientName": oidcClientName,
		"clientType": oidcClientType,
		"scopes":     defaultOIDCScopes,
		"grantTypes": []string{deviceGrantType, "refresh_token"},
		"issuerUrl":  startURL,
	})
	registrationHTTP, errRegistrationHTTP := hostHTTPDoCall(hostHTTPRequest{
		HostCallbackID: callbackID, Method: http.MethodPost, URL: baseURL + "/client/register",
		Headers: http.Header{"Content-Type": []string{"application/json"}}, Body: registrationBody,
	})
	if errRegistrationHTTP != nil {
		return loginState, "", time.Time{}, statusError{Code: "login_network_error", Message: "Kiro OIDC client registration failed", Retryable: true, HTTPStatus: http.StatusBadGateway, Cause: errRegistrationHTTP}
	}
	if registrationHTTP.StatusCode < 200 || registrationHTTP.StatusCode >= 300 {
		return loginState, "", time.Time{}, upstreamStatusError("Kiro OIDC client registration failed", registrationHTTP.StatusCode, registrationHTTP.Body)
	}
	var registration oidcClientRegistrationResponse
	if errDecode := json.Unmarshal(registrationHTTP.Body, &registration); errDecode != nil {
		return loginState, "", time.Time{}, statusError{Code: "invalid_login_response", Message: "Kiro OIDC client registration returned invalid JSON", HTTPStatus: http.StatusBadGateway, Cause: errDecode}
	}
	if strings.TrimSpace(registration.ClientID) == "" || strings.TrimSpace(registration.ClientSecret) == "" {
		return loginState, "", time.Time{}, statusError{Code: "invalid_login_response", Message: "Kiro OIDC client registration did not return credentials", HTTPStatus: http.StatusBadGateway}
	}
	deviceBody, _ := json.Marshal(map[string]string{
		"clientId": registration.ClientID, "clientSecret": registration.ClientSecret, "startUrl": startURL,
	})
	deviceHTTP, errDeviceHTTP := hostHTTPDoCall(hostHTTPRequest{
		HostCallbackID: callbackID, Method: http.MethodPost, URL: baseURL + "/device_authorization",
		Headers: http.Header{"Content-Type": []string{"application/json"}}, Body: deviceBody,
	})
	if errDeviceHTTP != nil {
		return loginState, "", time.Time{}, statusError{Code: "login_network_error", Message: "Kiro device authorization failed", Retryable: true, HTTPStatus: http.StatusBadGateway, Cause: errDeviceHTTP}
	}
	if deviceHTTP.StatusCode < 200 || deviceHTTP.StatusCode >= 300 {
		return loginState, "", time.Time{}, upstreamStatusError("Kiro device authorization failed", deviceHTTP.StatusCode, deviceHTTP.Body)
	}
	var device oidcDeviceAuthorizationResponse
	if errDecode := json.Unmarshal(deviceHTTP.Body, &device); errDecode != nil {
		return loginState, "", time.Time{}, statusError{Code: "invalid_login_response", Message: "Kiro device authorization returned invalid JSON", HTTPStatus: http.StatusBadGateway, Cause: errDecode}
	}
	loginURL := strings.TrimSpace(device.VerificationURIComplete)
	if loginURL == "" {
		loginURL = strings.TrimSpace(device.VerificationURI)
	}
	if loginURL == "" || strings.TrimSpace(device.DeviceCode) == "" {
		return loginState, "", time.Time{}, statusError{Code: "invalid_login_response", Message: "Kiro device authorization did not return a login URL", HTTPStatus: http.StatusBadGateway}
	}
	if device.ExpiresIn <= 0 {
		device.ExpiresIn = 600
	}
	if device.Interval <= 0 {
		device.Interval = 5
	}
	expiresAt := time.Now().UTC().Add(time.Duration(device.ExpiresIn) * time.Second)
	loginState = deviceLoginState{
		Version: 1, LoginMode: "aws-device", State: state, ClientID: registration.ClientID,
		ClientSecret: registration.ClientSecret, ClientSecretExpiresAt: registration.ClientSecretExpiresAt,
		DeviceCode: device.DeviceCode, UserCode: device.UserCode, Region: region, APIRegion: apiRegion, StartURL: startURL,
		ExpiresAt: expiresAt.Format(time.RFC3339), Interval: device.Interval,
		Scopes: append([]string(nil), defaultOIDCScopes...),
	}
	setNextDeviceLoginPoll(state, time.Now().UTC().Add(time.Duration(device.Interval)*time.Second))
	return loginState, loginURL, expiresAt, nil
}

func pollDeviceLoginRequest(req authLoginPollRequest) ([]byte, error) {
	loginState, errState := decodeDeviceLoginState(req.Metadata)
	if errState != nil || strings.TrimSpace(req.State) == "" || loginState.State != req.State {
		clearDeviceLoginPoll(req.State)
		clearBrowserLoginSession(req.State)
		return okEnvelope(authLoginPollResponse{Status: "error", Message: "Kiro login state is invalid or expired"})
	}
	expiresAt, errExpiry := time.Parse(time.RFC3339, loginState.ExpiresAt)
	if errExpiry != nil || !time.Now().UTC().Before(expiresAt) {
		clearDeviceLoginPoll(req.State)
		clearBrowserLoginSession(req.State)
		return okEnvelope(authLoginPollResponse{Status: "error", Message: "Kiro device authorization expired"})
	}
	if !deviceLoginPollDue(req.State, time.Now().UTC()) {
		return okEnvelope(authLoginPollResponse{Status: "pending"})
	}

	interval := loginState.Interval
	if interval <= 0 {
		interval = 5
	}
	setNextDeviceLoginPoll(req.State, time.Now().UTC().Add(time.Duration(interval)*time.Second))
	tokenBody, _ := json.Marshal(map[string]string{
		"clientId":     loginState.ClientID,
		"clientSecret": loginState.ClientSecret,
		"deviceCode":   loginState.DeviceCode,
		"grantType":    deviceGrantType,
	})
	tokenHTTP, errTokenHTTP := hostHTTPDoCall(hostHTTPRequest{
		HostCallbackID: req.HostCallbackID,
		Method:         http.MethodPost,
		URL:            "https://oidc." + loginState.Region + ".amazonaws.com/token",
		Headers:        http.Header{"Content-Type": []string{"application/json"}},
		Body:           tokenBody,
	})
	if errTokenHTTP != nil {
		return okEnvelope(authLoginPollResponse{Status: "pending", Message: "Waiting for Kiro authorization"})
	}
	if tokenHTTP.StatusCode < 200 || tokenHTTP.StatusCode >= 300 {
		code := normalizeOIDCErrorCode(tokenHTTP.Body)
		switch code {
		case "authorizationpending", "authorizationpendingexception":
			return okEnvelope(authLoginPollResponse{Status: "pending"})
		case "slowdown", "slowdownexception":
			setNextDeviceLoginPoll(req.State, time.Now().UTC().Add(time.Duration(interval+5)*time.Second))
			return okEnvelope(authLoginPollResponse{Status: "pending"})
		case "expiredtoken", "expiredtokenexception":
			clearDeviceLoginPoll(req.State)
			clearBrowserLoginSession(req.State)
			return okEnvelope(authLoginPollResponse{Status: "error", Message: "Kiro device authorization expired"})
		case "accessdenied", "accessdeniedexception":
			clearDeviceLoginPoll(req.State)
			clearBrowserLoginSession(req.State)
			return okEnvelope(authLoginPollResponse{Status: "error", Message: "Kiro authorization was denied"})
		default:
			clearDeviceLoginPoll(req.State)
			clearBrowserLoginSession(req.State)
			return okEnvelope(authLoginPollResponse{Status: "error", Message: "Kiro token exchange failed"})
		}
	}

	var token oidcTokenResponse
	if errDecode := json.Unmarshal(tokenHTTP.Body, &token); errDecode != nil {
		clearDeviceLoginPoll(req.State)
		clearBrowserLoginSession(req.State)
		return okEnvelope(authLoginPollResponse{Status: "error", Message: "Kiro token exchange returned invalid JSON"})
	}
	if strings.TrimSpace(token.AccessToken) == "" || strings.TrimSpace(token.RefreshToken) == "" {
		clearDeviceLoginPoll(req.State)
		clearBrowserLoginSession(req.State)
		return okEnvelope(authLoginPollResponse{Status: "error", Message: "Kiro token exchange returned incomplete credentials"})
	}
	if token.ExpiresIn <= 0 {
		token.ExpiresIn = 3600
	}
	label := "Kiro Builder ID"
	if !strings.EqualFold(strings.TrimRight(loginState.StartURL, "/"), strings.TrimRight(defaultSSOStartURL, "/")) {
		label = "Kiro Organization"
	}
	credential := credential{
		Version:      1,
		AuthType:     "aws_sso_oidc",
		Mode:         "copy",
		SourceKind:   "oauth_device",
		AccessToken:  token.AccessToken,
		RefreshToken: token.RefreshToken,
		ClientID:     loginState.ClientID,
		ClientSecret: loginState.ClientSecret,
		SSORegion:    loginState.Region,
		APIRegion:    loginState.APIRegion,
		ExpiresAt:    time.Now().UTC().Add(time.Duration(token.ExpiresIn) * time.Second).Format(time.RFC3339),
		Scopes:       append([]string(nil), loginState.Scopes...),
		Label:        label,
	}
	credential, _, _ = ensureProfileARN(credential, req.HostCallbackID)
	auth, errAuth := authDataFromCredential(credential)
	if errAuth != nil {
		clearDeviceLoginPoll(req.State)
		clearBrowserLoginSession(req.State)
		return nil, errAuth
	}
	clearDeviceLoginPoll(req.State)
	clearBrowserLoginSession(req.State)
	return okEnvelope(authLoginPollResponse{Status: "success", Auth: auth})
}

func decodeDeviceLoginState(metadata map[string]any) (deviceLoginState, error) {
	var state deviceLoginState
	raw, errMarshal := json.Marshal(metadata)
	if errMarshal != nil {
		return state, errMarshal
	}
	if errUnmarshal := json.Unmarshal(raw, &state); errUnmarshal != nil {
		return state, errUnmarshal
	}
	if strings.TrimSpace(state.APIRegion) == "" {
		state.APIRegion = configuredAPIRegion(loadedConfig())
	}
	if state.Version != 1 || strings.TrimSpace(state.ClientID) == "" || strings.TrimSpace(state.ClientSecret) == "" || strings.TrimSpace(state.DeviceCode) == "" || strings.TrimSpace(state.Region) == "" {
		return state, fmt.Errorf("incomplete device login state")
	}
	return state, nil
}

func configuredAPIRegion(config pluginConfig) string {
	return jsonx.NonEmpty(strings.ToLower(strings.TrimSpace(config.APIRegion)), defaultRegion)
}

func normalizeOIDCErrorCode(body []byte) string {
	var object map[string]any
	if json.Unmarshal(body, &object) != nil {
		return ""
	}
	value := jsonx.String(object, "error", "code", "__type")
	if separator := strings.LastIndexAny(value, "#:"); separator >= 0 {
		value = value[separator+1:]
	}
	var normalized strings.Builder
	for _, character := range strings.ToLower(value) {
		if character >= 'a' && character <= 'z' || character >= '0' && character <= '9' {
			normalized.WriteRune(character)
		}
	}
	return normalized.String()
}

func deviceLoginPollDue(state string, now time.Time) bool {
	deviceLoginPolls.Lock()
	defer deviceLoginPolls.Unlock()
	next, exists := deviceLoginPolls.next[state]
	return !exists || !now.Before(next)
}

func setNextDeviceLoginPoll(state string, next time.Time) {
	deviceLoginPolls.Lock()
	defer deviceLoginPolls.Unlock()
	deviceLoginPolls.next[state] = next
}

func clearDeviceLoginPoll(state string) {
	deviceLoginPolls.Lock()
	defer deviceLoginPolls.Unlock()
	delete(deviceLoginPolls.next, state)
}

func storeBrowserLoginSession(state browserLoginState) {
	browserLoginSessions.Lock()
	defer browserLoginSessions.Unlock()
	browserLoginSessions.sessions[state.State] = browserLoginSession{LoginState: state}
}

func storeBrowserCallback(state string, callback oauthCallbackPayload) bool {
	browserLoginSessions.Lock()
	defer browserLoginSessions.Unlock()
	session, exists := browserLoginSessions.sessions[state]
	if !exists {
		return false
	}
	callbackCopy := callback
	session.Callback = &callbackCopy
	browserLoginSessions.sessions[state] = session
	return true
}

func storeBrowserDeviceContinuation(state string, device deviceLoginState) bool {
	browserLoginSessions.Lock()
	defer browserLoginSessions.Unlock()
	session, exists := browserLoginSessions.sessions[state]
	if !exists {
		return false
	}
	deviceCopy := device
	session.Device = &deviceCopy
	browserLoginSessions.sessions[state] = session
	return true
}

func browserDeviceContinuation(state string) (deviceLoginState, bool) {
	browserLoginSessions.Lock()
	defer browserLoginSessions.Unlock()
	session, exists := browserLoginSessions.sessions[state]
	if !exists || session.Device == nil {
		return deviceLoginState{}, false
	}
	return *session.Device, true
}

func browserCallback(authDir, state string) (oauthCallbackPayload, string, bool, error) {
	browserLoginSessions.Lock()
	session, exists := browserLoginSessions.sessions[state]
	if exists && session.Callback != nil {
		callback := *session.Callback
		browserLoginSessions.Unlock()
		return callback, "", false, nil
	}
	browserLoginSessions.Unlock()
	return readOAuthCallback(authDir, state)
}

func browserLoginSessionForState(state string) (browserLoginSession, bool) {
	browserLoginSessions.Lock()
	defer browserLoginSessions.Unlock()
	session, exists := browserLoginSessions.sessions[state]
	return session, exists
}

func clearBrowserLoginSession(state string) {
	browserLoginSessions.Lock()
	defer browserLoginSessions.Unlock()
	delete(browserLoginSessions.sessions, state)
}

func validateOrganizationParameters(startURL, region string) error {
	parsed, errParse := url.Parse(strings.TrimSpace(startURL))
	if errParse != nil || parsed.Scheme != "https" || parsed.Hostname() == "" {
		return statusError{Code: "invalid_callback", Message: "Kiro organization callback returned an invalid issuer URL", HTTPStatus: http.StatusBadRequest}
	}
	host := strings.ToLower(parsed.Hostname())
	if host != "view.awsapps.com" && !strings.HasSuffix(host, ".awsapps.com") {
		return statusError{Code: "invalid_callback", Message: "Kiro organization issuer must be an AWS IAM Identity Center URL", HTTPStatus: http.StatusBadRequest}
	}
	if !awsRegionPattern.MatchString(strings.ToLower(strings.TrimSpace(region))) {
		return statusError{Code: "invalid_callback", Message: "Kiro organization callback returned an invalid AWS region", HTTPStatus: http.StatusBadRequest}
	}
	return nil
}
