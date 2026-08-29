package provider

import (
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/ViceEye/cpa-kiro-provider/internal/jsonx"
)

func parseAuth(raw []byte) ([]byte, error) {
	var req authParseRequest
	if errUnmarshal := json.Unmarshal(raw, &req); errUnmarshal != nil {
		return nil, errUnmarshal
	}
	if req.Provider != "" && !strings.EqualFold(req.Provider, providerID) {
		return okEnvelope(authParseResponse{Handled: false})
	}
	mode := loadedConfig().ImportMode
	var creds []credential
	var errParse error
	if len(req.RawJSON) > 0 {
		creds, errParse = parseCredentialJSON(req.RawJSON, req.Path, mode)
	} else if req.Path != "" {
		creds, errParse = importCredentials(req.Path, mode)
	} else {
		return okEnvelope(authParseResponse{Handled: false})
	}
	if errParse != nil {
		if errParse == errUnrecognizedCredential {
			return okEnvelope(authParseResponse{Handled: false})
		}
		return nil, errParse
	}
	physicalName := strings.TrimSpace(req.FileName)
	if physicalName == "" {
		physicalName = filepath.Base(strings.TrimSpace(req.Path))
	}
	// Single-account files take the host's file-based record ID so the scanned
	// record and the saved record agree, instead of duplicating the manager
	// entry for one physical file. Multi-account files keep per-account
	// content-hash IDs: a path-derived ID would collide across accounts.
	blankIdentity := physicalName != "" && len(creds) == 1
	auths := make([]authData, 0, len(creds))
	for _, cred := range creds {
		auth, errAuth := authDataFromCredential(cred)
		if errAuth != nil {
			return nil, errAuth
		}
		if blankIdentity {
			auth.ID = ""
			auth.FileName = physicalName
		}
		auths = append(auths, auth)
	}
	resp := authParseResponse{Handled: len(auths) > 0, Auths: auths}
	if len(auths) == 1 {
		resp.Auth = auths[0]
	}
	return okEnvelope(resp)
}

func refreshAuth(raw []byte) ([]byte, error) {
	var req authRefreshRequest
	if errUnmarshal := json.Unmarshal(raw, &req); errUnmarshal != nil {
		return nil, errUnmarshal
	}
	cred, errDecode := decodeCredential(req.StorageJSON)
	if errDecode != nil {
		return errorEnvelope("invalid_auth", errDecode.Error(), false, http.StatusUnauthorized), nil
	}
	// The credential's content identity stays content-derived. The host record
	// ID can be a file-based ID and must never be written into the stored JSON.
	if validCredentialID(cred.AuthID) == "" {
		cred.AuthID = credentialID(cred)
	}
	refreshed, errRefresh := refreshCredential(cred, req.HostCallbackID)
	if errRefresh != nil {
		return pluginErrorEnvelope(errRefresh), nil
	}
	refreshed, _, errProfile := ensureProfileARN(refreshed, req.HostCallbackID)
	if errProfile != nil {
		return pluginErrorEnvelope(errProfile), nil
	}
	auth, errAuth := authDataFromCredential(refreshed)
	if errAuth != nil {
		return nil, errAuth
	}
	// Empty ID/FileName tells the host to keep the existing record identity.
	auth.ID = ""
	auth.FileName = ""
	// Re-marshaling the credential drops fields the plugin does not model
	// (disabled, priority, note, ...). Carry them over from the stored JSON.
	var fresh map[string]any
	var stored map[string]any
	if json.Unmarshal(auth.StorageJSON, &fresh) == nil && json.Unmarshal(req.StorageJSON, &stored) == nil {
		for key, value := range stored {
			if _, credentialField := fresh[key]; !credentialField {
				fresh[key] = value
			}
		}
		auth.StorageJSON, _ = json.Marshal(fresh)
	}
	// Preserve host record attributes such as the auth file path.
	for key, value := range req.Attributes {
		if _, exists := auth.Attributes[key]; !exists {
			auth.Attributes[key] = value
		}
	}
	return okEnvelope(authRefreshResponse{Auth: auth, NextRefreshAfter: nextRefreshTime(refreshed)})
}

func refreshCredential(current credential, callbackID string) (credential, error) {
	cred, errReload := reloadReferencedCredential(current)
	if errReload != nil {
		return credential{}, statusError{Code: "credential_reload_failed", Message: errReload.Error(), HTTPStatus: http.StatusUnauthorized}
	}
	if strings.TrimSpace(cred.RefreshToken) == "" {
		return credential{}, statusError{Code: "missing_refresh_token", Message: "Kiro refresh token is missing", HTTPStatus: http.StatusUnauthorized}
	}
	var endpoint string
	var body []byte
	var headers http.Header
	if cred.AuthType == "aws_sso_oidc" || (cred.ClientID != "" && cred.ClientSecret != "") {
		if cred.ClientID == "" || cred.ClientSecret == "" {
			return credential{}, statusError{Code: "missing_oidc_registration", Message: "Kiro AWS SSO client ID or client secret is missing", HTTPStatus: http.StatusUnauthorized}
		}
		endpoint = configuredRegionURL(loadedConfig().OIDCRefreshURL, fmt.Sprintf("https://oidc.%s.amazonaws.com/token", jsonx.NonEmpty(cred.SSORegion, defaultRegion)), cred.SSORegion)
		body, _ = json.Marshal(map[string]any{
			"grantType":    "refresh_token",
			"clientId":     cred.ClientID,
			"clientSecret": cred.ClientSecret,
			"refreshToken": cred.RefreshToken,
		})
		headers = http.Header{"Content-Type": []string{"application/json"}}
	} else {
		endpoint = configuredRegionURL(loadedConfig().DesktopRefreshURL, fmt.Sprintf("https://prod.%s.auth.desktop.kiro.dev/refreshToken", jsonx.NonEmpty(cred.SSORegion, defaultRegion)), cred.SSORegion)
		body, _ = json.Marshal(map[string]string{"refreshToken": cred.RefreshToken})
		headers = http.Header{
			"Content-Type": []string{"application/json"},
			"User-Agent":   []string{"KiroIDE-0.7.45-" + cred.Fingerprint},
		}
	}
	resp, errHTTP := hostHTTPDoCall(hostHTTPRequest{HostCallbackID: callbackID, Method: http.MethodPost, URL: endpoint, Headers: headers, Body: body})
	if errHTTP != nil {
		return credential{}, statusError{Code: "refresh_network_error", Message: "Kiro token refresh request failed", Retryable: true, HTTPStatus: http.StatusBadGateway, Cause: errHTTP}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return credential{}, upstreamStatusError("Kiro token refresh failed", resp.StatusCode, resp.Body)
	}
	var result map[string]any
	if errJSON := json.Unmarshal(resp.Body, &result); errJSON != nil {
		return credential{}, statusError{Code: "invalid_refresh_response", Message: "Kiro token refresh returned invalid JSON", Retryable: true, HTTPStatus: http.StatusBadGateway, Cause: errJSON}
	}
	accessToken := jsonx.String(result, "accessToken", "access_token")
	if accessToken == "" {
		return credential{}, statusError{Code: "missing_access_token", Message: "Kiro token refresh response did not contain an access token", Retryable: true, HTTPStatus: http.StatusBadGateway}
	}
	cred.AccessToken = accessToken
	if refreshToken := jsonx.String(result, "refreshToken", "refresh_token"); refreshToken != "" {
		cred.RefreshToken = refreshToken
	}
	if profileARN := jsonx.String(result, "profileArn", "profile_arn"); profileARN != "" {
		cred.ProfileARN = profileARN
	}
	expiresIn := int64(3600)
	if number, okNumber := jsonx.Number(result["expiresIn"]); okNumber && number > 0 {
		expiresIn = int64(number)
	}
	cred.ExpiresAt = time.Now().UTC().Add(time.Duration(expiresIn-60) * time.Second).Format(time.RFC3339)
	finalizeCredential(&cred)
	return cred, nil
}

func configuredRegionURL(configured, fallback, region string) string {
	configured = strings.TrimSpace(configured)
	if configured == "" {
		return fallback
	}
	return strings.ReplaceAll(configured, "{region}", jsonx.NonEmpty(region, defaultRegion))
}

func decodeCredential(raw []byte) (credential, error) {
	var cred credential
	if len(raw) == 0 {
		return cred, fmt.Errorf("Kiro auth storage is empty")
	}
	if errUnmarshal := json.Unmarshal(raw, &cred); errUnmarshal != nil {
		return cred, fmt.Errorf("decode Kiro auth storage: %w", errUnmarshal)
	}
	finalizeCredential(&cred)
	return cred, nil
}

func credentialNeedsRefresh(cred credential) bool {
	if strings.TrimSpace(cred.AccessToken) == "" {
		return true
	}
	expires, errParse := parseExpiry(cred.ExpiresAt)
	return errParse != nil || expires.Before(time.Now().UTC().Add(10*time.Minute))
}

type statusError struct {
	Code       string
	Message    string
	Retryable  bool
	HTTPStatus int
	Cause      error
}

func (e statusError) Error() string {
	if e.Cause != nil {
		return e.Message + ": " + e.Cause.Error()
	}
	return e.Message
}

func upstreamStatusError(prefix string, status int, body []byte) statusError {
	message := prefix
	var object map[string]any
	if json.Unmarshal(body, &object) == nil {
		if detail := jsonx.String(object, "message", "error_description", "error"); detail != "" {
			message += ": " + detail
		}
	}
	retryable := status == http.StatusTooManyRequests || status >= 500
	code := "upstream_error"
	if status == http.StatusUnauthorized || status == http.StatusForbidden {
		code = "unauthorized"
	} else if status == http.StatusPaymentRequired {
		code = "quota_exhausted"
	} else if status == http.StatusTooManyRequests {
		code = "rate_limited"
	} else if status >= 400 && status < 500 {
		code = "invalid_request"
	}
	return statusError{Code: code, Message: message, Retryable: retryable, HTTPStatus: status}
}

func pluginErrorEnvelope(err error) []byte {
	if typed, ok := err.(statusError); ok {
		return errorEnvelope(typed.Code, typed.Error(), typed.Retryable, typed.HTTPStatus)
	}
	return errorEnvelope("plugin_error", err.Error(), false, http.StatusInternalServerError)
}
