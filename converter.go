package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

const maxKiroPayloadBytes = 600000

type chatRequest struct {
	Model    string        `json:"model"`
	Messages []chatMessage `json:"messages"`
	Tools    []chatTool    `json:"tools"`
	Stream   bool          `json:"stream"`
	User     string        `json:"user,omitempty"`
}

type chatMessage struct {
	Role       string          `json:"role"`
	Content    json.RawMessage `json:"content"`
	Name       string          `json:"name,omitempty"`
	ToolCallID string          `json:"tool_call_id,omitempty"`
	ToolCalls  []chatToolCall  `json:"tool_calls,omitempty"`
}

type chatToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function chatFunction `json:"function"`
}

type chatFunction struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

type chatTool struct {
	Type     string             `json:"type"`
	Function chatToolDefinition `json:"function"`
}

type chatToolDefinition struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

type normalizedMessage struct {
	Role        string
	Text        string
	Images      []map[string]any
	ToolUses    []map[string]any
	ToolResults []map[string]any
}

func buildKiroPayload(raw []byte, requestedModel string, cred credential) ([]byte, string, error) {
	var request chatRequest
	if errUnmarshal := json.Unmarshal(raw, &request); errUnmarshal != nil {
		return nil, "", fmt.Errorf("decode chat-completions request: %w", errUnmarshal)
	}
	model := normalizeModelName(request.Model)
	if requestedModel != "" {
		model = normalizeModelName(requestedModel)
	}
	if model == "" {
		model = "auto"
	}

	var systemParts []string
	var messages []normalizedMessage
	for _, message := range request.Messages {
		role := strings.ToLower(strings.TrimSpace(message.Role))
		text, images, toolResults := extractMessageContent(message.Content)
		if role == "system" || role == "developer" {
			if text != "" {
				systemParts = append(systemParts, text)
			}
			continue
		}
		normalized := normalizedMessage{Role: role, Text: text, Images: images, ToolResults: toolResults}
		switch role {
		case "assistant":
			normalized.ToolUses = convertAssistantToolCalls(message.ToolCalls)
		case "tool":
			normalized.Role = "user"
			if normalized.Text == "" {
				normalized.Text = "(empty result)"
			}
			normalized.ToolResults = append(normalized.ToolResults, map[string]any{
				"content":   []any{map[string]any{"text": normalized.Text}},
				"status":    "success",
				"toolUseId": message.ToolCallID,
			})
		default:
			normalized.Role = "user"
		}
		messages = append(messages, normalized)
	}
	if len(messages) == 0 {
		return nil, "", fmt.Errorf("chat-completions request contains no conversational messages")
	}
	messages = normalizeConversation(messages)
	if len(messages) == 0 {
		return nil, "", fmt.Errorf("chat-completions request contains no usable messages")
	}
	systemPrompt := strings.Join(systemParts, "\n\n")
	if systemPrompt != "" {
		messages[0].Text = systemPrompt + "\n\n" + messages[0].Text
	}

	history := make([]any, 0, len(messages)-1)
	for _, message := range messages[:len(messages)-1] {
		history = append(history, kiroHistoryMessage(message, model))
	}
	current := messages[len(messages)-1]
	if current.Role == "assistant" {
		history = append(history, kiroHistoryMessage(current, model))
		current = normalizedMessage{Role: "user", Text: "(empty placeholder)"}
	}
	currentInput := kiroUserInput(current, model)
	if tools := convertTools(request.Tools); len(tools) > 0 {
		contextObject, _ := currentInput["userInputMessageContext"].(map[string]any)
		if contextObject == nil {
			contextObject = make(map[string]any)
		}
		contextObject["tools"] = tools
		currentInput["userInputMessageContext"] = contextObject
	}

	conversationState := map[string]any{
		"chatTriggerType": "MANUAL",
		"conversationId":  conversationID(request.Messages),
		"currentMessage":  map[string]any{"userInputMessage": currentInput},
	}
	if len(history) > 0 {
		conversationState["history"] = history
	}
	payload := map[string]any{"conversationState": conversationState}
	if cred.ProfileARN != "" {
		payload["profileArn"] = cred.ProfileARN
	}
	encoded, errMarshal := marshalKiroPayload(payload, systemPrompt)
	if errMarshal != nil {
		return nil, "", errMarshal
	}
	return encoded, model, nil
}

func marshalKiroPayload(payload map[string]any, systemPrompt string) ([]byte, error) {
	encoded, errMarshal := json.Marshal(payload)
	if errMarshal != nil {
		return nil, fmt.Errorf("encode Kiro request: %w", errMarshal)
	}
	state, _ := payload["conversationState"].(map[string]any)
	history, _ := state["history"].([]any)
	for len(encoded) > maxKiroPayloadBytes && len(history) > 0 {
		history = history[min(2, len(history)):]
		for len(history) > 0 && historyStartsWithToolResult(history) {
			history = history[min(2, len(history)):]
		}
		if len(history) == 0 {
			delete(state, "history")
		} else {
			state["history"] = history
		}
		prependSystemPrompt(state, history, systemPrompt)
		encoded, errMarshal = json.Marshal(payload)
		if errMarshal != nil {
			return nil, fmt.Errorf("encode trimmed Kiro request: %w", errMarshal)
		}
	}
	if len(encoded) > maxKiroPayloadBytes {
		return nil, fmt.Errorf("Kiro request exceeds the %d-byte payload limit after trimming history", maxKiroPayloadBytes)
	}
	return encoded, nil
}

func prependSystemPrompt(state map[string]any, history []any, systemPrompt string) {
	if systemPrompt == "" {
		return
	}
	var input map[string]any
	if len(history) > 0 {
		entry, _ := history[0].(map[string]any)
		input, _ = entry["userInputMessage"].(map[string]any)
	} else {
		current, _ := state["currentMessage"].(map[string]any)
		input, _ = current["userInputMessage"].(map[string]any)
	}
	if input != nil {
		input["content"] = systemPrompt + "\n\n" + nonEmpty(stringValue(input, "content"), "(empty placeholder)")
	}
}

func historyStartsWithToolResult(history []any) bool {
	if len(history) == 0 {
		return false
	}
	entry, _ := history[0].(map[string]any)
	user, _ := entry["userInputMessage"].(map[string]any)
	context, _ := user["userInputMessageContext"].(map[string]any)
	results, _ := context["toolResults"].([]map[string]any)
	return len(results) > 0
}

func normalizeConversation(messages []normalizedMessage) []normalizedMessage {
	merged := make([]normalizedMessage, 0, len(messages))
	for _, message := range messages {
		if message.Role != "assistant" {
			message.Role = "user"
		}
		if message.Text == "" && len(message.Images) == 0 && len(message.ToolResults) == 0 && len(message.ToolUses) == 0 {
			message.Text = "(empty placeholder)"
		}
		if len(merged) > 0 && merged[len(merged)-1].Role == message.Role {
			if merged[len(merged)-1].Text != "" && message.Text != "" {
				merged[len(merged)-1].Text += "\n\n" + message.Text
			} else if message.Text != "" {
				merged[len(merged)-1].Text = message.Text
			}
			merged[len(merged)-1].Images = append(merged[len(merged)-1].Images, message.Images...)
			merged[len(merged)-1].ToolUses = append(merged[len(merged)-1].ToolUses, message.ToolUses...)
			merged[len(merged)-1].ToolResults = append(merged[len(merged)-1].ToolResults, message.ToolResults...)
			continue
		}
		merged = append(merged, message)
	}
	if len(merged) > 0 && merged[0].Role != "user" {
		merged = append([]normalizedMessage{{Role: "user", Text: "(empty placeholder)"}}, merged...)
	}
	alternating := make([]normalizedMessage, 0, len(merged)+2)
	for _, message := range merged {
		if len(alternating) > 0 && alternating[len(alternating)-1].Role == message.Role {
			other := "assistant"
			if message.Role == "assistant" {
				other = "user"
			}
			alternating = append(alternating, normalizedMessage{Role: other, Text: "(empty placeholder)"})
		}
		alternating = append(alternating, message)
	}
	return alternating
}

func kiroHistoryMessage(message normalizedMessage, model string) map[string]any {
	if message.Role == "assistant" {
		response := map[string]any{"content": nonEmpty(message.Text, "(empty placeholder)")}
		if len(message.ToolUses) > 0 {
			response["toolUses"] = message.ToolUses
		}
		return map[string]any{"assistantResponseMessage": response}
	}
	return map[string]any{"userInputMessage": kiroUserInput(message, model)}
}

func kiroUserInput(message normalizedMessage, model string) map[string]any {
	input := map[string]any{
		"content": nonEmpty(message.Text, "(empty placeholder)"),
		"modelId": model,
		"origin":  "AI_EDITOR",
	}
	if len(message.Images) > 0 {
		input["images"] = message.Images
	}
	if len(message.ToolResults) > 0 {
		input["userInputMessageContext"] = map[string]any{"toolResults": message.ToolResults}
	}
	return input
}

func extractMessageContent(raw json.RawMessage) (string, []map[string]any, []map[string]any) {
	if len(raw) == 0 || string(raw) == "null" {
		return "", nil, nil
	}
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return text, nil, nil
	}
	var blocks []map[string]any
	if json.Unmarshal(raw, &blocks) != nil {
		return string(raw), nil, nil
	}
	var textParts []string
	var images []map[string]any
	var toolResults []map[string]any
	for _, block := range blocks {
		switch strings.ToLower(stringValue(block, "type")) {
		case "text", "input_text", "output_text":
			if value := textValue(block, "text"); value != "" {
				textParts = append(textParts, value)
			}
		case "image_url":
			if imageURL, okURL := block["image_url"].(map[string]any); okURL {
				if image := convertImageURL(stringValue(imageURL, "url")); image != nil {
					images = append(images, image)
				}
			}
		case "image":
			if source, okSource := block["source"].(map[string]any); okSource {
				data := stringValue(source, "data")
				mediaType := stringValue(source, "media_type")
				if data != "" {
					images = append(images, map[string]any{"format": imageFormat(mediaType), "source": map[string]any{"bytes": data}})
				}
			}
		case "tool_result":
			content := block["content"]
			contentText := contentToText(content)
			toolResults = append(toolResults, map[string]any{
				"content":   []any{map[string]any{"text": nonEmpty(contentText, "(empty result)")}},
				"status":    "success",
				"toolUseId": stringValue(block, "tool_use_id", "tool_call_id"),
			})
		}
	}
	return strings.Join(textParts, "\n"), images, toolResults
}

func convertImageURL(value string) map[string]any {
	if !strings.HasPrefix(value, "data:") {
		return nil
	}
	comma := strings.Index(value, ",")
	if comma < 0 {
		return nil
	}
	header := value[5:comma]
	mediaType := strings.Split(header, ";")[0]
	return map[string]any{"format": imageFormat(mediaType), "source": map[string]any{"bytes": value[comma+1:]}}
}

func imageFormat(mediaType string) string {
	format := mediaType
	if slash := strings.LastIndex(mediaType, "/"); slash >= 0 {
		format = mediaType[slash+1:]
	}
	if format == "jpg" || format == "" {
		return "jpeg"
	}
	return format
}

func convertAssistantToolCalls(calls []chatToolCall) []map[string]any {
	out := make([]map[string]any, 0, len(calls))
	for _, call := range calls {
		var input any = map[string]any{}
		if len(call.Function.Arguments) > 0 {
			if errJSON := json.Unmarshal(call.Function.Arguments, &input); errJSON == nil {
				if argumentsText, isText := input.(string); isText {
					if json.Unmarshal([]byte(argumentsText), &input) != nil {
						input = map[string]any{"value": argumentsText}
					}
				}
			}
		}
		out = append(out, map[string]any{
			"name":      call.Function.Name,
			"input":     input,
			"toolUseId": call.ID,
		})
	}
	return out
}

func convertTools(tools []chatTool) []map[string]any {
	out := make([]map[string]any, 0, len(tools))
	for _, tool := range tools {
		name := strings.TrimSpace(tool.Function.Name)
		if name == "" || len(name) > 64 {
			continue
		}
		description := strings.TrimSpace(tool.Function.Description)
		if description == "" {
			description = "Tool: " + name
		}
		if len(description) > 10000 {
			description = description[:10000]
		}
		parameters := sanitizeSchema(tool.Function.Parameters)
		out = append(out, map[string]any{"toolSpecification": map[string]any{
			"name":        name,
			"description": description,
			"inputSchema": map[string]any{"json": parameters},
		}})
	}
	return out
}

func sanitizeSchema(schema map[string]any) map[string]any {
	if schema == nil {
		return map[string]any{"type": "object", "properties": map[string]any{}}
	}
	for _, forbidden := range []string{"$schema", "$id", "examples", "default", "title"} {
		delete(schema, forbidden)
	}
	return schema
}

func contentToText(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case []any:
		var parts []string
		for _, item := range typed {
			if object, okObject := item.(map[string]any); okObject {
				if text := textValue(object, "text", "content"); text != "" {
					parts = append(parts, text)
				}
			}
		}
		return strings.Join(parts, "\n")
	default:
		encoded, _ := json.Marshal(value)
		return string(encoded)
	}
}

func conversationID(messages []chatMessage) string {
	encoded, _ := json.Marshal(messages)
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:8])
}

var (
	standardModelPattern = regexp.MustCompile(`^(claude-(?:haiku|sonnet|opus)-\d+)-(\d{1,2})(?:-(?:\d{8}|latest|\d+))?$`)
	noMinorModelPattern  = regexp.MustCompile(`^(claude-(?:haiku|sonnet|opus)-\d+)(?:-\d{8})?$`)
	legacyModelPattern   = regexp.MustCompile(`^claude-(\d+)-(\d+)-(haiku|sonnet|opus)(?:-(?:\d{8}|latest|\d+))?$`)
)

func normalizeModelName(name string) string {
	name = strings.TrimSpace(strings.TrimPrefix(name, "kiro/"))
	name = regexp.MustCompile(`\[\d+[mk]\]$`).ReplaceAllString(strings.ToLower(name), "")
	if match := standardModelPattern.FindStringSubmatch(name); len(match) > 0 {
		return match[1] + "." + match[2]
	}
	if match := noMinorModelPattern.FindStringSubmatch(name); len(match) > 0 {
		return match[1]
	}
	if match := legacyModelPattern.FindStringSubmatch(name); len(match) > 0 {
		return "claude-" + match[1] + "." + match[2] + "-" + match[3]
	}
	return name
}

func nonEmpty(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}
