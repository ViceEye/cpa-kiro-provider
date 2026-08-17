package main

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type browserCallbackManagementRequest struct {
	RedirectURL string `json:"redirect_url"`
}

func handleBrowserCallbackManagement(req managementRequest) ([]byte, error) {
	if !strings.EqualFold(req.Method, http.MethodPost) {
		return okEnvelope(managementResponse{
			StatusCode: http.StatusMethodNotAllowed,
			Headers:    jsonHeaders(),
			Body:       mustJSON(map[string]any{"error": "method_not_allowed"}),
		})
	}
	var body browserCallbackManagementRequest
	if errDecode := json.Unmarshal(req.Body, &body); errDecode != nil {
		return browserCallbackError(http.StatusBadRequest, "invalid_callback", "Kiro callback request must contain JSON")
	}
	callbackURL, errParse := url.Parse(strings.TrimSpace(body.RedirectURL))
	if errParse != nil || callbackURL.Scheme == "" || callbackURL.Host == "" {
		return browserCallbackError(http.StatusBadRequest, "invalid_callback", "Kiro callback URL is invalid")
	}
	query := callbackURL.Query()
	state := strings.TrimSpace(query.Get("state"))
	if state == "" {
		return browserCallbackError(http.StatusBadRequest, "missing_state", "Kiro callback URL has no state")
	}
	session, exists := browserLoginSessionForState(state)
	if !exists || session.LoginState.State != state {
		return browserCallbackError(http.StatusBadRequest, "unknown_state", "Kiro login state is unknown or expired; start login again")
	}
	expiresAt, errExpiry := time.Parse(time.RFC3339, session.LoginState.ExpiresAt)
	if errExpiry != nil || !time.Now().UTC().Before(expiresAt) {
		clearBrowserLoginSession(state)
		return browserCallbackError(http.StatusBadRequest, "expired_state", "Kiro login state has expired; start login again")
	}
	if !callbackMatchesRedirect(callbackURL, session.LoginState.RedirectURI) {
		return browserCallbackError(http.StatusBadRequest, "invalid_callback", "Kiro callback URL does not match the login redirect URI")
	}
	callback := oauthCallbackPayload{
		Code: strings.TrimSpace(query.Get("code")), State: state, Error: strings.TrimSpace(query.Get("error")),
	}
	if callback.Code != "" || callback.Error != "" {
		if !storeBrowserCallback(state, callback) {
			return browserCallbackError(http.StatusBadRequest, "unknown_state", "Kiro login state is unknown or expired; start login again")
		}
		return okEnvelope(managementResponse{
			StatusCode: http.StatusOK,
			Headers:    jsonHeaders(),
			Body: mustJSON(map[string]any{
				"status": "accepted", "state": state,
			}),
		})
	}

	loginOption := normalizeKiroLoginOption(query.Get("login_option"))
	if loginOption == "" {
		loginOption = normalizeKiroLoginOption(query.Get("loginOption"))
	}
	if loginOption == "" {
		return browserCallbackError(http.StatusBadRequest, "missing_login_option", "Kiro callback has neither an OAuth code nor a supported login option")
	}
	if session.Device != nil {
		return browserCallbackError(http.StatusConflict, "login_already_continued", "Kiro organization login has already continued; open the current verification link")
	}
	startURL := strings.TrimSpace(query.Get("issuer_url"))
	if startURL == "" {
		startURL = strings.TrimSpace(query.Get("issuerUrl"))
	}
	if startURL == "" && loginOption == "builderid" {
		startURL = defaultSSOStartURL
	}
	region := strings.TrimSpace(query.Get("idc_region"))
	if region == "" {
		region = strings.TrimSpace(query.Get("idcRegion"))
	}
	if region == "" {
		region = strings.TrimSpace(loadedConfig().SSORegion)
	}
	if region == "" {
		region = defaultRegion
	}
	apiRegion := nonEmpty(strings.TrimSpace(session.LoginState.APIRegion), configuredAPIRegion(loadedConfig()))
	deviceState, verificationURL, expiresAt, errDevice := beginDeviceAuthorization(req.HostCallbackID, state, startURL, region, apiRegion)
	if errDevice != nil {
		status := pluginHTTPStatus(errDevice)
		code := "organization_login_failed"
		message := errDevice.Error()
		if typed, ok := errDevice.(statusError); ok {
			code = typed.Code
		}
		return browserCallbackError(status, code, message)
	}
	if !storeBrowserDeviceContinuation(state, deviceState) {
		clearDeviceLoginPoll(state)
		return browserCallbackError(http.StatusBadRequest, "unknown_state", "Kiro login state is unknown or expired; start login again")
	}
	return okEnvelope(managementResponse{
		StatusCode: http.StatusOK,
		Headers:    jsonHeaders(),
		Body: mustJSON(map[string]any{
			"status":       "continue",
			"state":        state,
			"url":          verificationURL,
			"user_code":    deviceState.UserCode,
			"expires_at":   expiresAt.Format(time.RFC3339),
			"login_option": loginOption,
		}),
	})
}

func callbackMatchesRedirect(callbackURL *url.URL, redirectURI string) bool {
	expected, errParse := url.Parse(strings.TrimSpace(redirectURI))
	if errParse != nil || expected.Scheme == "" || expected.Host == "" {
		return false
	}
	return strings.EqualFold(callbackURL.Scheme, expected.Scheme) && strings.EqualFold(callbackURL.Host, expected.Host)
}

func normalizeKiroLoginOption(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "awsidc", "aws-idc", "idc", "sso", "organization", "organisation", "enterprise", "iam", "iam-identity-center":
		return "awsidc"
	case "builderid", "builder-id", "builder_id", "internal":
		return "builderid"
	default:
		return ""
	}
}

func browserCallbackError(status int, code, message string) ([]byte, error) {
	return okEnvelope(managementResponse{
		StatusCode: status,
		Headers:    jsonHeaders(),
		Body:       mustJSON(map[string]any{"error": code, "message": message}),
	})
}
