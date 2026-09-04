package provider

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
)

type reloginSession struct {
	Metadata map[string]any
	FileName string
	Existing map[string]any
	AuthDir  string
}

var reloginSessions = struct {
	sync.Mutex
	items map[string]reloginSession
}{items: make(map[string]reloginSession)}

func handleReloginStart(req managementRequest) ([]byte, error) {
	if req.Method != http.MethodPost {
		return managementJSON(http.StatusMethodNotAllowed, map[string]any{"error": "method_not_allowed"}), nil
	}
	var body reloginRequest
	if err := json.Unmarshal(req.Body, &body); err != nil || strings.TrimSpace(body.AuthIndex) == "" {
		return managementJSON(http.StatusBadRequest, map[string]any{"error": "auth_index_required"}), nil
	}
	existingRaw, err := callHostCall("host.auth.get", map[string]any{"auth_index": strings.TrimSpace(body.AuthIndex)})
	if err != nil {
		return managementJSON(http.StatusBadRequest, map[string]any{"error": "auth_not_found", "message": err.Error()}), nil
	}
	var existing struct {
		Name string          `json:"name"`
		Path string          `json:"path"`
		JSON json.RawMessage `json:"json"`
	}
	if err := json.Unmarshal(existingRaw, &existing); err != nil {
		return managementJSON(http.StatusBadRequest, map[string]any{"error": "invalid_kiro_auth"}), nil
	}
	var old map[string]any
	if err := json.Unmarshal(existing.JSON, &old); err != nil {
		return managementJSON(http.StatusBadRequest, map[string]any{"error": "invalid_kiro_auth"}), nil
	}
	provider, _ := old["type"].(string)
	if !strings.EqualFold(strings.TrimSpace(provider), providerID) {
		return managementJSON(http.StatusBadRequest, map[string]any{"error": "not_kiro_auth"}), nil
	}
	fileName := filepath.Base(strings.TrimSpace(existing.Name))
	if fileName == "." || fileName == "" || !strings.HasSuffix(strings.ToLower(fileName), ".json") {
		return managementJSON(http.StatusBadRequest, map[string]any{"error": "invalid_auth_filename"}), nil
	}

	raw, err := startLogin(mustJSON(authLoginStartRequest{Provider: providerID, Metadata: map[string]any{"relogin": true}}))
	if err != nil {
		return nil, err
	}
	var env envelope
	if err := json.Unmarshal(raw, &env); err != nil || !env.OK {
		return managementJSON(http.StatusBadGateway, map[string]any{"error": "oauth_start_failed"}), nil
	}
	var started authLoginStartResponse
	if err := json.Unmarshal(env.Result, &started); err != nil || strings.TrimSpace(started.State) == "" {
		return managementJSON(http.StatusBadGateway, map[string]any{"error": "oauth_start_failed"}), nil
	}
	reloginSessions.Lock()
	reloginSessions.items[started.State] = reloginSession{Metadata: started.Metadata, FileName: fileName, Existing: old, AuthDir: filepath.Dir(existing.Path)}
	reloginSessions.Unlock()
	return managementJSON(http.StatusOK, map[string]any{"status": "ok", "url": started.URL, "state": started.State}), nil
}

func handleReloginStatus(req managementRequest) ([]byte, error) {
	if req.Method != http.MethodGet && req.Method != http.MethodPost {
		return managementJSON(http.StatusMethodNotAllowed, map[string]any{"error": "method_not_allowed"}), nil
	}
	state := strings.TrimSpace(req.QueryValue("state"))
	if state == "" {
		var body reloginStatusRequest
		_ = json.Unmarshal(req.Body, &body)
		state = strings.TrimSpace(body.State)
	}
	reloginSessions.Lock()
	session, ok := reloginSessions.items[state]
	reloginSessions.Unlock()
	if !ok {
		return managementJSON(http.StatusNotFound, map[string]any{"error": "unknown_state"}), nil
	}
	pollRaw, err := pollLogin(mustJSON(authLoginPollRequest{Provider: providerID, State: state, Metadata: session.Metadata, Host: hostConfigSummary{AuthDir: session.AuthDir}}))
	if err != nil {
		return managementJSON(http.StatusBadGateway, map[string]any{"error": "oauth_poll_failed", "message": err.Error()}), nil
	}
	var env envelope
	if err := json.Unmarshal(pollRaw, &env); err != nil || !env.OK {
		return managementJSON(http.StatusBadGateway, map[string]any{"error": "oauth_poll_failed"}), nil
	}
	var result authLoginPollResponse
	if err := json.Unmarshal(env.Result, &result); err != nil {
		return managementJSON(http.StatusBadGateway, map[string]any{"error": "oauth_poll_failed"}), nil
	}
	if result.Status != "success" {
		if result.Status == "error" {
			reloginSessions.Lock()
			delete(reloginSessions.items, state)
			reloginSessions.Unlock()
		}
		return managementJSON(http.StatusOK, map[string]any{"status": result.Status, "message": result.Message}), nil
	}
	var fresh map[string]any
	if err := json.Unmarshal(result.Auth.StorageJSON, &fresh); err != nil {
		return managementJSON(http.StatusBadGateway, map[string]any{"error": "invalid_oauth_result"}), nil
	}
	for key, value := range session.Existing {
		if _, credentialField := fresh[key]; !credentialField {
			fresh[key] = value
		}
	}
	rawFresh, _ := json.Marshal(fresh)
	if _, err := callHostCall("host.auth.save", map[string]any{"name": session.FileName, "json": json.RawMessage(rawFresh)}); err != nil {
		return managementJSON(http.StatusBadGateway, map[string]any{"error": "auth_save_failed", "message": err.Error()}), nil
	}
	reloginSessions.Lock()
	delete(reloginSessions.items, state)
	reloginSessions.Unlock()
	return managementJSON(http.StatusOK, map[string]any{"status": "success", "name": session.FileName}), nil
}

func (r managementRequest) QueryValue(key string) string {
	if values := r.Query[key]; len(values) > 0 {
		return values[0]
	}
	return ""
}
