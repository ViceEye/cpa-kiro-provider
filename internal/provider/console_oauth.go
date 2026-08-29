package provider

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync"
)

var consoleOAuthSessions = struct {
	sync.Mutex
	metadata map[string]map[string]any
}{metadata: make(map[string]map[string]any)}

// CPA can issue overlapping status polls while the device/token exchange is
// in flight. Serialize them so one OAuth state cannot be persisted twice.
var consoleOAuthStatusMu sync.Mutex

func handleConsoleOAuthStart(req managementRequest) ([]byte, error) {
	if req.Method != http.MethodPost {
		return managementJSON(http.StatusMethodNotAllowed, map[string]any{"error": "method_not_allowed"}), nil
	}
	var input map[string]any
	if len(req.Body) > 0 && json.Unmarshal(req.Body, &input) != nil {
		return managementJSON(http.StatusBadRequest, map[string]any{"error": "invalid_request"}), nil
	}
	raw, err := startLogin(mustJSON(authLoginStartRequest{Provider: providerID, Metadata: input}))
	if err != nil {
		return nil, err
	}
	var env envelope
	if json.Unmarshal(raw, &env) != nil || !env.OK {
		return managementJSON(http.StatusBadGateway, map[string]any{"error": "oauth_start_failed"}), nil
	}
	var started authLoginStartResponse
	if json.Unmarshal(env.Result, &started) != nil || strings.TrimSpace(started.State) == "" {
		return managementJSON(http.StatusBadGateway, map[string]any{"error": "oauth_start_failed"}), nil
	}
	consoleOAuthSessions.Lock()
	consoleOAuthSessions.metadata[started.State] = started.Metadata
	consoleOAuthSessions.Unlock()
	return managementJSON(http.StatusOK, map[string]any{"status": "ok", "url": started.URL, "state": started.State}), nil
}

func handleConsoleOAuthStatus(req managementRequest) ([]byte, error) {
	if req.Method != http.MethodGet {
		return managementJSON(http.StatusMethodNotAllowed, map[string]any{"error": "method_not_allowed"}), nil
	}
	consoleOAuthStatusMu.Lock()
	defer consoleOAuthStatusMu.Unlock()
	state := strings.TrimSpace(req.QueryValue("state"))
	consoleOAuthSessions.Lock()
	metadata, ok := consoleOAuthSessions.metadata[state]
	consoleOAuthSessions.Unlock()
	if !ok || state == "" {
		return managementJSON(http.StatusNotFound, map[string]any{"error": "unknown_state"}), nil
	}
	raw, err := pollLogin(mustJSON(authLoginPollRequest{Provider: providerID, State: state, Metadata: metadata}))
	if err != nil {
		return managementJSON(http.StatusBadGateway, map[string]any{"error": "oauth_poll_failed", "message": err.Error()}), nil
	}
	var env envelope
	if json.Unmarshal(raw, &env) != nil || !env.OK {
		return managementJSON(http.StatusBadGateway, map[string]any{"error": "oauth_poll_failed"}), nil
	}
	var result authLoginPollResponse
	if json.Unmarshal(env.Result, &result) != nil {
		return managementJSON(http.StatusBadGateway, map[string]any{"error": "oauth_poll_failed"}), nil
	}
	if result.Status == "success" {
		if strings.TrimSpace(result.Auth.FileName) == "" || len(result.Auth.StorageJSON) == 0 {
			return managementJSON(http.StatusBadGateway, map[string]any{"error": "oauth_save_failed", "message": "OAuth returned no credential data"}), nil
		}
		if _, errSave := callHostCall("host.auth.save", map[string]any{
			"name": result.Auth.FileName,
			"json": json.RawMessage(result.Auth.StorageJSON),
		}); errSave != nil {
			return managementJSON(http.StatusBadGateway, map[string]any{"error": "oauth_save_failed", "message": errSave.Error()}), nil
		}
	}
	if result.Status == "success" || result.Status == "error" {
		consoleOAuthSessions.Lock()
		delete(consoleOAuthSessions.metadata, state)
		consoleOAuthSessions.Unlock()
	}
	return managementJSON(http.StatusOK, map[string]any{"status": result.Status, "message": result.Message}), nil
}
