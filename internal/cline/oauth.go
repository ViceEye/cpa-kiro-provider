package cline

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// Cline browser OAuth flow (mirrors the official client):
//  1. Authorize URL: /api/v1/auth/authorize?client_type=extension&callback_url=<cb>&redirect_uri=<cb>
//  2. The user completes login; Cline redirects to the callback URL (a local
//     port nobody listens on) — the user copies that URL.
//  3. Token exchange: POST /api/v1/auth/token
//     {grant_type: "authorization_code", code, client_type: "extension",
//      redirect_uri: <cb>, provider: <idp>}
//     → {success, data: {accessToken, refreshToken, expiresAt, userInfo}}.

type clineLoginSession struct {
	CallbackURL string
	ExpiresAt   time.Time
}

var clineLoginSessions = struct {
	sync.Mutex
	items map[string]clineLoginSession
}{items: make(map[string]clineLoginSession)}

// LoginStart returns the Cline authorize URL for the manual-callback flow.
func LoginStart(raw []byte) ([]byte, error) {
	var req struct {
		Provider string         `json:"Provider"`
		Metadata map[string]any `json:"Metadata"`
	}
	_ = json.Unmarshal(raw, &req)

	state := randomID()
	callbackURL := strings.TrimSpace(stringFromMetadata(req.Metadata, "callback_url"))
	if callbackURL == "" {
		callbackURL = "http://localhost:3128/auth"
	}

	clineLoginSessions.Lock()
	clineLoginSessions.items[state] = clineLoginSession{
		CallbackURL: callbackURL,
		ExpiresAt:   time.Now().UTC().Add(15 * time.Minute),
	}
	clineLoginSessions.Unlock()

	authorize, err := url.Parse(apiBase + "/api/v1/auth/authorize")
	if err != nil {
		return errorEnvelope("login_config_error", "Cline authorize URL is invalid", false, http.StatusInternalServerError), nil
	}
	query := authorize.Query()
	query.Set("client_type", "extension")
	query.Set("callback_url", callbackURL)
	query.Set("redirect_uri", callbackURL)
	query.Set("state", state)
	authorize.RawQuery = query.Encode()

	return okEnvelope(map[string]any{
		"Provider":  pluginProvider,
		"URL":       authorize.String(),
		"State":     state,
		"ExpiresAt": time.Now().UTC().Add(15 * time.Minute),
		"Metadata": map[string]any{
			"login_mode":   "cline",
			"callback_url": callbackURL,
			"state":        state,
		},
	})
}

// LoginPoll completes the login: it accepts the pasted callback URL (or raw
// code), exchanges it for tokens, persists the credential via host.auth.save
// and returns the connection record for CPA's own login flow.
func LoginPoll(raw []byte) ([]byte, error) {
	var req struct {
		Provider string         `json:"Provider"`
		State    string         `json:"State"`
		Metadata map[string]any `json:"Metadata"`
		Body     map[string]any `json:"Body"`
	}
	_ = json.Unmarshal(raw, &req)

	state := strings.TrimSpace(req.State)
	if state == "" {
		state = strings.TrimSpace(stringFromMetadata(req.Metadata, "state"))
	}
	pasted := strings.TrimSpace(stringFromMetadata(req.Metadata, "callback_url"))
	if pasted == "" {
		pasted = strings.TrimSpace(stringFromMetadata(req.Body, "callback_url"))
	}

	clineLoginSessions.Lock()
	session, ok := clineLoginSessions.items[state]
	clineLoginSessions.Unlock()
	if !ok || state == "" {
		return okEnvelope(map[string]any{"Status": "error", "Message": "Cline login state is unknown or expired; start login again"})
	}
	if time.Now().UTC().After(session.ExpiresAt) {
		clineLoginSessions.Lock()
		delete(clineLoginSessions.items, state)
		clineLoginSessions.Unlock()
		return okEnvelope(map[string]any{"Status": "error", "Message": "Cline login expired; start login again"})
	}
	if pasted == "" {
		return okEnvelope(map[string]any{"Status": "pending", "Message": "Waiting for the callback URL to be pasted"})
	}

	code, idp, err := parseAuthorizationInput(pasted)
	if err != nil {
		return okEnvelope(map[string]any{"Status": "error", "Message": err.Error()})
	}

	cred, err := exchangeAuthorizationCode(code, session.CallbackURL, idp)
	if err != nil {
		return okEnvelope(map[string]any{"Status": "error", "Message": err.Error()})
	}

	clineLoginSessions.Lock()
	delete(clineLoginSessions.items, state)
	clineLoginSessions.Unlock()

	auth, err := authDataFromCredential(cred)
	if err != nil {
		return okEnvelope(map[string]any{"Status": "error", "Message": err.Error()})
	}
	// Persist under the credential's own file name; the host keys the record
	// by file so re-logins stay stable.
	if _, err := callHost("host.auth.save", map[string]any{
		"name": auth.FileName, "json": json.RawMessage(auth.StorageJSON),
	}); err != nil {
		return okEnvelope(map[string]any{"Status": "error", "Message": "credential save failed: " + err.Error()})
	}
	return okEnvelope(map[string]any{
		"Status":  "success",
		"Message": "Cline login completed",
		"Auth":    auth,
	})
}

// parseAuthorizationInput extracts the code and identity provider from a
// pasted redirect URL or bare code.
func parseAuthorizationInput(input string) (code, provider string, err error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return "", "", fmt.Errorf("empty callback input")
	}
	if !strings.Contains(input, "://") {
		return strings.TrimSpace(input), "cline", nil
	}
	parsed, err := url.Parse(input)
	if err != nil {
		return "", "", fmt.Errorf("invalid callback URL: %w", err)
	}
	query := parsed.Query()
	code = strings.TrimSpace(query.Get("code"))
	if code == "" {
		return "", "", fmt.Errorf("callback URL has no code parameter")
	}
	if p := strings.TrimSpace(query.Get("provider")); p != "" {
		provider = p
	} else {
		provider = "cline"
	}
	return code, provider, nil
}

// exchangeAuthorizationCode swaps the authorization code for tokens.
func exchangeAuthorizationCode(code, callbackURL, idp string) (credential, error) {
	var cred credential
	body, _ := json.Marshal(map[string]string{
		"grant_type":   "authorization_code",
		"code":         code,
		"client_type":  "extension",
		"redirect_uri": callbackURL,
		"provider":     idp,
	})
	response, err := hostHTTP(hostHTTPRequest{
		Method:  http.MethodPost,
		URL:     apiBase + "/api/v1/auth/token",
		Headers: map[string][]string{"Content-Type": {"application/json"}, "Accept": {"application/json"}},
		Body:    body,
	})
	if err != nil {
		return cred, fmt.Errorf("token exchange failed: %v", err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return cred, fmt.Errorf("token exchange failed (%d): %s", response.StatusCode, truncateError(response.Body, response.StatusCode))
	}
	var parsed clineTokenResponse
	if err := json.Unmarshal(response.Body, &parsed); err != nil {
		return cred, fmt.Errorf("token exchange returned invalid JSON")
	}
	if parsed.Data.AccessToken == "" || parsed.Data.RefreshToken == "" {
		return cred, fmt.Errorf("token exchange returned incomplete credentials")
	}
	expiresIn := parsed.Data.ExpiresIn
	if expiresIn <= 0 {
		expiresIn = 3600
	}
	expiresAt := parsed.Data.ExpiresAt
	if expiresAt == "" {
		expiresAt = time.Now().UTC().Add(time.Duration(expiresIn-60) * time.Second).Format(time.RFC3339)
	}
	return credential{
		Type:         TypeMarker,
		Version:      1,
		AccessToken:  parsed.Data.AccessToken,
		RefreshToken: parsed.Data.RefreshToken,
		ExpiresAt:    expiresAt,
		LastRefreshAt: time.Now().UTC().Format(time.RFC3339),
	}, nil
}

func stringFromMetadata(metadata map[string]any, key string) string {
	if metadata == nil {
		return ""
	}
	if value, ok := metadata[key].(string); ok {
		return value
	}
	return ""
}

var _ = fmt.Sprintf
