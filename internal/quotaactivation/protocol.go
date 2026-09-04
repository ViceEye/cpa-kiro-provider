// Package quotaactivation contains the small, provider-specific model request
// builders and response checks adapted from quota-activation.
package quotaactivation

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// ProtocolRequest is the minimal request shape used by the CPA host HTTP bridge.
type ProtocolRequest struct {
	Method  string
	URL     string
	Headers http.Header
	Body    []byte
}

// AuthMaterial contains only the upstream fields required to build a request.
// It must never be logged with token values.
type AuthMaterial struct {
	AccessToken string
	AccountID   string
	ProjectID   string
}

// ParseAuthMaterial accepts the common CPA auth-file shapes used by Codex and
// Antigravity, including nested token documents.
func ParseAuthMaterial(raw []byte) (AuthMaterial, error) {
	if len(strings.TrimSpace(string(raw))) == 0 {
		return AuthMaterial{}, fmt.Errorf("凭证内容为空")
	}
	var root map[string]json.RawMessage
	if err := json.Unmarshal(raw, &root); err != nil {
		return AuthMaterial{}, fmt.Errorf("凭证不是合法数据")
	}

	material := AuthMaterial{
		AccessToken: firstStringField(root,
			"access_token", "accessToken", "oauth_access_token", "oauthAccessToken",
			"api_key", "apiKey", "session_token", "sessionToken", "token", "id_token", "idToken",
		),
		AccountID: firstStringField(root, "account_id", "chatgpt_account_id", "accountId", "chatgptAccountId"),
		ProjectID: firstStringField(root, "project_id", "projectId", "project", "quota_project_id", "quotaProjectID"),
	}

	for _, key := range []string{"tokens", "token_data", "tokenData", "credentials", "auth", "oauth", "session"} {
		nestedRaw, ok := root[key]
		if !ok {
			continue
		}
		var nested map[string]json.RawMessage
		if json.Unmarshal(nestedRaw, &nested) != nil {
			continue
		}
		if material.AccessToken == "" {
			material.AccessToken = firstStringField(nested,
				"access_token", "accessToken", "oauth_access_token", "oauthAccessToken",
				"api_key", "apiKey", "session_token", "sessionToken", "token", "id_token", "idToken",
			)
		}
		if material.AccountID == "" {
			material.AccountID = firstStringField(nested, "account_id", "chatgpt_account_id", "accountId", "chatgptAccountId")
		}
		if material.ProjectID == "" {
			material.ProjectID = firstStringField(nested, "project_id", "projectId", "project", "quota_project_id", "quotaProjectID")
		}
	}

	if material.AccountID == "" {
		material.AccountID = accountIDFromJWT(firstStringField(root, "id_token", "idToken"))
	}
	if material.AccessToken == "" {
		return AuthMaterial{}, fmt.Errorf("缺少访问令牌")
	}
	return material, nil
}

func firstStringField(object map[string]json.RawMessage, keys ...string) string {
	for _, key := range keys {
		raw, ok := object[key]
		if !ok {
			continue
		}
		var value string
		if json.Unmarshal(raw, &value) == nil && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
		var objectValue struct {
			ID string `json:"id"`
		}
		if json.Unmarshal(raw, &objectValue) == nil && strings.TrimSpace(objectValue.ID) != "" {
			return strings.TrimSpace(objectValue.ID)
		}
	}
	return ""
}

func accountIDFromJWT(token string) string {
	parts := strings.Split(strings.TrimSpace(token), ".")
	if len(parts) != 3 {
		return ""
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return ""
	}
	var claims map[string]json.RawMessage
	if json.Unmarshal(payload, &claims) != nil {
		return ""
	}
	if value := firstStringField(claims, "chatgpt_account_id", "account_id"); value != "" {
		return value
	}
	var authClaims map[string]json.RawMessage
	if json.Unmarshal(claims["https://api.openai.com/auth"], &authClaims) == nil {
		return firstStringField(authClaims, "chatgpt_account_id", "account_id")
	}
	return ""
}
