package cline

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// clineSSERelay separates SSE framing from the Cline Responses state machine.
type clineSSERelay struct {
	parser    *sseRecordParser
	responses *responsesToChatConverter
}

func newSSERelay(model string) *clineSSERelay {
	return &clineSSERelay{parser: newSSERecordParser(), responses: newResponsesToChatConverter(model)}
}

func (r *clineSSERelay) Feed(payload []byte) ([][]byte, error) {
	if r == nil || len(payload) == 0 {
		return nil, nil
	}
	records, err := r.parser.Feed(payload)
	if err != nil {
		return nil, err
	}
	var out [][]byte
	for _, record := range records {
		frames, err := r.processRecord(record)
		if err != nil {
			return out, err
		}
		out = append(out, frames...)
	}
	return out, nil
}

func (r *clineSSERelay) Finish() ([][]byte, error) {
	if r == nil {
		return nil, nil
	}
	records, err := r.parser.Finish()
	if err != nil {
		return nil, err
	}
	var out [][]byte
	for _, record := range records {
		frames, err := r.processRecord(record)
		if err != nil {
			return nil, err
		}
		out = append(out, frames...)
	}
	frames, err := r.responses.finish()
	if err != nil {
		return out, err
	}
	return append(out, frames...), nil
}

func (r *clineSSERelay) processRecord(record sseRecord) ([][]byte, error) {
	data := bytes.TrimSpace(record.Data)
	if len(data) == 0 || bytes.Equal(data, []byte("[DONE]")) {
		return nil, nil
	}
	data = unwrapClineDataEnvelope(data)
	if isChatCompletionPayload(data) {
		return [][]byte{formatSSEData(data)}, nil
	}
	return r.responses.convert(data, strings.TrimSpace(record.Event))
}

type responseContent struct {
	Text string `json:"text"`
}

type responseItem struct {
	ID        string            `json:"id"`
	Type      string            `json:"type"`
	CallID    string            `json:"call_id"`
	Name      string            `json:"name"`
	Arguments string            `json:"arguments"`
	Input     string            `json:"input"`
	Content   []responseContent `json:"content"`
}

type responseError struct {
	Message string `json:"message"`
}

type responseUsage struct {
	InputTokens        int64 `json:"input_tokens"`
	OutputTokens       int64 `json:"output_tokens"`
	TotalTokens        int64 `json:"total_tokens"`
	PromptTokens       int64 `json:"prompt_tokens"`
	CompletionTokens   int64 `json:"completion_tokens"`
	CachedTokens       int64 `json:"cached_tokens"`
	InputTokensDetails struct {
		CachedTokens int64 `json:"cached_tokens"`
	} `json:"input_tokens_details"`
	PromptTokensDetails struct {
		CachedTokens int64 `json:"cached_tokens"`
	} `json:"prompt_tokens_details"`
}

type responsePayload struct {
	ID                string         `json:"id"`
	Object            string         `json:"object"`
	Type              string         `json:"type"`
	CreatedAt         int64          `json:"created_at"`
	Created           int64          `json:"created"`
	Model             string         `json:"model"`
	Output            []responseItem `json:"output"`
	OutputText        string         `json:"output_text"`
	Usage             *responseUsage `json:"usage"`
	Error             *responseError `json:"error"`
	IncompleteDetails *struct {
		Reason string `json:"reason"`
	} `json:"incomplete_details"`
}

type responseSSEEvent struct {
	Type        string           `json:"type"`
	ID          string           `json:"id"`
	CreatedAt   int64            `json:"created_at"`
	Model       string           `json:"model"`
	Response    *responsePayload `json:"response"`
	Item        *responseItem    `json:"item"`
	ItemID      string           `json:"item_id"`
	CallID      string           `json:"call_id"`
	OutputIndex *int             `json:"output_index"`
	Delta       string           `json:"delta"`
	Arguments   string           `json:"arguments"`
	Input       string           `json:"input"`
	Usage       *responseUsage   `json:"usage"`
	Error       *responseError   `json:"error"`
}

type responsesToolCall struct {
	callID, name  string
	custom        bool
	index         int
	args          strings.Builder
	announced     bool
	customEmitted bool
}

type responsesToChatConverter struct {
	model, chatID string
	created       int64
	started       bool
	finished      bool
	finishReason  string
	usage         *responseUsage
	tools         []*responsesToolCall
	lookup        map[string]*responsesToolCall
}

func newResponsesToChatConverter(model string) *responsesToChatConverter {
	return &responsesToChatConverter{model: model, lookup: make(map[string]*responsesToolCall)}
}

func (c *responsesToChatConverter) convert(raw []byte, hintedType string) ([][]byte, error) {
	var event responseSSEEvent
	if err := json.Unmarshal(raw, &event); err != nil {
		return nil, fmt.Errorf("decode Cline Responses event: %w", err)
	}
	if event.Type == "" {
		event.Type = hintedType
	}
	if !strings.HasPrefix(event.Type, "response.") && event.Type != "error" {
		return [][]byte{formatSSEData(raw)}, nil
	}
	if event.Type == "response.failed" || event.Type == "error" {
		return nil, fmt.Errorf("Cline Responses stream failed: %s", responseEventError(event))
	}
	c.updateMetadata(event)

	switch event.Type {
	case "response.output_text.delta":
		if event.Delta == "" {
			return nil, nil
		}
		return [][]byte{c.chunk(map[string]any{"content": event.Delta}, nil, nil)}, nil
	case "response.reasoning_summary_text.delta", "response.reasoning_text.delta", "response.reasoning_content.delta":
		if event.Delta == "" {
			return nil, nil
		}
		return [][]byte{c.chunk(map[string]any{"reasoning_content": event.Delta}, nil, nil)}, nil
	case "response.refusal.delta":
		if event.Delta == "" {
			return nil, nil
		}
		return [][]byte{c.chunk(map[string]any{"refusal": event.Delta}, nil, nil)}, nil
	case "response.output_item.added":
		if event.Item == nil || !isToolItem(event.Item.Type) {
			return nil, nil
		}
		tool := c.toolForEvent(event)
		c.updateTool(tool, event.Item)
		return c.announceTool(tool), nil
	case "response.function_call_arguments.delta":
		tool := c.toolForEvent(event)
		wasAnnounced := tool.announced
		if event.Delta != "" {
			tool.args.WriteString(event.Delta)
		}
		frames := c.announceTool(tool)
		if wasAnnounced && event.Delta != "" {
			frames = appendFrame(frames, c.toolArgumentsChunk(tool, event.Delta))
		}
		return frames, nil
	case "response.custom_tool_call_input.delta":
		tool := c.toolForEvent(event)
		tool.custom = true
		tool.args.WriteString(event.Delta)
		return c.announceTool(tool), nil
	case "response.function_call_arguments.done":
		tool := c.toolForEvent(event)
		frames := c.announceTool(tool)
		frames = append(frames, c.appendMissingArguments(tool, event.Arguments)...)
		return frames, nil
	case "response.custom_tool_call_input.done":
		tool := c.toolForEvent(event)
		tool.custom = true
		frames := c.announceTool(tool)
		if event.Input != "" {
			tool.args.Reset()
			tool.args.WriteString(event.Input)
		}
		if !tool.announced {
			return frames, nil
		}
		return appendFrame(frames, c.emitCustomArguments(tool)), nil
	case "response.output_item.done":
		if event.Item == nil || !isToolItem(event.Item.Type) {
			return nil, nil
		}
		tool := c.toolForEvent(event)
		c.updateTool(tool, event.Item)
		frames := c.announceTool(tool)
		if tool.custom {
			frames = appendFrame(frames, c.emitCustomArguments(tool))
		} else {
			frames = append(frames, c.appendMissingArguments(tool, event.Item.Arguments)...)
		}
		return frames, nil
	case "response.completed", "response.done", "response.incomplete":
		if event.Response != nil {
			if event.Response.Usage != nil {
				c.usage = event.Response.Usage
			}
			c.updateCompletedTools(event.Response.Output)
		} else if event.Usage != nil {
			c.usage = event.Usage
		}
		if event.Type == "response.incomplete" {
			c.finishReason = "length"
			if event.Response != nil && event.Response.IncompleteDetails != nil && event.Response.IncompleteDetails.Reason == "content_filter" {
				c.finishReason = "content_filter"
			}
		}
		return c.finish()
	default:
		return nil, nil
	}
}

func (c *responsesToChatConverter) finish() ([][]byte, error) {
	if c.finished || !c.started {
		return nil, nil
	}
	var out [][]byte
	announcedTools := 0
	for _, tool := range c.tools {
		if !tool.announced {
			out = append(out, c.announceTool(tool)...)
		}
		if !tool.announced {
			continue
		}
		announcedTools++
		if tool.custom {
			out = appendFrame(out, c.emitCustomArguments(tool))
		}
	}
	if c.finishReason == "" {
		c.finishReason = "stop"
		if announcedTools > 0 {
			c.finishReason = "tool_calls"
		}
	}
	c.finished = true
	out = append(out, c.chunk(map[string]any{}, &c.finishReason, c.usage))
	return out, nil
}

func (c *responsesToChatConverter) updateMetadata(event responseSSEEvent) {
	if event.Response != nil {
		if c.chatID == "" && event.Response.ID != "" {
			c.chatID = chatIDForResponse(event.Response.ID)
		}
		if c.created == 0 {
			c.created = event.Response.CreatedAt
			if c.created == 0 {
				c.created = event.Response.Created
			}
		}
		if c.model == "" {
			c.model = event.Response.Model
		}
	}
	if c.chatID == "" && event.ID != "" {
		c.chatID = chatIDForResponse(event.ID)
	}
	if c.created == 0 {
		c.created = event.CreatedAt
	}
	if c.model == "" {
		c.model = event.Model
	}
	if c.chatID == "" {
		c.chatID = "chatcmpl-" + strings.ReplaceAll(randomID(), "-", "")
	}
	if c.created == 0 {
		c.created = time.Now().Unix()
	}
	if c.model == "" {
		c.model = "unknown"
	}
	c.started = true
}

func (c *responsesToChatConverter) toolForEvent(event responseSSEEvent) *responsesToolCall {
	keys := make([]string, 0, 5)
	if event.ItemID != "" {
		keys = append(keys, event.ItemID)
	}
	if event.CallID != "" {
		keys = append(keys, event.CallID)
	}
	if event.Item != nil {
		if event.Item.ID != "" {
			keys = append(keys, event.Item.ID)
		}
		if event.Item.CallID != "" {
			keys = append(keys, event.Item.CallID)
		}
	}
	if event.OutputIndex != nil {
		keys = append(keys, fmt.Sprintf("output:%d", *event.OutputIndex))
	}
	for _, key := range keys {
		if tool := c.lookup[key]; tool != nil {
			for _, alias := range keys {
				c.lookup[alias] = tool
			}
			return tool
		}
	}
	tool := &responsesToolCall{index: len(c.tools)}
	c.tools = append(c.tools, tool)
	for _, key := range keys {
		c.lookup[key] = tool
	}
	if len(keys) == 0 {
		c.lookup[fmt.Sprintf("tool:%d", tool.index)] = tool
	}
	return tool
}

func (c *responsesToChatConverter) updateTool(tool *responsesToolCall, item *responseItem) {
	if tool == nil || item == nil {
		return
	}
	if item.ID != "" {
		c.lookup[item.ID] = tool
	}
	if item.CallID != "" {
		tool.callID = item.CallID
		c.lookup[item.CallID] = tool
	}
	if name := strings.TrimSpace(item.Name); name != "" {
		tool.name = name
	}
	if item.Type == "custom_tool_call" {
		tool.custom = true
	}
	if tool.callID == "" {
		tool.callID = "call_" + strings.ReplaceAll(randomID(), "-", "")
	}
}

func (c *responsesToChatConverter) updateCompletedTools(items []responseItem) {
	for index := range items {
		item := &items[index]
		if !isToolItem(item.Type) {
			continue
		}
		tool := c.toolForEvent(responseSSEEvent{Item: item, ItemID: item.ID, CallID: item.CallID})
		c.updateTool(tool, item)
		if tool.custom {
			if item.Input != "" {
				tool.args.Reset()
				tool.args.WriteString(item.Input)
			}
			continue
		}
		c.appendMissingArguments(tool, item.Arguments)
	}
}

func (c *responsesToChatConverter) announceTool(tool *responsesToolCall) [][]byte {
	if tool == nil || tool.announced || strings.TrimSpace(tool.name) == "" {
		return nil
	}
	tool.name = strings.TrimSpace(tool.name)
	if tool.callID == "" {
		tool.callID = "call_" + strings.ReplaceAll(randomID(), "-", "")
	}
	tool.announced = true
	frames := [][]byte{c.toolChunk(tool, tool.name, "", true)}
	if !tool.custom && tool.args.Len() > 0 {
		frames = appendFrame(frames, c.toolArgumentsChunk(tool, tool.args.String()))
	}
	return frames
}

func (c *responsesToChatConverter) appendMissingArguments(tool *responsesToolCall, full string) [][]byte {
	if tool == nil || tool.custom || full == "" {
		return nil
	}
	current := tool.args.String()
	if strings.HasPrefix(full, current) {
		suffix := full[len(current):]
		tool.args.Reset()
		tool.args.WriteString(full)
		if suffix == "" {
			return nil
		}
		return [][]byte{c.toolArgumentsChunk(tool, suffix)}
	}
	if full == current {
		return nil
	}
	tool.args.Reset()
	tool.args.WriteString(full)
	if !tool.announced {
		return nil
	}
	return [][]byte{c.toolArgumentsChunk(tool, full)}
}

func (c *responsesToChatConverter) emitCustomArguments(tool *responsesToolCall) []byte {
	if tool == nil || !tool.announced || tool.customEmitted {
		return nil
	}
	tool.customEmitted = true
	arguments, _ := json.Marshal(map[string]string{"input": tool.args.String()})
	return c.toolArgumentsChunk(tool, string(arguments))
}

func (c *responsesToChatConverter) toolArgumentsChunk(tool *responsesToolCall, arguments string) []byte {
	return c.toolChunk(tool, "", arguments, false)
}

func (c *responsesToChatConverter) toolChunk(tool *responsesToolCall, name, arguments string, initial bool) []byte {
	function := map[string]any{"arguments": arguments}
	if initial {
		function["name"] = name
	}
	call := map[string]any{"index": tool.index, "function": function}
	if initial {
		call["id"] = tool.callID
		call["type"] = "function"
	}
	return c.chunk(map[string]any{"tool_calls": []any{call}}, nil, nil)
}

func (c *responsesToChatConverter) chunk(delta map[string]any, finishReason *string, usage *responseUsage) []byte {
	choice := map[string]any{"index": 0, "delta": delta, "finish_reason": finishReason}
	payload := map[string]any{"id": c.chatID, "object": "chat.completion.chunk", "created": c.created, "model": c.model, "choices": []any{choice}}
	if usage != nil {
		payload["usage"] = chatUsage(usage)
	}
	encoded, _ := json.Marshal(payload)
	return formatSSEData(encoded)
}

func formatSSEData(payload []byte) []byte {
	return append(append([]byte("data: "), payload...), '\n', '\n')
}

func appendFrame(frames [][]byte, frame []byte) [][]byte {
	if len(frame) > 0 {
		return append(frames, frame)
	}
	return frames
}

func isToolItem(itemType string) bool {
	return itemType == "function_call" || itemType == "custom_tool_call"
}

func responseEventError(event responseSSEEvent) string {
	if event.Error != nil && event.Error.Message != "" {
		return event.Error.Message
	}
	if event.Response != nil && event.Response.Error != nil && event.Response.Error.Message != "" {
		return event.Response.Error.Message
	}
	return "unknown upstream error"
}

func chatIDForResponse(id string) string {
	if strings.HasPrefix(id, "chatcmpl-") {
		return id
	}
	return "chatcmpl-" + strings.TrimPrefix(id, "resp_")
}

func unwrapClineDataEnvelope(body []byte) []byte {
	var root map[string]json.RawMessage
	if json.Unmarshal(body, &root) != nil {
		return body
	}
	data, ok := root["data"]
	if !ok {
		return body
	}
	var value any
	if json.Unmarshal(data, &value) != nil {
		return body
	}
	switch value.(type) {
	case map[string]any, []any:
		return data
	default:
		return body
	}
}

func isChatCompletionPayload(body []byte) bool {
	var probe struct {
		Choices json.RawMessage `json:"choices"`
	}
	if json.Unmarshal(body, &probe) != nil {
		return false
	}
	return len(probe.Choices) > 0 && !bytes.Equal(bytes.TrimSpace(probe.Choices), []byte("null"))
}

func normalizeClineResponse(body []byte, model string) ([]byte, error) {
	body = bytes.TrimSpace(unwrapClineDataEnvelope(body))
	if isChatCompletionPayload(body) {
		return body, nil
	}
	var event responseSSEEvent
	if err := json.Unmarshal(body, &event); err != nil {
		return nil, fmt.Errorf("decode Cline response: %w", err)
	}
	response := responsePayload{}
	if event.Response != nil && strings.HasPrefix(event.Type, "response.") {
		response = *event.Response
	} else if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("decode Cline response payload: %w", err)
	}
	if response.Object != "response" && response.Type != "response" && len(response.Output) == 0 && response.OutputText == "" && event.Type == "" {
		return body, nil
	}
	return responseToChatCompletion(response, model)
}

func responseToChatCompletion(response responsePayload, fallbackModel string) ([]byte, error) {
	id := "chatcmpl-" + strings.ReplaceAll(randomID(), "-", "")
	if response.ID != "" {
		id = chatIDForResponse(response.ID)
	}
	created := response.CreatedAt
	if created == 0 {
		created = response.Created
	}
	if created == 0 {
		created = time.Now().Unix()
	}
	model := response.Model
	if model == "" {
		model = fallbackModel
	}
	if model == "" {
		model = "unknown"
	}
	text := response.OutputText
	toolCalls := make([]any, 0)
	for _, item := range response.Output {
		if item.Type == "message" && text == "" {
			for _, part := range item.Content {
				text += part.Text
			}
		}
		if !isToolItem(item.Type) {
			continue
		}
		callID := item.CallID
		if callID == "" {
			callID = item.ID
		}
		if callID == "" {
			callID = "call_" + strings.ReplaceAll(randomID(), "-", "")
		}
		arguments := item.Arguments
		if item.Type == "custom_tool_call" {
			encoded, _ := json.Marshal(map[string]string{"input": item.Input})
			arguments = string(encoded)
		}
		name := strings.TrimSpace(item.Name)
		if name == "" {
			continue
		}
		toolCalls = append(toolCalls, map[string]any{"id": callID, "type": "function", "function": map[string]any{"name": name, "arguments": arguments}})
	}

	message := map[string]any{"role": "assistant", "content": text}
	finishReason := "stop"
	if len(toolCalls) > 0 {
		message["content"] = nil
		message["tool_calls"] = toolCalls
		finishReason = "tool_calls"
	}
	result := map[string]any{"id": id, "object": "chat.completion", "created": created, "model": model, "choices": []any{map[string]any{"index": 0, "message": message, "finish_reason": finishReason}}}
	if response.Usage != nil {
		result["usage"] = chatUsage(response.Usage)
	}
	return json.Marshal(result)
}

func chatUsage(usage *responseUsage) map[string]any {
	if usage == nil {
		return nil
	}
	prompt := usage.InputTokens
	if prompt == 0 {
		prompt = usage.PromptTokens
	}
	completion := usage.OutputTokens
	if completion == 0 {
		completion = usage.CompletionTokens
	}
	total := usage.TotalTokens
	if total == 0 {
		total = prompt + completion
	}
	cached := usage.CachedTokens
	if cached == 0 {
		cached = usage.InputTokensDetails.CachedTokens
	}
	if cached == 0 {
		cached = usage.PromptTokensDetails.CachedTokens
	}
	result := map[string]any{"prompt_tokens": prompt, "completion_tokens": completion, "total_tokens": total}
	if cached > 0 {
		result["prompt_tokens_details"] = map[string]any{"cached_tokens": cached}
	}
	return result
}
