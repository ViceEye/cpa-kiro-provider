package cline

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"time"
)

// buildAPIHeaders assembles the identity/auth headers Cline expects.
func buildAPIHeaders(accessToken string, extra map[string]string) map[string]string {
	headers := map[string]string{
		"Accept":        "application/json",
		"HTTP-Referer":  "https://cline.bot",
		"X-Title":       "Cline",
		"X-CLIENT-TYPE": "cline-provider",
	}
	if accessToken != "" {
		token := accessToken
		if !strings.HasPrefix(token, "workos:") {
			token = "workos:" + token
		}
		headers["Authorization"] = "Bearer " + token
	}
	for key, value := range extra {
		headers[key] = value
	}
	return headers
}

func decodeCredential(raw []byte) (credential, error) {
	var cred credential
	if len(raw) == 0 {
		return cred, fmt.Errorf("Cline auth storage is empty")
	}
	if err := json.Unmarshal(raw, &cred); err != nil {
		return cred, fmt.Errorf("decode Cline auth storage: %w", err)
	}
	cred.Type = pluginProvider
	cred.Kind = TypeMarker
	if cred.Version == 0 {
		cred.Version = 1
	}
	return cred, nil
}

func credentialNeedsRefresh(cred credential) bool {
	if strings.TrimSpace(cred.AccessToken) == "" {
		return true
	}
	expires, err := time.Parse(time.RFC3339, cred.ExpiresAt)
	if err != nil {
		return false // no expiry known; rely on 401 retry
	}
	return expires.Before(time.Now().UTC().Add(5 * time.Minute))
}

func credentialID(cred credential) string {
	identity := cred.Email + "\x00" + cred.RefreshToken
	sum := sha256.Sum256([]byte(identity))
	return "cline-" + hex.EncodeToString(sum[:10])
}

// StableCredentialID returns the internal Cline credential ID used by request
// statistics and management data. It prefers the persisted ID so rotating
// refresh tokens do not split one account into multiple records.
func StableCredentialID(raw []byte) string {
	cred, err := decodeCredential(raw)
	if err != nil {
		return ""
	}
	if id := strings.TrimSpace(cred.AuthID); id != "" {
		return id
	}
	if strings.TrimSpace(cred.Email) == "" && strings.TrimSpace(cred.RefreshToken) == "" {
		return ""
	}
	return credentialID(cred)
}

func authDataFromCredential(cred credential) (authData, error) {
	if strings.TrimSpace(cred.RefreshToken) == "" {
		return authData{}, fmt.Errorf("Cline credential has no refresh token")
	}
	if cred.AuthID == "" {
		cred.AuthID = credentialID(cred)
	}
	cred.Type = pluginProvider
	cred.Kind = TypeMarker
	storage, err := json.Marshal(cred)
	if err != nil {
		return authData{}, err
	}
	return authData{
		Provider:    pluginProvider,
		ID:          cred.AuthID,
		FileName:    cred.AuthID + ".json",
		Label:       cred.Email,
		StorageJSON: storage,
		Metadata:    map[string]any{"auth_type": "oauth", "source_kind": "oauth_cline"},
		Attributes:  map[string]string{"auth_provider": pluginProvider},
	}, nil
}

// refreshCredential exchanges the refresh token for a fresh access token.
func refreshCredential(cred credential, callbackID string) (credential, error) {
	if strings.TrimSpace(cred.RefreshToken) == "" {
		return credential{}, statusErr("missing_refresh_token", "Cline refresh token is missing", false, http.StatusUnauthorized)
	}
	body, _ := json.Marshal(map[string]string{
		"refreshToken": cred.RefreshToken,
		"grantType":    "refresh_token",
		"clientType":   "extension",
	})
	response, err := hostHTTP(hostHTTPRequest{
		HostCallbackID: callbackID,
		Method:         http.MethodPost,
		URL:            apiBase + refreshPath,
		Headers:        map[string][]string{"Content-Type": {"application/json"}, "Accept": {"application/json"}},
		Body:           body,
	})
	if err != nil {
		return credential{}, statusErr("refresh_network_error", "Cline token refresh request failed", true, http.StatusBadGateway)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return credential{}, statusErr("refresh_failed", fmt.Sprintf("Cline token refresh failed (%d)", response.StatusCode), response.StatusCode == http.StatusTooManyRequests || response.StatusCode >= 500, http.StatusBadGateway)
	}
	var parsed clineTokenResponse
	if err := json.Unmarshal(response.Body, &parsed); err != nil {
		return credential{}, statusErr("invalid_refresh_response", "Cline token refresh returned invalid JSON", false, http.StatusBadGateway)
	}
	if parsed.Data.AccessToken == "" {
		return credential{}, statusErr("invalid_refresh_response", "Cline token refresh returned no access token", false, http.StatusUnauthorized)
	}
	cred.AccessToken = parsed.Data.AccessToken
	if parsed.Data.RefreshToken != "" {
		cred.RefreshToken = parsed.Data.RefreshToken
	}
	expiresIn := parsed.Data.ExpiresIn
	if expiresIn <= 0 {
		expiresIn = 3600
	}
	if parsed.Data.ExpiresAt != "" {
		cred.ExpiresAt = parsed.Data.ExpiresAt
	} else {
		cred.ExpiresAt = time.Now().UTC().Add(time.Duration(expiresIn-60) * time.Second).Format(time.RFC3339)
	}
	cred.LastRefreshAt = time.Now().UTC().Format(time.RFC3339)
	return cred, nil
}

// parseAuth accepts a raw credential JSON (at minimum {"refresh_token": ...}),
// validates it by refreshing, and returns the connection record.
func ParseAuth(raw []byte) ([]byte, error) {
	var req authParseRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		return nil, err
	}
	if req.Provider != "" && !strings.EqualFold(req.Provider, pluginProvider) {
		return okEnvelope(authParseResponse{Handled: false})
	}
	if len(req.RawJSON) == 0 {
		return okEnvelope(authParseResponse{Handled: false})
	}
	cred, err := decodeCredential(req.RawJSON)
	if err != nil {
		return okEnvelope(authParseResponse{Handled: false})
	}
	if strings.TrimSpace(cred.RefreshToken) == "" {
		return okEnvelope(authParseResponse{Handled: false})
	}
	// Pure parse — no network. The token is refreshed lazily on first use
	// (executor/usage) so file scanning never depends on upstream reachability.
	auth, err := authDataFromCredential(cred)
	if err != nil {
		return nil, err
	}
	// Single-credential files take the host's file-based record ID so the
	// scanned record and host.auth.save upserts agree (same rule as kiro).
	physicalName := strings.TrimSpace(req.FileName)
	if physicalName == "" {
		physicalName = filepath.Base(strings.TrimSpace(req.Path))
	}
	if physicalName != "" {
		auth.ID = ""
		auth.FileName = physicalName
	}
	return okEnvelope(authParseResponse{Handled: true, Auth: auth, Auths: []authData{auth}})
}

// refreshAuth refreshes an existing credential on CPA's schedule.
func RefreshAuth(raw []byte) ([]byte, error) {
	var req authRefreshRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		return nil, err
	}
	cred, err := decodeCredential(req.StorageJSON)
	if err != nil {
		return errorEnvelope("invalid_auth", err.Error(), false, http.StatusUnauthorized), nil
	}
	if validID := validAuthID(req.AuthID); validID != "" {
		cred.AuthID = validID
	}
	refreshed, err := refreshCredential(cred, req.HostCallbackID)
	if err != nil {
		return pluginError(err), nil
	}
	auth, err := authDataFromCredential(refreshed)
	if err != nil {
		return nil, err
	}
	// Empty ID/FileName: the host keeps its existing record identity.
	auth.ID = ""
	auth.FileName = ""
	for key, value := range req.Attributes {
		if _, exists := auth.Attributes[key]; !exists {
			auth.Attributes[key] = value
		}
	}
	return okEnvelope(authRefreshResponse{Auth: auth, NextRefreshAfter: nextRefreshTime(refreshed)})
}

func validAuthID(value string) string {
	value = strings.TrimSpace(value)
	if len(value) != len("cline-")+20 || !strings.HasPrefix(value, "cline-") {
		return ""
	}
	for _, char := range value[len("cline-"):] {
		if (char < '0' || char > '9') && (char < 'a' || char > 'f') {
			return ""
		}
	}
	return value
}

func nextRefreshTime(cred credential) time.Time {
	expires, err := time.Parse(time.RFC3339, cred.ExpiresAt)
	if err != nil || expires.IsZero() {
		return time.Now().UTC()
	}
	refreshAt := expires.Add(-5 * time.Minute)
	if refreshAt.Before(time.Now().UTC()) {
		return time.Now().UTC()
	}
	return refreshAt
}
