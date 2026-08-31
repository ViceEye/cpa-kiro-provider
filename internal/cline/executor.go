package cline

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// execute handles a non-streaming chat request. The upstream wraps its
// OpenAI-style response in a {"data": {...}} envelope — unwrap it so CPA
// sees a standard shape.
func Execute(raw []byte) ([]byte, error) {
	var req executorRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		return nil, err
	}
	cred, err := decodeCredential(req.StorageJSON)
	if err != nil {
		return errorEnvelope("invalid_auth", err.Error(), false, http.StatusUnauthorized), nil
	}
	if credentialNeedsRefresh(cred) {
		refreshed, err := refreshCredential(cred, req.HostCallbackID)
		if err != nil {
			return pluginError(err), nil
		}
		cred = refreshed
		persistCredentialBestEffort(cred)
	}

	payload, err := upstreamPayload(req.Payload, req.Model)
	if err != nil {
		return errorEnvelope("invalid_request", err.Error(), false, http.StatusBadRequest), nil
	}
	response, err := hostHTTP(hostHTTPRequest{
		HostCallbackID: req.HostCallbackID,
		Method:         http.MethodPost,
		URL:            apiBase + chatPath,
		Headers: map[string][]string{
			"Authorization": {"Bearer workos:" + cred.AccessToken},
			"Content-Type":  {"application/json"},
			"HTTP-Referer":  {clineIdentityHeaders["HTTP-Referer"]},
			"X-Title":       {clineIdentityHeaders["X-Title"]},
			"X-CLIENT-TYPE": {clineIdentityHeaders["X-CLIENT-TYPE"]},
		},
		Body: payload,
	})
	if err != nil {
		return pluginError(statusErr("upstream_network_error", "Cline request failed: "+err.Error(), true, http.StatusBadGateway)), nil
	}
	if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden {
		refreshed, err := refreshCredential(cred, req.HostCallbackID)
		if err != nil {
			return pluginError(err), nil
		}
		cred = refreshed
		persistCredentialBestEffort(cred)
		response, err = hostHTTP(hostHTTPRequest{
			HostCallbackID: req.HostCallbackID,
			Method:         http.MethodPost,
			URL:            apiBase + chatPath,
			Headers: map[string][]string{
				"Authorization": {"Bearer workos:" + cred.AccessToken},
				"Content-Type":  {"application/json"},
				"HTTP-Referer":  {clineIdentityHeaders["HTTP-Referer"]},
				"X-Title":       {clineIdentityHeaders["X-Title"]},
				"X-CLIENT-TYPE": {clineIdentityHeaders["X-CLIENT-TYPE"]},
			},
			Body: payload,
		})
		if err != nil {
			return pluginError(statusErr("upstream_network_error", "Cline request failed: "+err.Error(), true, http.StatusBadGateway)), nil
		}
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		retryable := response.StatusCode == http.StatusTooManyRequests || response.StatusCode >= 500
		return errorEnvelope("upstream_error", truncateError(response.Body, response.StatusCode), retryable, response.StatusCode), nil
	}

	inner, err := unwrapDataEnvelope(response.Body)
	if err != nil {
		return errorEnvelope("upstream_error", err.Error(), false, http.StatusBadGateway), nil
	}
	return okEnvelope(executorResponse{
		Payload:  inner,
		Headers:  http.Header{"Content-Type": []string{"application/json"}},
		Metadata: map[string]any{"provider": pluginProvider},
	})
}

// executeStream opens an SSE stream and relays chunks, unwrapping the data
// envelope per chunk when present.
func ExecuteStream(raw []byte) ([]byte, error) {
	var req executorRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		return nil, err
	}
	cred, err := decodeCredential(req.StorageJSON)
	if err != nil {
		return errorEnvelope("invalid_auth", err.Error(), false, http.StatusUnauthorized), nil
	}
	if credentialNeedsRefresh(cred) {
		refreshed, err := refreshCredential(cred, req.HostCallbackID)
		if err != nil {
			return pluginError(err), nil
		}
		cred = refreshed
		persistCredentialBestEffort(cred)
	}
	payload, err := upstreamPayload(req.Payload, req.Model)
	if err != nil {
		return errorEnvelope("invalid_request", err.Error(), false, http.StatusBadRequest), nil
	}
	if !strings.Contains(string(payload), `"stream":true`) {
		payload = bytes.Replace(payload, []byte("{"), []byte(`{"stream":true,`), 1)
	}
	response, err := hostHTTPDoStream(hostHTTPRequest{
		HostCallbackID: req.HostCallbackID,
		Method:         http.MethodPost,
		URL:            apiBase + chatPath,
		Headers: map[string][]string{
			"Authorization": {"Bearer workos:" + cred.AccessToken},
			"Content-Type":  {"application/json"},
			"Accept":        {"text/event-stream"},
			"HTTP-Referer":  {clineIdentityHeaders["HTTP-Referer"]},
			"X-Title":       {clineIdentityHeaders["X-Title"]},
			"X-CLIENT-TYPE": {clineIdentityHeaders["X-CLIENT-TYPE"]},
		},
		Body: payload,
	})
	if err != nil {
		return pluginError(statusErr("upstream_network_error", "Cline stream failed: "+err.Error(), true, http.StatusBadGateway)), nil
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		// Stream-mode error responses carry the body inline (no stream id);
		// drain nothing — report the status directly.
		return errorEnvelope("upstream_error", fmt.Sprintf("Cline upstream error (%d)", response.StatusCode),
			response.StatusCode == http.StatusTooManyRequests || response.StatusCode >= 500, response.StatusCode), nil
	}

	go consumeStream(req.StreamID, response.StreamID)
	return okEnvelope(map[string]any{"headers": http.Header{"Content-Type": []string{"text/event-stream"}}})
}

// consumeStream relays upstream SSE to the host stream, unwrapping the
// {"data": {...}} envelope per chunk when present.
func consumeStream(pluginStreamID, upstreamStreamID string) {
	defer closePluginStream(pluginStreamID, "")
	for {
		chunk, err := readHostHTTPStream(upstreamStreamID)
		if err != nil {
			closePluginStream(pluginStreamID, err.Error())
			return
		}
		if len(chunk.Payload) > 0 {
			relayed := relaySSE(chunk.Payload)
			if len(relayed) > 0 {
				if err := emitPluginStream(pluginStreamID, relayed); err != nil {
					return
				}
			}
		}
		if chunk.Error != "" {
			closePluginStream(pluginStreamID, chunk.Error)
			return
		}
		if chunk.Done {
			return
		}
	}
}

// relaySSE rewrites each `data: {...}` line, unwrapping the upstream envelope.
func relaySSE(payload []byte) []byte {
	lines := strings.Split(string(payload), "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || trimmed == "data: [DONE]" {
			out = append(out, line)
			continue
		}
		if !strings.HasPrefix(trimmed, "data:") {
			out = append(out, line)
			continue
		}
		jsonPart := strings.TrimSpace(strings.TrimPrefix(trimmed, "data:"))
		inner, errUnwrap := unwrapDataEnvelope([]byte(jsonPart))
		if errUnwrap != nil {
			out = append(out, line)
			continue
		}
		out = append(out, "data: "+string(inner))
	}
	return []byte(strings.Join(out, "\n"))
}

// upstreamPayload clones the OpenAI body and rewrites the model id to the
// upstream form (strip the cline/ prefix).
func upstreamPayload(payload []byte, model string) ([]byte, error) {
	var body map[string]any
	if err := json.Unmarshal(payload, &body); err != nil {
		return nil, fmt.Errorf("decode chat payload: %w", err)
	}
	body["model"] = strings.TrimPrefix(model, modelPrefix)
	trimmed, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	return trimmed, nil
}

// unwrapDataEnvelope removes Cline's {"data": {...}} response wrapper.
func unwrapDataEnvelope(body []byte) ([]byte, error) {
	var probe struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body, &probe); err != nil || len(probe.Data) == 0 {
		return body, nil
	}
	var probeData struct {
		Choices json.RawMessage `json:"choices"`
	}
	if err := json.Unmarshal(probe.Data, &probeData); err != nil || len(probeData.Choices) == 0 {
		// "data" exists but is not a chat response — pass through unchanged.
		return body, nil
	}
	return []byte(probe.Data), nil
}

func truncateError(body []byte, status int) string {
	text := string(body)
	if len(text) > 300 {
		text = text[:300]
	}
	return fmt.Sprintf("Cline upstream error (%d): %s", status, strings.TrimSpace(text))
}
