package cline

import "strings"

// sanitizeClineRequestBody removes tool history that Cline's Responses
// backend cannot validate, while preserving valid messages and tools.
func sanitizeClineRequestBody(body map[string]any) {
	if messages, ok := body["messages"].([]any); ok {
		body["messages"] = sanitizeChatMessages(messages)
	}
	if tools, ok := body["tools"].([]any); ok {
		body["tools"] = sanitizeChatTools(tools)
	}
	if functions, ok := body["functions"].([]any); ok {
		body["functions"] = sanitizeLegacyFunctions(functions)
	}
	if functionCall, ok := body["function_call"].(map[string]any); ok {
		name := strings.TrimSpace(stringValue(functionCall["name"]))
		if name == "" {
			delete(body, "function_call")
		} else {
			functionCall["name"] = name
		}
	}
	if input, ok := body["input"].([]any); ok {
		body["input"] = sanitizeResponsesInput(input)
	}
}

func sanitizeChatMessages(messages []any) []any {
	invalidToolCallIDs := make(map[string]struct{})
	for _, raw := range messages {
		message, ok := raw.(map[string]any)
		if !ok || stringValue(message["role"]) != "assistant" {
			continue
		}

		if calls, ok := message["tool_calls"].([]any); ok {
			validCalls := make([]any, 0, len(calls))
			for _, rawCall := range calls {
				call, ok := rawCall.(map[string]any)
				if !ok {
					validCalls = append(validCalls, rawCall)
					continue
				}
				function, ok := call["function"].(map[string]any)
				name := ""
				if ok {
					name = strings.TrimSpace(stringValue(function["name"]))
				}
				if name == "" {
					if id := strings.TrimSpace(stringValue(call["id"])); id != "" {
						invalidToolCallIDs[id] = struct{}{}
					}
					continue
				}
				function["name"] = name
				call["function"] = function
				validCalls = append(validCalls, call)
			}
			if len(validCalls) == 0 {
				delete(message, "tool_calls")
			} else {
				message["tool_calls"] = validCalls
			}
		}

		if functionCall, ok := message["function_call"].(map[string]any); ok {
			name := strings.TrimSpace(stringValue(functionCall["name"]))
			if name == "" {
				delete(message, "function_call")
			} else {
				functionCall["name"] = name
			}
		}
	}

	cleaned := make([]any, 0, len(messages))
	for _, raw := range messages {
		message, ok := raw.(map[string]any)
		if !ok {
			cleaned = append(cleaned, raw)
			continue
		}
		role := stringValue(message["role"])
		if role == "tool" {
			if _, invalid := invalidToolCallIDs[strings.TrimSpace(stringValue(message["tool_call_id"]))]; invalid {
				continue
			}
		}
		if role == "function" && strings.TrimSpace(stringValue(message["name"])) == "" {
			continue
		}
		cleaned = append(cleaned, message)
	}
	return cleaned
}

func sanitizeChatTools(tools []any) []any {
	cleaned := make([]any, 0, len(tools))
	for _, raw := range tools {
		tool, ok := raw.(map[string]any)
		if !ok {
			cleaned = append(cleaned, raw)
			continue
		}
		function, ok := tool["function"].(map[string]any)
		if !ok {
			cleaned = append(cleaned, tool)
			continue
		}
		name := strings.TrimSpace(stringValue(function["name"]))
		if name == "" {
			continue
		}
		function["name"] = name
		tool["function"] = function
		cleaned = append(cleaned, tool)
	}
	return cleaned
}

func sanitizeLegacyFunctions(functions []any) []any {
	cleaned := make([]any, 0, len(functions))
	for _, raw := range functions {
		function, ok := raw.(map[string]any)
		if !ok {
			cleaned = append(cleaned, raw)
			continue
		}
		name := strings.TrimSpace(stringValue(function["name"]))
		if name == "" {
			continue
		}
		function["name"] = name
		cleaned = append(cleaned, function)
	}
	return cleaned
}

func sanitizeResponsesInput(input []any) []any {
	invalidCallIDs := make(map[string]struct{})
	for _, raw := range input {
		item, ok := raw.(map[string]any)
		if !ok || !isMalformedToolItem(item) {
			continue
		}
		if id := strings.TrimSpace(stringValue(item["call_id"])); id != "" {
			invalidCallIDs[id] = struct{}{}
		}
		if id := strings.TrimSpace(stringValue(item["id"])); id != "" {
			invalidCallIDs[id] = struct{}{}
		}
	}

	cleaned := make([]any, 0, len(input))
	for _, raw := range input {
		item, ok := raw.(map[string]any)
		if !ok {
			cleaned = append(cleaned, raw)
			continue
		}
		itemType := stringValue(item["type"])
		if isMalformedToolItem(item) {
			continue
		}
		if itemType == "function_call_output" {
			if _, invalid := invalidCallIDs[strings.TrimSpace(stringValue(item["call_id"]))]; invalid {
				continue
			}
		}
		cleaned = append(cleaned, item)
	}
	return cleaned
}

func isMalformedToolItem(item map[string]any) bool {
	itemType := stringValue(item["type"])
	if itemType != "function_call" && itemType != "custom_tool_call" {
		return false
	}
	return strings.TrimSpace(stringValue(item["name"])) == ""
}

func stringValue(value any) string {
	text, _ := value.(string)
	return text
}
