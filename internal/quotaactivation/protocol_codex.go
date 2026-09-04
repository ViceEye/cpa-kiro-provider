package quotaactivation

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

const CodexActivationURL = "https://chatgpt.com/backend-api/codex/responses"

type codexActivationBody struct {
	Model        string             `json:"model"`
	Instructions string             `json:"instructions"`
	Input        []codexInputMessage `json:"input"`
	Store        bool               `json:"store"`
	Stream       bool               `json:"stream"`
}

type codexInputMessage struct {
	Type    string           `json:"type"`
	Role    string           `json:"role"`
	Content []codexInputPart `json:"content"`
}

type codexInputPart struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// BuildCodexProtocol builds the native Codex activation request. The shape is
// intentionally the same as quota-activation: input message list, stream=true,
// and store=false.
func BuildCodexProtocol(material AuthMaterial, model, prompt string) (ProtocolRequest, error) {
	if strings.TrimSpace(material.AccessToken) == "" {
		return ProtocolRequest{}, fmt.Errorf("Codex唤醒失败：缺少访问令牌")
	}
	model = strings.TrimSpace(strings.TrimPrefix(model, "nexus/"))
	if model == "" {
		return ProtocolRequest{}, fmt.Errorf("Codex唤醒失败：缺少模型名称")
	}
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		prompt = "quota activation ping"
	}
	body, err := json.Marshal(codexActivationBody{
		Model:        model,
		Instructions: "You are a helpful assistant.",
		Input: []codexInputMessage{{
			Type: "message",
			Role: "user",
			Content: []codexInputPart{{Type: "input_text", Text: prompt}},
		}},
		Store:  false,
		Stream: true,
	})
	if err != nil {
		return ProtocolRequest{}, fmt.Errorf("Codex唤醒失败：编码请求失败")
	}
	headers := http.Header{
		"Accept":        []string{"text/event-stream"},
		"Authorization": []string{"Bearer " + strings.TrimSpace(material.AccessToken)},
		"Content-Type":  []string{"application/json"},
		"OpenAI-Beta":   []string{"responses=v1"},
		"Originator":    []string{"codex_cli_rs"},
		"User-Agent":    []string{"codex_cli_rs/0.76.0 (Debian 13.0.0; x86_64) WindowsTerminal"},
	}
	if accountID := strings.TrimSpace(material.AccountID); accountID != "" {
		headers.Set("Chatgpt-Account-Id", accountID)
	}
	return ProtocolRequest{Method: http.MethodPost, URL: CodexActivationURL, Headers: headers, Body: body}, nil
}

// EvaluateCodexActivationSuccess requires a structurally valid response instead
// of treating every HTTP 2xx response as a successful quota activation.
func EvaluateCodexActivationSuccess(statusCode int, body []byte) (bool, string) {
	if statusCode < http.StatusOK || statusCode >= http.StatusMultipleChoices {
		return false, fmt.Sprintf("Codex唤醒失败：上游返回非成功状态（HTTP %d）", statusCode)
	}
	trimmed := strings.TrimSpace(string(body))
	if trimmed == "" {
		return false, "Codex唤醒失败：响应体为空"
	}
	if strings.Contains(trimmed, "data:") {
		return evaluateCodexSSE(trimmed)
	}
	var root map[string]json.RawMessage
	if json.Unmarshal(body, &root) != nil {
		return false, "Codex唤醒失败：响应不是合法数据"
	}
	if codexError(root) {
		return false, "Codex唤醒失败：上游返回业务错误"
	}
	if codexSuccess(root) {
		return true, ""
	}
	if nested, ok := root["response"]; ok {
		var response map[string]json.RawMessage
		if json.Unmarshal(nested, &response) == nil && !codexError(response) && codexSuccess(response) {
			return true, ""
		}
	}
	return false, "Codex唤醒失败：响应缺少有效输出结构"
}

func evaluateCodexSSE(value string) (bool, string) {
	sawCompleted := false
	sawCreatedWithID := false
	for _, line := range strings.Split(value, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "" || payload == "[DONE]" {
			continue
		}
		var event map[string]json.RawMessage
		if json.Unmarshal([]byte(payload), &event) != nil {
			continue
		}
		var eventType string
		_ = json.Unmarshal(event["type"], &eventType)
		if eventType == "response.failed" || eventType == "error" || codexError(event) {
			return false, "Codex唤醒失败：上游返回业务错误"
		}
		if eventType == "response.completed" {
			sawCompleted = true
		}
		if nested, ok := event["response"]; ok {
			var response map[string]json.RawMessage
			if json.Unmarshal(nested, &response) == nil {
				if codexError(response) {
					return false, "Codex唤醒失败：上游返回业务错误"
				}
				if codexSuccess(response) {
					return true, ""
				}
				if eventType == "response.created" {
					var id string
					if json.Unmarshal(response["id"], &id) == nil && strings.TrimSpace(id) != "" {
						sawCreatedWithID = true
					}
				}
			}
		}
	}
	if sawCompleted || sawCreatedWithID {
		return true, ""
	}
	return false, "Codex唤醒失败：响应缺少有效输出结构"
}

func codexSuccess(root map[string]json.RawMessage) bool {
	var id string
	if json.Unmarshal(root["id"], &id) == nil && strings.TrimSpace(id) != "" {
		return true
	}
	var output []json.RawMessage
	if json.Unmarshal(root["output"], &output) == nil && len(output) > 0 {
		return true
	}
	return false
}

func codexError(root map[string]json.RawMessage) bool {
	raw, ok := root["error"]
	if !ok || string(raw) == "null" {
		return false
	}
	return len(strings.TrimSpace(string(raw))) > 0
}
