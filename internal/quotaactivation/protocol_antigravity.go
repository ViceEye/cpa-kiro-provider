package quotaactivation

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

const AntigravityActivationURL = "https://daily-cloudcode-pa.googleapis.com/v1internal:generateContent"

type antigravityActivationBody struct {
	Project     string                    `json:"project"`
	Model       string                    `json:"model"`
	UserAgent   string                    `json:"userAgent"`
	RequestType string                    `json:"requestType"`
	RequestID   string                    `json:"requestId"`
	Request     antigravityInnerRequest   `json:"request"`
}

type antigravityInnerRequest struct {
	SessionID string                 `json:"sessionId"`
	Contents  []antigravityContent   `json:"contents"`
}

type antigravityContent struct {
	Role  string             `json:"role"`
	Parts []antigravityPart  `json:"parts"`
}

type antigravityPart struct {
	Text string `json:"text"`
}

// BuildAntigravityProtocol builds the native generateContent activation request.
func BuildAntigravityProtocol(material AuthMaterial, model, prompt string) (ProtocolRequest, error) {
	if strings.TrimSpace(material.AccessToken) == "" {
		return ProtocolRequest{}, fmt.Errorf("Antigravity唤醒失败：缺少访问令牌")
	}
	model = strings.TrimSpace(strings.TrimPrefix(model, "nexus/"))
	if model == "" {
		return ProtocolRequest{}, fmt.Errorf("Antigravity唤醒失败：缺少模型名称")
	}
	project := strings.TrimSpace(material.ProjectID)
	if project == "" {
		return ProtocolRequest{}, fmt.Errorf("Antigravity唤醒失败：缺少项目")
	}
	if !strings.HasPrefix(project, "projects/") {
		project = "projects/" + project
	}
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		prompt = "quota activation ping"
	}
	requestID, err := randomID()
	if err != nil {
		return ProtocolRequest{}, fmt.Errorf("Antigravity唤醒失败：生成请求标识失败")
	}
	sessionID, err := randomID()
	if err != nil {
		return ProtocolRequest{}, fmt.Errorf("Antigravity唤醒失败：生成会话标识失败")
	}
	body, err := json.Marshal(antigravityActivationBody{
		Project: project, Model: model, UserAgent: "antigravity", RequestType: "agent", RequestID: requestID,
		Request: antigravityInnerRequest{
			SessionID: sessionID,
			Contents: []antigravityContent{{Role: "user", Parts: []antigravityPart{{Text: prompt}}}},
		},
	})
	if err != nil {
		return ProtocolRequest{}, fmt.Errorf("Antigravity唤醒失败：编码请求失败")
	}
	return ProtocolRequest{
		Method: http.MethodPost, URL: AntigravityActivationURL,
		Headers: http.Header{
			"Accept":        []string{"application/json"},
			"Authorization": []string{"Bearer " + strings.TrimSpace(material.AccessToken)},
			"Content-Type":  []string{"application/json"},
			"User-Agent":    []string{"antigravity/cli/1.0.8 darwin/arm64"},
		},
		Body: body,
	}, nil
}

// EvaluateAntigravityActivationSuccess requires a candidate in the response.
func EvaluateAntigravityActivationSuccess(statusCode int, body []byte) (bool, string) {
	if statusCode < http.StatusOK || statusCode >= http.StatusMultipleChoices {
		return false, fmt.Sprintf("Antigravity唤醒失败：上游返回非成功状态（HTTP %d）", statusCode)
	}
	if len(strings.TrimSpace(string(body))) == 0 {
		return false, "Antigravity唤醒失败：响应体为空"
	}
	var payload struct {
		Candidates []json.RawMessage `json:"candidates"`
		Response   struct {
			Candidates []json.RawMessage `json:"candidates"`
		} `json:"response"`
		Error json.RawMessage `json:"error"`
	}
	if json.Unmarshal(body, &payload) != nil {
		return false, "Antigravity唤醒失败：响应不是合法数据"
	}
	if len(payload.Error) > 0 && string(payload.Error) != "null" {
		return false, "Antigravity唤醒失败：上游返回业务错误"
	}
	if len(payload.Candidates) == 0 && len(payload.Response.Candidates) == 0 {
		return false, "Antigravity唤醒失败：响应缺少候选项"
	}
	return true, ""
}

func randomID() (string, error) {
	var data [16]byte
	if _, err := rand.Read(data[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(data[:]), nil
}
