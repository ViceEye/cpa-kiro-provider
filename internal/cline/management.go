package cline

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// modelsForAuth registers the free-tier catalog under the cline/ prefix.
func modelsForAuth(raw []byte) ([]byte, error) {
	models := make([]modelInfo, 0, len(freeModels))
	for _, model := range freeModels {
		entry := model
		entry.ID = modelPrefix + model.ID
		models = append(models, entry)
	}
	return okEnvelope(modelResponse{Provider: providerID, Models: models})
}

// usageForConnection returns the remaining free-credit balance. The quota
// shape mirrors the kiro plugin's quotaAccount output.
func Usage(raw []byte) ([]byte, error) {
	var req managementRequest
	_ = json.Unmarshal(raw, &req)
	callbackID := req.HostCallbackID
	conn := struct {
		StorageJSON    []byte `json:"StorageJSON"`
		AuthIndex      string `json:"auth_index"`
		HostCallbackID string `json:"host_callback_id"`
	}{}
	_ = json.Unmarshal(req.Body, &conn)
	if callbackID == "" {
		callbackID = conn.HostCallbackID
	}
	cred, err := decodeCredential(conn.StorageJSON)
	if err != nil {
		return managementJSON(http.StatusOK, map[string]any{
			"accounts": []map[string]any{{"status": "error", "error": err.Error()}},
		}), nil
	}
	if credentialNeedsRefresh(cred) {
		refreshed, err := refreshCredential(cred, callbackID)
		if err != nil {
			return managementJSON(http.StatusOK, map[string]any{
				"accounts": []map[string]any{{"status": "error", "error": err.Error()}},
			}), nil
		}
		cred = refreshed
		persistCredentialBestEffort(cred)
	}

	headers := buildAPIHeaders(cred.AccessToken, nil)
	meRes, err := hostHTTP(hostHTTPRequest{
		HostCallbackID: callbackID, Method: http.MethodGet, URL: apiBase + mePath,
		Headers: map[string][]string{"Authorization": {headers["Authorization"]}, "Accept": {"application/json"}},
	})
	if err != nil || meRes.StatusCode != http.StatusOK {
		return managementJSON(http.StatusOK, map[string]any{
			"accounts": []map[string]any{{"status": "error", "error": fmt.Sprintf("Cline user API failed (%d)", meRes.StatusCode)}},
		}), nil
	}
	var user clineUser
	if err := json.Unmarshal(meRes.Body, &user); err != nil || user.Data.ID == "" {
		return managementJSON(http.StatusOK, map[string]any{
			"accounts": []map[string]any{{"status": "error", "error": "Cline user API returned no id"}},
		}), nil
	}
	cred.Email = strings.TrimSpace(user.Data.Email)
	balRes, err := hostHTTP(hostHTTPRequest{
		HostCallbackID: callbackID, Method: http.MethodGet,
		URL:     apiBase + fmt.Sprintf(balanceFmt, user.Data.ID),
		Headers: map[string][]string{"Authorization": {headers["Authorization"]}, "Accept": {"application/json"}},
	})
	if err != nil || balRes.StatusCode != http.StatusOK {
		return managementJSON(http.StatusOK, map[string]any{
			"accounts": []map[string]any{{"status": "error", "error": fmt.Sprintf("Cline balance API failed (%d)", balRes.StatusCode)}},
		}), nil
	}
	var balance clineBalance
	_ = json.Unmarshal(balRes.Body, &balance)

	persistCredentialBestEffort(cred)
	return managementJSON(http.StatusOK, map[string]any{
		"accounts": []map[string]any{{
			"status":     "ok",
			"name":       user.Data.Email,
			"plan":       "Cline Free",
			"balance":    balance.Data.Balance,
			"unit":       "credit",
			"quotas":     []map[string]any{{"name": "Credits", "remaining": balance.Data.Balance, "unlimited": false, "unit": "credit"}},
			"email":      user.Data.Email,
			"auth_index": conn.AuthIndex,
		}},
	}), nil
}

// persistCredentialBestEffort writes the refreshed credential back to the
// host auth store under the credential's own file name.
func persistCredentialBestEffort(cred credential) {
	auth, err := authDataFromCredential(cred)
	if err != nil {
		return
	}
	_, _ = callHost("host.auth.save", map[string]any{"name": auth.FileName, "json": json.RawMessage(auth.StorageJSON)})
}
