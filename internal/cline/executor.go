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
	authID := requestAuthID(req, cred)
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
		Headers:        clineHTTPHeaders(cred.AccessToken, ""),
		Body:           payload,
	})
	if err != nil {
		observeRequest(authID, req.Model, false, err.Error())
		return pluginError(statusErr("upstream_network_error", "Cline request failed: "+err.Error(), true, http.StatusBadGateway)), nil
	}
	if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden {
		refreshed, err := refreshCredential(cred, req.HostCallbackID)
		if err != nil {
			observeRequest(authID, req.Model, false, err.Error())
			return pluginError(err), nil
		}
		cred = refreshed
		persistCredentialBestEffort(cred)
		response, err = hostHTTP(hostHTTPRequest{
			HostCallbackID: req.HostCallbackID,
			Method:         http.MethodPost,
			URL:            apiBase + chatPath,
			Headers:        clineHTTPHeaders(cred.AccessToken, ""),
			Body:           payload,
		})
		if err != nil {
			observeRequest(authID, req.Model, false, err.Error())
			return pluginError(statusErr("upstream_network_error", "Cline request failed: "+err.Error(), true, http.StatusBadGateway)), nil
		}
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		retryable := response.StatusCode == http.StatusTooManyRequests || response.StatusCode >= 500
		message := truncateError(response.Body, response.StatusCode)
		observation := strings.TrimSpace(string(response.Body))
		if observation == "" {
			observation = message
		}
		observeRequest(authID, req.Model, false, observation)
		return errorEnvelope("upstream_error", message, retryable, response.StatusCode), nil
	}

	inner, err := normalizeClineResponse(response.Body, req.Model)
	if err != nil {
		observeRequest(authID, req.Model, false, err.Error())
		return errorEnvelope("upstream_error", err.Error(), false, http.StatusBadGateway), nil
	}
	observeRequest(authID, req.Model, true, "")
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
	authID := requestAuthID(req, cred)
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
		Headers:        clineHTTPHeaders(cred.AccessToken, "text/event-stream"),
		Body:           payload,
	})
	if err != nil {
		observeRequest(authID, req.Model, false, err.Error())
		return pluginError(statusErr("upstream_network_error", "Cline stream failed: "+err.Error(), true, http.StatusBadGateway)), nil
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		body, errRead := readAllHostHTTPStream(response.StreamID)
		closeHostHTTPStream(response.StreamID)
		message := truncateError(body, response.StatusCode)
		if errRead != nil && len(body) == 0 {
			message = fmt.Sprintf("Cline upstream error (%d): %s", response.StatusCode, errRead.Error())
		}
		observation := strings.TrimSpace(string(body))
		if observation == "" {
			observation = message
		}
		observeRequest(authID, req.Model, false, observation)
		return errorEnvelope("upstream_error", message,
			response.StatusCode == http.StatusTooManyRequests || response.StatusCode >= 500, response.StatusCode), nil
	}

	go consumeStream(req.StreamID, response.StreamID, authID, req.Model)
	return okEnvelope(map[string]any{"headers": http.Header{"Content-Type": []string{"text/event-stream"}}})
}

// consumeStream relays upstream SSE to the host stream, unwrapping the
// {"data": {...}} envelope per chunk when present.
func consumeStream(pluginStreamID, upstreamStreamID, authID, model string) {
	streamError := ""
	completed := false
	defer func() {
		if completed {
			observeRequest(authID, model, true, "")
		} else if streamError != "" {
			observeRequest(authID, model, false, streamError)
		}
		closePluginStream(pluginStreamID, streamError)
	}()
	relay := newSSERelay(model)
	for {
		chunk, err := readHostHTTPStream(upstreamStreamID)
		if err != nil {
			streamError = err.Error()
			return
		}
		if len(chunk.Payload) > 0 {
			frames, relayErr := relay.Feed(chunk.Payload)
			if relayErr != nil {
				streamError = relayErr.Error()
				return
			}
			for _, frame := range frames {
				if err := emitPluginStream(pluginStreamID, frame); err != nil {
					streamError = err.Error()
					return
				}
			}
		}
		if chunk.Error != "" {
			streamError = chunk.Error
			return
		}
		if chunk.Done {
			frames, relayErr := relay.Finish()
			if relayErr != nil {
				streamError = relayErr.Error()
				return
			}
			for _, frame := range frames {
				if err := emitPluginStream(pluginStreamID, frame); err != nil {
					streamError = err.Error()
					return
				}
			}
			completed = true
			return
		}
	}
}

func requestAuthID(req executorRequest, cred credential) string {
	if authID := strings.TrimSpace(cred.AuthID); authID != "" {
		return authID
	}
	if strings.TrimSpace(cred.Email) != "" || strings.TrimSpace(cred.RefreshToken) != "" {
		return credentialID(cred)
	}
	return strings.TrimSpace(req.AuthID)
}

// relaySSE is kept for single-chunk callers and tests. Runtime streaming uses
// consumeStream's stateful relay so events split across host chunks survive.
func relaySSE(payload []byte) []byte {
	relay := newSSERelay("unknown")
	frames, err := relay.Feed(payload)
	if err != nil {
		return payload
	}
	tail, err := relay.Finish()
	if err != nil {
		return payload
	}
	frames = append(frames, tail...)
	output := bytes.Join(frames, nil)
	if bytes.Contains(payload, []byte("data: [DONE]")) {
		output = append(output, []byte("data: [DONE]\n")...)
	}
	return output
}

// upstreamPayload clones the OpenAI body and rewrites the model id to the
// upstream form (strip the cline/ prefix).
func upstreamPayload(payload []byte, model string) ([]byte, error) {
	var body map[string]any
	if err := json.Unmarshal(payload, &body); err != nil {
		return nil, fmt.Errorf("decode chat payload: %w", err)
	}
	body["model"] = strings.TrimPrefix(model, modelPrefix)
	sanitizeClineRequestBody(body)
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
