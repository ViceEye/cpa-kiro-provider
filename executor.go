package main

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

type completionAccumulator struct {
	ID         string
	Model      string
	Content    strings.Builder
	ToolCalls  []map[string]any
	toolIndex  map[string]int
	Usage      int64
	ContextUse float64
}

func executeRequest(raw []byte) ([]byte, error) {
	var req executorRequest
	if errUnmarshal := json.Unmarshal(raw, &req); errUnmarshal != nil {
		return nil, errUnmarshal
	}
	payload, cred, model, response, errExecute := executeKiroNonStream(req)
	_ = payload
	_ = cred
	if errExecute != nil {
		return pluginErrorEnvelope(errExecute), nil
	}
	body, errConvert := convertNonStreamResponse(response.Body, model)
	if errConvert != nil {
		return pluginErrorEnvelope(errConvert), nil
	}
	return okEnvelope(executorResponse{Payload: body, Headers: http.Header{"Content-Type": []string{"application/json"}}})
}

func executeKiroNonStream(req executorRequest) ([]byte, credential, string, hostHTTPResponse, error) {
	cred, errCred := decodeCredential(req.StorageJSON)
	if errCred != nil {
		return nil, credential{}, "", hostHTTPResponse{}, statusError{Code: "invalid_auth", Message: errCred.Error(), HTTPStatus: http.StatusUnauthorized}
	}
	if stableID := validCredentialID(req.AuthID); stableID != "" {
		cred.AuthID = stableID
	} else if cred.AuthID == "" {
		cred.AuthID = credentialID(cred)
	}
	if credentialNeedsRefresh(cred) {
		refreshed, errRefresh := refreshCredential(cred, req.HostCallbackID)
		if errRefresh != nil {
			return nil, cred, "", hostHTTPResponse{}, errRefresh
		}
		cred = refreshed
		persistCredentialBestEffort(req.AuthID, cred)
	}
	withProfile, discovered, errProfile := ensureProfileARN(cred, req.HostCallbackID)
	if errProfile != nil {
		return nil, cred, "", hostHTTPResponse{}, errProfile
	}
	cred = withProfile
	if discovered {
		persistCredentialBestEffort(req.AuthID, cred)
	}
	payload, model, errPayload := buildKiroPayload(req.Payload, req.Model, cred)
	if errPayload != nil {
		return nil, cred, model, hostHTTPResponse{}, statusError{Code: "invalid_request", Message: errPayload.Error(), HTTPStatus: http.StatusBadRequest}
	}
	response, errHTTP := sendKiroNonStream(cred, payload, req.HostCallbackID)
	if errHTTP != nil {
		return payload, cred, model, response, errHTTP
	}
	if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden {
		refreshed, errRefresh := refreshCredential(cred, req.HostCallbackID)
		if errRefresh != nil {
			return payload, cred, model, response, errRefresh
		}
		cred = refreshed
		persistCredentialBestEffort(req.AuthID, cred)
		response, errHTTP = sendKiroNonStream(cred, payload, req.HostCallbackID)
		if errHTTP != nil {
			return payload, cred, model, response, errHTTP
		}
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return payload, cred, model, response, upstreamStatusError("Kiro generateAssistantResponse failed", response.StatusCode, response.Body)
	}
	return payload, cred, model, response, nil
}

func sendKiroNonStream(cred credential, payload []byte, callbackID string) (hostHTTPResponse, error) {
	endpoint := runtimeEndpoint(cred)
	response, errHTTP := hostHTTPDoCall(hostHTTPRequest{HostCallbackID: callbackID, Method: http.MethodPost, URL: endpoint, Headers: kiroHeaders(cred), Body: payload})
	if errHTTP != nil {
		return response, statusError{Code: "network_error", Message: "Kiro request failed", Retryable: true, HTTPStatus: http.StatusBadGateway, Cause: errHTTP}
	}
	return response, nil
}

func executeStream(raw []byte) ([]byte, error) {
	var req executorRequest
	if errUnmarshal := json.Unmarshal(raw, &req); errUnmarshal != nil {
		return nil, errUnmarshal
	}
	if strings.TrimSpace(req.StreamID) == "" {
		return errorEnvelope("executor_error", "stream_id is required", false, http.StatusInternalServerError), nil
	}
	response, model, errPrepare := prepareKiroStream(req)
	if errPrepare != nil {
		return pluginErrorEnvelope(errPrepare), nil
	}
	go func() {
		errRun := consumeKiroStream(req.StreamID, response, model)
		if errRun != nil {
			closePluginStream(req.StreamID, errRun.Error())
			return
		}
		closePluginStream(req.StreamID, "")
	}()
	return okEnvelope(map[string]any{"headers": http.Header{"Content-Type": []string{"text/event-stream"}}})
}

func prepareKiroStream(req executorRequest) (hostHTTPStreamResponse, string, error) {
	cred, errCred := decodeCredential(req.StorageJSON)
	if errCred != nil {
		return hostHTTPStreamResponse{}, "", statusError{Code: "invalid_auth", Message: errCred.Error(), HTTPStatus: http.StatusUnauthorized}
	}
	if stableID := validCredentialID(req.AuthID); stableID != "" {
		cred.AuthID = stableID
	} else if cred.AuthID == "" {
		cred.AuthID = credentialID(cred)
	}
	if credentialNeedsRefresh(cred) {
		refreshed, errRefresh := refreshCredential(cred, req.HostCallbackID)
		if errRefresh != nil {
			return hostHTTPStreamResponse{}, "", errRefresh
		}
		cred = refreshed
		persistCredentialBestEffort(req.AuthID, cred)
	}
	withProfile, discovered, errProfile := ensureProfileARN(cred, req.HostCallbackID)
	if errProfile != nil {
		return hostHTTPStreamResponse{}, "", errProfile
	}
	cred = withProfile
	if discovered {
		persistCredentialBestEffort(req.AuthID, cred)
	}
	payload, model, errPayload := buildKiroPayload(req.Payload, req.Model, cred)
	if errPayload != nil {
		return hostHTTPStreamResponse{}, model, statusError{Code: "invalid_request", Message: errPayload.Error(), HTTPStatus: http.StatusBadRequest}
	}
	response, errOpen := openKiroStream(cred, payload, req.HostCallbackID)
	if errOpen != nil {
		return response, model, errOpen
	}
	if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden {
		closeHostHTTPStream(response.StreamID)
		refreshed, errRefresh := refreshCredential(cred, req.HostCallbackID)
		if errRefresh != nil {
			return hostHTTPStreamResponse{}, model, errRefresh
		}
		cred = refreshed
		persistCredentialBestEffort(req.AuthID, cred)
		response, errOpen = openKiroStream(cred, payload, req.HostCallbackID)
		if errOpen != nil {
			return response, model, errOpen
		}
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		body, errRead := readAllHostHTTPStream(response.StreamID)
		closeHostHTTPStream(response.StreamID)
		if errRead != nil {
			return hostHTTPStreamResponse{}, model, errRead
		}
		return hostHTTPStreamResponse{}, model, upstreamStatusError("Kiro generateAssistantResponse failed", response.StatusCode, body)
	}
	return response, model, nil
}

func consumeKiroStream(streamID string, response hostHTTPStreamResponse, model string) error {
	defer closeHostHTTPStream(response.StreamID)
	parser := &eventStreamParser{}
	acc := newCompletionAccumulator(model)
	for {
		chunk, errRead := readHostHTTPStreamCall(response.StreamID)
		if errRead != nil {
			return statusError{Code: "stream_read_error", Message: "Kiro stream read failed", Retryable: true, HTTPStatus: http.StatusBadGateway, Cause: errRead}
		}
		if len(chunk.Payload) > 0 {
			events, errParse := parser.Feed(chunk.Payload)
			if errParse != nil {
				return statusError{Code: "invalid_event_stream", Message: errParse.Error(), Retryable: true, HTTPStatus: http.StatusBadGateway}
			}
			for _, event := range events {
				if event.Type == "error" {
					return statusError{Code: "upstream_stream_error", Message: nonEmpty(event.Message, "Kiro stream returned an error"), Retryable: true, HTTPStatus: http.StatusBadGateway}
				}
				for _, frame := range acc.streamFrames(event) {
					if errEmit := emitPluginStream(streamID, frame); errEmit != nil {
						return errEmit
					}
				}
			}
		}
		if chunk.Error != "" {
			return statusError{Code: "stream_read_error", Message: chunk.Error, Retryable: true, HTTPStatus: http.StatusBadGateway}
		}
		if chunk.Done {
			break
		}
	}
	tail, errFinish := parser.Finish()
	if errFinish != nil {
		return statusError{Code: "invalid_event_stream", Message: errFinish.Error(), Retryable: true, HTTPStatus: http.StatusBadGateway}
	}
	for _, event := range tail {
		for _, frame := range acc.streamFrames(event) {
			if errEmit := emitPluginStream(streamID, frame); errEmit != nil {
				return errEmit
			}
		}
	}
	if errEmit := emitPluginStream(streamID, acc.finishFrame()); errEmit != nil {
		return errEmit
	}
	// CPA owns the HTTP SSE framing and emits the terminal [DONE] marker when
	// the plugin stream closes. Plugin chunks remain canonical OpenAI JSON.
	return nil
}

func openKiroStream(cred credential, payload []byte, callbackID string) (hostHTTPStreamResponse, error) {
	endpoint := runtimeEndpoint(cred)
	response, errHTTP := hostHTTPDoStreamCall(hostHTTPRequest{HostCallbackID: callbackID, Method: http.MethodPost, URL: endpoint, Headers: kiroHeaders(cred), Body: payload})
	if errHTTP != nil {
		return response, statusError{Code: "network_error", Message: "Kiro streaming request failed", Retryable: true, HTTPStatus: http.StatusBadGateway, Cause: errHTTP}
	}
	return response, nil
}

func runtimeEndpoint(cred credential) string {
	fallback := "https://runtime." + nonEmpty(cred.APIRegion, defaultRegion) + ".kiro.dev"
	baseURL := strings.TrimRight(configuredRegionURL(loadedConfig().RuntimeBaseURL, fallback, cred.APIRegion), "/")
	return baseURL + "/generateAssistantResponse"
}

func kiroHeaders(cred credential) http.Header {
	fingerprint := nonEmpty(cred.Fingerprint, "default-kiro-provider")
	return http.Header{
		"Authorization":               []string{"Bearer " + cred.AccessToken},
		"Content-Type":                []string{"application/x-amz-json-1.0"},
		"X-Amz-Target":                []string{"AmazonCodeWhispererStreamingService.GenerateAssistantResponse"},
		"User-Agent":                  []string{"aws-sdk-js/1.0.27 ua/2.1 os/linux lang/js md/nodejs api/codewhispererstreaming KiroIDE-0.7.45-" + fingerprint},
		"X-Amz-User-Agent":            []string{"aws-sdk-js/1.0.27 KiroIDE-0.7.45-" + fingerprint},
		"X-Amzn-Codewhisperer-Optout": []string{"true"},
		"X-Amzn-Kiro-Agent-Mode":      []string{"vibe"},
		"Amz-Sdk-Invocation-Id":       []string{randomID()},
		"Amz-Sdk-Request":             []string{"attempt=1; max=3"},
	}
}

func convertNonStreamResponse(raw []byte, model string) ([]byte, error) {
	parser := &eventStreamParser{}
	events, errParse := parser.Feed(raw)
	if errParse != nil {
		return nil, statusError{Code: "invalid_event_stream", Message: errParse.Error(), Retryable: true, HTTPStatus: http.StatusBadGateway}
	}
	tail, errFinish := parser.Finish()
	if errFinish != nil {
		return nil, statusError{Code: "invalid_event_stream", Message: errFinish.Error(), Retryable: true, HTTPStatus: http.StatusBadGateway}
	}
	events = append(events, tail...)
	acc := newCompletionAccumulator(model)
	for _, event := range events {
		if event.Type == "error" {
			return nil, statusError{Code: "upstream_stream_error", Message: event.Message, Retryable: true, HTTPStatus: http.StatusBadGateway}
		}
		acc.apply(event)
	}
	message := map[string]any{"role": "assistant", "content": acc.Content.String()}
	finishReason := "stop"
	if len(acc.ToolCalls) > 0 {
		message["tool_calls"] = acc.ToolCalls
		finishReason = "tool_calls"
	}
	response := map[string]any{
		"id": acc.ID, "object": "chat.completion", "created": time.Now().Unix(), "model": "kiro/" + model,
		"choices": []any{map[string]any{"index": 0, "message": message, "finish_reason": finishReason}},
		"usage":   map[string]any{"prompt_tokens": 0, "completion_tokens": 0, "total_tokens": 0, "kiro_credits": acc.Usage, "context_usage_percentage": acc.ContextUse},
	}
	return json.Marshal(response)
}

func newCompletionAccumulator(model string) *completionAccumulator {
	return &completionAccumulator{ID: "chatcmpl-" + strings.ReplaceAll(randomID(), "-", ""), Model: model, toolIndex: make(map[string]int)}
}

func (a *completionAccumulator) apply(event kiroEvent) {
	switch event.Type {
	case "content":
		a.Content.WriteString(event.Content)
	case "tool_start":
		index := len(a.ToolCalls)
		a.toolIndex[event.ToolUseID] = index
		a.ToolCalls = append(a.ToolCalls, map[string]any{"id": event.ToolUseID, "type": "function", "function": map[string]any{"name": event.ToolName, "arguments": ""}})
	case "tool_input":
		a.appendToolArguments(event.ToolUseID, event.ToolInput)
	case "tool_stop":
		if event.ToolInput != "" {
			a.setToolArguments(event.ToolUseID, event.ToolInput)
		}
	case "usage":
		a.Usage = event.Usage
	case "context_usage":
		a.ContextUse = event.ContextUse
	}
}

func (a *completionAccumulator) streamFrames(event kiroEvent) [][]byte {
	a.apply(event)
	delta := map[string]any{}
	switch event.Type {
	case "content":
		delta["content"] = event.Content
	case "tool_start":
		index := a.toolIndex[event.ToolUseID]
		delta["tool_calls"] = []any{map[string]any{"index": index, "id": event.ToolUseID, "type": "function", "function": map[string]any{"name": event.ToolName, "arguments": ""}}}
	case "tool_input":
		index := a.toolIndex[event.ToolUseID]
		delta["tool_calls"] = []any{map[string]any{"index": index, "function": map[string]any{"arguments": event.ToolInput}}}
	default:
		return nil
	}
	return [][]byte{a.chunk(delta, nil)}
}

func (a *completionAccumulator) finishFrame() []byte {
	reason := "stop"
	if len(a.ToolCalls) > 0 {
		reason = "tool_calls"
	}
	return a.chunk(map[string]any{}, &reason)
}

func (a *completionAccumulator) chunk(delta map[string]any, finishReason *string) []byte {
	choice := map[string]any{"index": 0, "delta": delta, "finish_reason": finishReason}
	payload := map[string]any{"id": a.ID, "object": "chat.completion.chunk", "created": time.Now().Unix(), "model": "kiro/" + a.Model, "choices": []any{choice}}
	encoded, _ := json.Marshal(payload)
	return encoded
}

func (a *completionAccumulator) appendToolArguments(id, fragment string) {
	index, exists := a.toolIndex[id]
	if !exists || index >= len(a.ToolCalls) {
		return
	}
	function, _ := a.ToolCalls[index]["function"].(map[string]any)
	current, _ := function["arguments"].(string)
	function["arguments"] = current + fragment
}

func (a *completionAccumulator) setToolArguments(id, value string) {
	index, exists := a.toolIndex[id]
	if !exists || index >= len(a.ToolCalls) {
		return
	}
	function, _ := a.ToolCalls[index]["function"].(map[string]any)
	function["arguments"] = value
}

func persistCredentialBestEffort(authID string, cred credential) {
	if strings.TrimSpace(authID) == "" {
		return
	}
	persistCredentialByNameBestEffort(authID+".json", cred)
}

func countTokens(raw []byte) ([]byte, error) {
	var req executorRequest
	if errUnmarshal := json.Unmarshal(raw, &req); errUnmarshal != nil {
		return nil, errUnmarshal
	}
	var request chatRequest
	_ = json.Unmarshal(req.Payload, &request)
	characters := 0
	for _, message := range request.Messages {
		characters += len(message.Content)
	}
	estimate := (characters + 3) / 4
	body, _ := json.Marshal(map[string]any{"total_tokens": estimate})
	return okEnvelope(executorResponse{Payload: body, Headers: http.Header{"Content-Type": []string{"application/json"}}})
}

func executorHTTPRequest(raw []byte) ([]byte, error) {
	var req struct {
		Method         string      `json:"Method"`
		URL            string      `json:"URL"`
		Headers        http.Header `json:"Headers"`
		Body           []byte      `json:"Body"`
		StorageJSON    []byte      `json:"StorageJSON"`
		HostCallbackID string      `json:"host_callback_id"`
	}
	if errUnmarshal := json.Unmarshal(raw, &req); errUnmarshal != nil {
		return nil, errUnmarshal
	}
	cred, errCred := decodeCredential(req.StorageJSON)
	if errCred != nil {
		return pluginErrorEnvelope(errCred), nil
	}
	for key, values := range kiroHeaders(cred) {
		if len(req.Headers.Values(key)) == 0 {
			req.Headers[key] = values
		}
	}
	resp, errHTTP := hostHTTPDoCall(hostHTTPRequest{HostCallbackID: req.HostCallbackID, Method: req.Method, URL: req.URL, Headers: req.Headers, Body: req.Body})
	if errHTTP != nil {
		return pluginErrorEnvelope(errHTTP), nil
	}
	return okEnvelope(executorHTTPResponse{StatusCode: resp.StatusCode, Headers: resp.Headers, Body: resp.Body})
}
