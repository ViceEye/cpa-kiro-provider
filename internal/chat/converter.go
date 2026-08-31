package chat

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"regexp"
	"strings"

	"github.com/ViceEye/cpa-provider-nexus/internal/jsonx"
)

const maxKiroPayloadBytes = 900 * 1024

const (
	maxImageDimension = 2000
	maxDecodedPixels  = 64_000_000
	maxImageBase64    = 5 * 1024 * 1024
)

// Kiro rejects empty content, so turns that carry only tool metadata need a
// filler string. It is deliberately a single neutral character: an
// English-looking sentence lands in the history as something the model appears
// to have said, and the model then imitates it on later tool-only turns.
const emptyContentFiller = "."

// legacyEmptyContentFiller is the sentence earlier versions injected. It is
// stripped from incoming history so conversations already polluted with it
// stop echoing it back.
const legacyEmptyContentFiller = "(empty placeholder)"

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

func BuildPayload(raw []byte, requestedModel, profileARN string) ([]byte, string, error) {
	var request chatRequest
	if errUnmarshal := json.Unmarshal(raw, &request); errUnmarshal != nil {
		return nil, "", fmt.Errorf("decode chat-completions request: %w", errUnmarshal)
	}
	model := NormalizeModelName(request.Model)
	if requestedModel != "" {
		model = NormalizeModelName(requestedModel)
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
			// Older builds injected the filler sentence as assistant content;
			// the model then echoed it back as real output. Drop it on the way
			// in so it stops being reinforced.
			if strings.TrimSpace(normalized.Text) == legacyEmptyContentFiller {
				normalized.Text = ""
			}
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
	tools := convertTools(request.Tools)
	if len(tools) == 0 {
		messages = stripToolContent(messages)
	} else {
		messages = normalizeToolPairs(messages)
	}

	history := make([]any, 0, len(messages)-1)
	for _, message := range messages[:len(messages)-1] {
		history = append(history, kiroHistoryMessage(message, model))
	}
	current := messages[len(messages)-1]
	if current.Role == "assistant" {
		history = append(history, kiroHistoryMessage(current, model))
		current = normalizedMessage{Role: "user", Text: emptyContentFiller}
	}
	currentInput := kiroUserInput(current, model)
	if len(tools) > 0 {
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
	if profileARN != "" {
		payload["profileArn"] = profileARN
	}
	encoded, errMarshal := marshalKiroPayload(payload, systemPrompt)
	if errMarshal != nil {
		return nil, "", errMarshal
	}
	return encoded, model, nil
}

func EstimateTokens(raw []byte) (int, error) {
	var request chatRequest
	if errUnmarshal := json.Unmarshal(raw, &request); errUnmarshal != nil {
		return 0, errUnmarshal
	}
	characters := 0
	for _, message := range request.Messages {
		characters += len(message.Content)
	}
	return (characters + 3) / 4, nil
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
		// History is exhausted, so the current message alone is over the limit.
		// Truncating its oversized text is better than failing the turn: a
		// single huge tool result should degrade, not break the conversation.
		encoded, errMarshal = shrinkCurrentMessage(payload, state, len(encoded))
		if errMarshal != nil {
			return nil, errMarshal
		}
	}
	// Trimming only removes history, so the current message can be left holding
	// tool results whose declaring assistant turn was just dropped. Kiro rejects
	// that with "unexpected tool_use_id found in tool_result blocks", so demote
	// any now-orphaned results to plain text.
	if demoteOrphanedCurrentToolResults(state) {
		encoded, errMarshal = json.Marshal(payload)
		if errMarshal != nil {
			return nil, fmt.Errorf("encode repaired Kiro request: %w", errMarshal)
		}
	}
	if len(encoded) > maxKiroPayloadBytes {
		return nil, fmt.Errorf("Kiro request exceeds the %d-byte payload limit after trimming history", maxKiroPayloadBytes)
	}
	return encoded, nil
}

// demoteOrphanedCurrentToolResults converts tool results in the current message
// into plain text when the preceding history entry does not declare their
// toolUseId. It reports whether the payload was modified.
func demoteOrphanedCurrentToolResults(state map[string]any) bool {
	current, _ := state["currentMessage"].(map[string]any)
	input, _ := current["userInputMessage"].(map[string]any)
	context, _ := input["userInputMessageContext"].(map[string]any)
	results, _ := context["toolResults"].([]map[string]any)
	if len(results) == 0 {
		return false
	}

	declared := declaredToolUseIDs(state)
	kept := make([]map[string]any, 0, len(results))
	var orphaned []map[string]any
	for _, result := range results {
		if declared[jsonx.String(result, "toolUseId")] {
			kept = append(kept, result)
			continue
		}
		orphaned = append(orphaned, result)
	}
	if len(orphaned) == 0 {
		return false
	}

	if len(kept) > 0 {
		context["toolResults"] = kept
	} else {
		delete(context, "toolResults")
		if len(context) == 0 {
			delete(input, "userInputMessageContext")
		}
	}
	input["content"] = appendText(jsonx.String(input, "content"), toolResultsText(orphaned))
	return true
}

// declaredToolUseIDs returns the tool use IDs announced by the last history
// entry, which is the only turn Kiro accepts as the partner of a tool result.
func declaredToolUseIDs(state map[string]any) map[string]bool {
	declared := map[string]bool{}
	history, _ := state["history"].([]any)
	if len(history) == 0 {
		return declared
	}
	entry, _ := history[len(history)-1].(map[string]any)
	assistant, _ := entry["assistantResponseMessage"].(map[string]any)
	uses, _ := assistant["toolUses"].([]map[string]any)
	for _, use := range uses {
		declared[jsonx.String(use, "toolUseId")] = true
	}
	return declared
}

const truncationNotice = "\n\n[truncated: content exceeded the upstream request size limit]"

const imageDropNotice = "\n\n[an attached image was dropped: the request exceeded the upstream size limit]"

// dropImagesForSpace removes whole images, largest first, until enough bytes
// are reclaimed. It returns the remaining excess and the surviving images.
func dropImagesForSpace(images []map[string]any, excess int) (int, []map[string]any) {
	kept := make([]map[string]any, len(images))
	copy(kept, images)
	for excess > 0 && len(kept) > 0 {
		target, largest := -1, 0
		for index, image := range kept {
			if size := imageByteSize(image); size > largest {
				target, largest = index, size
			}
		}
		if target < 0 {
			break
		}
		excess -= largest
		kept = append(kept[:target], kept[target+1:]...)
	}
	if excess < 0 {
		excess = 0
	}
	return excess, kept
}

func imageByteSize(image map[string]any) int {
	source, _ := image["source"].(map[string]any)
	return len(jsonx.String(source, "bytes"))
}

// shrinkCurrentMessage trims the largest text in the current message until the
// encoded payload fits. Tool results are trimmed before the prompt itself,
// since a large result is the usual cause.
func shrinkCurrentMessage(payload map[string]any, state map[string]any, encodedSize int) ([]byte, error) {
	current, _ := state["currentMessage"].(map[string]any)
	input, _ := current["userInputMessage"].(map[string]any)
	if input == nil {
		return nil, fmt.Errorf("Kiro request exceeds the %d-byte payload limit after trimming history", maxKiroPayloadBytes)
	}

	excess := encodedSize - maxKiroPayloadBytes + len(truncationNotice) + 1024
	// Images are base64 blobs and cannot be shortened, only dropped. A single
	// screenshot can exceed the whole limit on its own, so they go first.
	if images, _ := input["images"].([]map[string]any); len(images) > 0 {
		remaining, kept := dropImagesForSpace(images, excess)
		excess = remaining
		if len(kept) < len(images) {
			if len(kept) > 0 {
				input["images"] = kept
			} else {
				delete(input, "images")
			}
			input["content"] = jsonx.String(input, "content") + imageDropNotice
		}
	}
	context, _ := input["userInputMessageContext"].(map[string]any)
	if results, _ := context["toolResults"].([]map[string]any); len(results) > 0 {
		excess = truncateToolResults(results, excess)
	}
	if excess > 0 {
		if content := jsonx.String(input, "content"); len(content) > excess {
			input["content"] = content[:len(content)-excess] + truncationNotice
		}
	}

	encoded, errMarshal := json.Marshal(payload)
	if errMarshal != nil {
		return nil, fmt.Errorf("encode truncated Kiro request: %w", errMarshal)
	}
	return encoded, nil
}

// truncateToolResults shortens result text blocks, largest first, and returns
// the number of bytes still to be removed.
func truncateToolResults(results []map[string]any, excess int) int {
	for excess > 0 {
		target, longest := -1, 0
		for index, result := range results {
			if size := toolResultTextSize(result); size > longest {
				target, longest = index, size
			}
		}
		if target < 0 || longest <= len(truncationNotice) {
			return excess
		}
		blocks, _ := results[target]["content"].([]any)
		for _, block := range blocks {
			object, _ := block.(map[string]any)
			text := jsonx.String(object, "text")
			if text == "" {
				continue
			}
			keep := len(text) - excess
			if keep < 0 {
				keep = 0
			}
			object["text"] = text[:keep] + truncationNotice
			excess -= len(text) - keep
			if excess <= 0 {
				return 0
			}
		}
	}
	return excess
}

func toolResultTextSize(result map[string]any) int {
	blocks, _ := result["content"].([]any)
	size := 0
	for _, block := range blocks {
		object, _ := block.(map[string]any)
		size += len(jsonx.String(object, "text"))
	}
	return size
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
		input["content"] = systemPrompt + "\n\n" + jsonx.NonEmpty(jsonx.String(input, "content"), emptyContentFiller)
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
			message.Text = emptyContentFiller
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
		merged = append([]normalizedMessage{{Role: "user", Text: emptyContentFiller}}, merged...)
	}
	alternating := make([]normalizedMessage, 0, len(merged)+2)
	for _, message := range merged {
		if len(alternating) > 0 && alternating[len(alternating)-1].Role == message.Role {
			other := "assistant"
			if message.Role == "assistant" {
				other = "user"
			}
			alternating = append(alternating, normalizedMessage{Role: other, Text: emptyContentFiller})
		}
		alternating = append(alternating, message)
	}
	return alternating
}

func stripToolContent(messages []normalizedMessage) []normalizedMessage {
	for index := range messages {
		message := &messages[index]
		if len(message.ToolUses) == 0 && len(message.ToolResults) == 0 {
			continue
		}
		parts := make([]string, 0, 3)
		if message.Text != "" {
			parts = append(parts, message.Text)
		}
		for _, use := range message.ToolUses {
			name := jsonx.String(use, "name")
			input, _ := json.Marshal(use["input"])
			parts = append(parts, "[Tool Call] "+name+"\n"+string(input))
		}
		for _, result := range message.ToolResults {
			id := jsonx.String(result, "toolUseId")
			content, _ := json.Marshal(result["content"])
			label := "[Tool Result]"
			if id != "" {
				label += " (" + id + ")"
			}
			parts = append(parts, label+"\n"+string(content))
		}
		message.Text = strings.Join(parts, "\n\n")
		message.ToolUses = nil
		message.ToolResults = nil
	}
	return messages
}

// normalizeToolPairs keeps only tool calls and results which form a complete,
// adjacent exchange. Kiro's Claude models reject dangling or mismatched IDs;
// the discarded protocol metadata remains available to the model as text.
func normalizeToolPairs(messages []normalizedMessage) []normalizedMessage {
	for index := range messages {
		message := &messages[index]
		if message.Role != "assistant" || len(message.ToolUses) == 0 {
			continue
		}

		validUses, invalidUses := validToolUses(message.ToolUses)
		message.ToolUses = validUses
		message.Text = appendText(message.Text, toolUsesText(invalidUses))
		if len(validUses) == 0 || index+1 >= len(messages) || messages[index+1].Role != "user" {
			message.Text = appendText(message.Text, toolUsesText(validUses))
			message.ToolUses = nil
			continue
		}

		next := &messages[index+1]
		matchedUses, unmatchedUses, matchedResults, unmatchedResults := matchToolPairs(validUses, next.ToolResults)
		message.ToolUses = matchedUses
		message.Text = appendText(message.Text, toolUsesText(unmatchedUses))
		next.ToolResults = matchedResults
		next.Text = appendText(next.Text, toolResultsText(unmatchedResults))
	}

	for index := range messages {
		message := &messages[index]
		if len(message.ToolResults) == 0 {
			continue
		}
		if index == 0 || messages[index-1].Role != "assistant" || len(messages[index-1].ToolUses) == 0 {
			message.Text = appendText(message.Text, toolResultsText(message.ToolResults))
			message.ToolResults = nil
		}
	}
	return messages
}

func validToolUses(uses []map[string]any) ([]map[string]any, []map[string]any) {
	valid := make([]map[string]any, 0, len(uses))
	invalid := make([]map[string]any, 0)
	seen := make(map[string]struct{}, len(uses))
	for _, use := range uses {
		id := jsonx.String(use, "toolUseId")
		name := jsonx.String(use, "name")
		if id == "" || name == "" {
			invalid = append(invalid, use)
			continue
		}
		if _, exists := seen[id]; exists {
			invalid = append(invalid, use)
			continue
		}
		seen[id] = struct{}{}
		if _, object := use["input"].(map[string]any); !object {
			use["input"] = map[string]any{"value": use["input"]}
		}
		valid = append(valid, use)
	}
	return valid, invalid
}

func matchToolPairs(uses, results []map[string]any) ([]map[string]any, []map[string]any, []map[string]any, []map[string]any) {
	byID := make(map[string]map[string]any, len(results))
	var unmatchedResults []map[string]any
	for _, result := range results {
		id := jsonx.String(result, "toolUseId")
		if id == "" {
			unmatchedResults = append(unmatchedResults, result)
			continue
		}
		if _, exists := byID[id]; exists {
			unmatchedResults = append(unmatchedResults, result)
			continue
		}
		byID[id] = result
	}

	matchedUses := make([]map[string]any, 0, len(uses))
	unmatchedUses := make([]map[string]any, 0)
	matchedResults := make([]map[string]any, 0, len(results))
	for _, use := range uses {
		id := jsonx.String(use, "toolUseId")
		result, exists := byID[id]
		if !exists {
			unmatchedUses = append(unmatchedUses, use)
			continue
		}
		matchedUses = append(matchedUses, use)
		matchedResults = append(matchedResults, result)
		delete(byID, id)
	}
	for _, result := range byID {
		unmatchedResults = append(unmatchedResults, result)
	}
	return matchedUses, unmatchedUses, matchedResults, unmatchedResults
}

func appendText(text, addition string) string {
	if addition == "" {
		return text
	}
	if text == "" {
		return addition
	}
	return text + "\n\n" + addition
}

func toolUsesText(uses []map[string]any) string {
	parts := make([]string, 0, len(uses))
	for _, use := range uses {
		name := jsonx.String(use, "name")
		input, _ := json.Marshal(use["input"])
		parts = append(parts, "[Tool Call] "+jsonx.NonEmpty(name, "unknown")+"\n"+string(input))
	}
	return strings.Join(parts, "\n\n")
}

func toolResultsText(results []map[string]any) string {
	parts := make([]string, 0, len(results))
	for _, result := range results {
		id := jsonx.String(result, "toolUseId")
		content, _ := json.Marshal(result["content"])
		label := "[Tool Result]"
		if id != "" {
			label += " (" + id + ")"
		}
		parts = append(parts, label+"\n"+string(content))
	}
	return strings.Join(parts, "\n\n")
}

func kiroHistoryMessage(message normalizedMessage, model string) map[string]any {
	if message.Role == "assistant" {
		response := map[string]any{"content": jsonx.NonEmpty(message.Text, emptyContentFiller)}
		if len(message.ToolUses) > 0 {
			response["toolUses"] = message.ToolUses
		}
		return map[string]any{"assistantResponseMessage": response}
	}
	return map[string]any{"userInputMessage": kiroUserInput(message, model)}
}

func kiroUserInput(message normalizedMessage, model string) map[string]any {
	input := map[string]any{
		"content": jsonx.NonEmpty(message.Text, emptyContentFiller),
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
		switch strings.ToLower(jsonx.String(block, "type")) {
		case "text", "input_text", "output_text":
			if value := jsonx.Text(block, "text"); value != "" {
				textParts = append(textParts, value)
			}
		case "image_url":
			if imageURL, okURL := block["image_url"].(map[string]any); okURL {
				if image := convertImageURL(jsonx.String(imageURL, "url")); image != nil {
					images = append(images, image)
				}
			}
		case "image":
			if source, okSource := block["source"].(map[string]any); okSource {
				data := jsonx.String(source, "data")
				mediaType := jsonx.String(source, "media_type")
				if data != "" {
					format, normalized := normalizeImage(mediaType, data)
					images = append(images, map[string]any{"format": format, "source": map[string]any{"bytes": normalized}})
				}
			}
		case "tool_result":
			content := block["content"]
			contentText := contentToText(content)
			toolResults = append(toolResults, map[string]any{
				"content":   []any{map[string]any{"text": jsonx.NonEmpty(contentText, "(empty result)")}},
				"status":    "success",
				"toolUseId": jsonx.String(block, "tool_use_id", "tool_call_id"),
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
	format, data := normalizeImage(mediaType, value[comma+1:])
	return map[string]any{"format": format, "source": map[string]any{"bytes": data}}
}

// normalizeImage keeps ordinary images cheap enough for Kiro without adding
// a third-party dependency. Invalid or unsupported data is returned unchanged
// and remains eligible for the payload-size fallback that drops whole images.
func normalizeImage(mediaType, data string) (string, string) {
	if strings.HasPrefix(data, "data:") {
		if comma := strings.IndexByte(data, ','); comma >= 0 {
			header := data[5:comma]
			if mediaType == "" {
				mediaType = strings.Split(header, ";")[0]
			}
			data = data[comma+1:]
		}
	}
	format := imageFormat(mediaType)
	if len(data) > maxImageBase64 {
		return format, data
	}
	decoded, err := decodeBase64(data)
	if err != nil {
		return format, data
	}
	src, _, err := image.Decode(bytes.NewReader(decoded))
	if err != nil {
		return format, data
	}
	bounds := src.Bounds()
	if bounds.Dx() <= 0 || bounds.Dy() <= 0 || bounds.Dx()*bounds.Dy() > maxDecodedPixels {
		return format, data
	}
	resized := src
	resizedChanged := false
	if maxDim := max(bounds.Dx(), bounds.Dy()); maxDim > maxImageDimension {
		resized = resizeNearest(src, maxImageDimension)
		resizedChanged = true
	}

	var out bytes.Buffer
	if format == "png" {
		err = png.Encode(&out, resized)
	} else {
		format = "jpeg"
		err = jpeg.Encode(&out, resized, &jpeg.Options{Quality: 75})
	}
	if err != nil {
		return imageFormat(mediaType), data
	}
	encoded := encodeBase64(out.Bytes())
	if len(encoded) >= len(data) && !resizedChanged {
		return imageFormat(mediaType), data
	}
	return format, encoded
}

func resizeNearest(src image.Image, maxDimension int) image.Image {
	b := src.Bounds()
	scale := float64(maxDimension) / float64(max(b.Dx(), b.Dy()))
	w, h := max(1, int(float64(b.Dx())*scale)), max(1, int(float64(b.Dy())*scale))
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			sx := b.Min.X + x*b.Dx()/w
			sy := b.Min.Y + y*b.Dy()/h
			dst.Set(x, y, src.At(sx, sy))
		}
	}
	return dst
}

func decodeBase64(value string) ([]byte, error) {
	for _, encoding := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
		if decoded, err := encoding.DecodeString(value); err == nil {
			return decoded, nil
		}
	}
	return nil, fmt.Errorf("invalid base64 image")
}

func encodeBase64(value []byte) string { return base64.StdEncoding.EncodeToString(value) }

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
	return sanitizeSchemaMap(schema, true)
}

// sanitizeSchemaMap removes fields rejected by Kiro and normalizes the root
// schema to an object when a top-level combinator is present. Kiro accepts
// oneOf/allOf/anyOf inside nested properties, but rejects them at inputSchema's
// top level.
func sanitizeSchemaMap(schema map[string]any, topLevel bool) map[string]any {
	result := make(map[string]any, len(schema))
	var combinators []map[string]any
	var allOfRequired []any

	for key, value := range schema {
		switch key {
		case "$schema", "$id", "examples", "default", "title", "additionalProperties":
			continue
		case "required":
			if required, ok := value.([]any); ok && len(required) == 0 {
				continue
			}
		case "oneOf", "anyOf", "allOf":
			if topLevel {
				if branches, ok := value.([]any); ok {
					for _, branch := range branches {
						object, ok := branch.(map[string]any)
						if !ok {
							continue
						}
						normalized := sanitizeSchemaMap(object, false)
						combinators = append(combinators, normalized)
						if key == "allOf" {
							if required, ok := normalized["required"].([]any); ok {
								allOfRequired = appendUniqueValues(allOfRequired, required...)
							}
						}
					}
				}
				continue
			}
		}
		result[key] = sanitizeSchemaValue(value)
	}

	if !topLevel {
		return result
	}
	if len(combinators) == 0 {
		if _, exists := result["type"]; !exists {
			result["type"] = "object"
		}
		if _, exists := result["properties"]; !exists {
			result["properties"] = map[string]any{}
		}
		return result
	}

	properties, _ := result["properties"].(map[string]any)
	if properties == nil {
		properties = make(map[string]any)
	}
	for _, branch := range combinators {
		branchProperties, _ := branch["properties"].(map[string]any)
		for name, property := range branchProperties {
			if _, exists := properties[name]; !exists {
				properties[name] = property
			}
		}
	}
	result["type"] = "object"
	result["properties"] = properties
	// Only keep required names that survived the property merge; a required
	// entry without a matching property is rejected upstream.
	if required := filterRequired(allOfRequired, properties); len(required) > 0 {
		result["required"] = required
	}
	return result
}

func filterRequired(required []any, properties map[string]any) []any {
	kept := make([]any, 0, len(required))
	for _, value := range required {
		name, isText := value.(string)
		if !isText {
			continue
		}
		if _, exists := properties[name]; exists {
			kept = append(kept, value)
		}
	}
	return kept
}

func sanitizeSchemaValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		return sanitizeSchemaMap(typed, false)
	case []any:
		out := make([]any, 0, len(typed))
		for _, item := range typed {
			if object, ok := item.(map[string]any); ok {
				out = append(out, sanitizeSchemaMap(object, false))
			} else {
				out = append(out, item)
			}
		}
		return out
	default:
		return value
	}
}

func appendUniqueValues(values []any, additions ...any) []any {
	for _, addition := range additions {
		name, ok := addition.(string)
		if !ok {
			continue
		}
		seen := false
		for _, value := range values {
			if existing, ok := value.(string); ok && existing == name {
				seen = true
				break
			}
		}
		if !seen {
			values = append(values, addition)
		}
	}
	return values
}

func contentToText(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case []any:
		var parts []string
		for _, item := range typed {
			if object, okObject := item.(map[string]any); okObject {
				if text := jsonx.Text(object, "text", "content"); text != "" {
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

func NormalizeModelName(name string) string {
	name = strings.TrimSpace(strings.TrimPrefix(name, "nexus/"))
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
