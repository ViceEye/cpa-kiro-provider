package cline

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
)

// Host bridge — mirrors the kiro-provider plugin's RPC plumbing. Each plugin
// .so gets its own host caller, wired by cliproxy_plugin_init via
// SetHostCaller.

var (
	hostHTTPDoCall       = func(hostHTTPRequest) (hostHTTPResponse, error) { return hostHTTPResponse{}, errors.New("host unavailable") }
	hostHTTPDoStreamCall = func(hostHTTPRequest) (hostHTTPStreamResponse, error) { return hostHTTPStreamResponse{}, errors.New("host unavailable") }
	readHostHTTPStreamCall = func(streamID string) (hostHTTPStreamReadResponse, error) {
		return hostHTTPStreamReadResponse{}, errors.New("host unavailable")
	}
	callHostCall = func(string, any) (json.RawMessage, error) { return nil, errors.New("host unavailable") }
)

// SetHostCaller wires the host function table (called from main.go).
func SetHostCaller(caller func(string, any) (json.RawMessage, error)) {
	if caller != nil {
		callHostCall = caller
	}
}

type hostHTTPRequest struct {
	HostCallbackID string      `json:"HostCallbackID"`
	Method         string      `json:"Method"`
	URL            string      `json:"URL"`
	Headers        http.Header `json:"Headers"`
	Body           []byte      `json:"Body"`
}

type hostHTTPResponse struct {
	StatusCode int         `json:"StatusCode"`
	Headers    http.Header `json:"Headers"`
	Body       []byte      `json:"Body"`
}

type hostHTTPStreamResponse struct {
	StatusCode int         `json:"status_code"`
	Headers    http.Header `json:"headers"`
	StreamID   string      `json:"stream_id"`
}

type hostHTTPStreamReadResponse struct {
	Payload []byte `json:"payload"`
	Error   string `json:"error"`
	Done    bool   `json:"done"`
}

func hostHTTP(req hostHTTPRequest) (hostHTTPResponse, error) {
	result, err := callHostCall("host.http.do", req)
	if err != nil {
		return hostHTTPResponse{}, err
	}
	var response hostHTTPResponse
	if err := json.Unmarshal(result, &response); err != nil {
		return hostHTTPResponse{}, err
	}
	return response, nil
}

func callHost(method string, payload any) (json.RawMessage, error) {
	return callHostCall(method, payload)
}

func hostHTTPDoStream(req hostHTTPRequest) (hostHTTPStreamResponse, error) {
	result, err := callHostCall("host.http.do_stream", req)
	if err != nil {
		return hostHTTPStreamResponse{}, err
	}
	var response hostHTTPStreamResponse
	if err := json.Unmarshal(result, &response); err != nil {
		return hostHTTPStreamResponse{}, err
	}
	return response, nil
}

func emitPluginStream(streamID string, payload []byte) error {
	_, err := callHostCall("host.stream.emit", map[string]any{"stream_id": streamID, "payload": payload})
	return err
}

func closePluginStream(streamID, errorMessage string) {
	_, _ = callHostCall("host.stream.close", map[string]any{"stream_id": streamID, "error": errorMessage})
}

func readHostHTTPStream(streamID string) (hostHTTPStreamReadResponse, error) {
	result, err := callHostCall("host.http.stream_read", map[string]string{"stream_id": streamID})
	if err != nil {
		return hostHTTPStreamReadResponse{}, err
	}
	var response hostHTTPStreamReadResponse
	if err := json.Unmarshal(result, &response); err != nil {
		return hostHTTPStreamReadResponse{}, err
	}
	return response, nil
}

type envelope struct {
	OK     bool            `json:"ok"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *envelopeError  `json:"error,omitempty"`
}

type envelopeError struct {
	Code       string `json:"code"`
	Message    string `json:"message"`
	Retryable  bool   `json:"retryable,omitempty"`
	HTTPStatus int    `json:"http_status,omitempty"`
}

func okEnvelope(value any) ([]byte, error) {
	result, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return json.Marshal(envelope{OK: true, Result: result})
}

func errorEnvelope(code, message string, retryable bool, status int) []byte {
	raw, _ := json.Marshal(envelope{OK: false, Error: &envelopeError{Code: code, Message: message, Retryable: retryable, HTTPStatus: status}})
	return raw
}

// ErrorEnvelope is the exported form used by main.go.
func ErrorEnvelope(code, message string, retryable bool, status int) []byte {
	return errorEnvelope(code, message, retryable, status)
}

func pluginError(err error) []byte {
	if typed, ok := err.(statusError); ok {
		return errorEnvelope(typed.Code, typed.Error(), typed.Retryable, typed.HTTPStatus)
	}
	return errorEnvelope("plugin_error", err.Error(), false, http.StatusInternalServerError)
}

func mustJSON(value any) []byte {
	raw, _ := json.Marshal(value)
	return raw
}

func managementJSON(status int, body map[string]any) []byte {
	return mustJSON(map[string]any{"ok": true, "result": map[string]any{
		"StatusCode": status, "Headers": jsonHeaders(), "Body": mustJSON(body),
	}})
}

func jsonHeaders() http.Header {
	return http.Header{"Content-Type": []string{"application/json; charset=utf-8"}, "Cache-Control": []string{"no-store"}}
}

func randomID() string {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return fmt.Sprintf("%p", &bytes)
	}
	bytes[6] = (bytes[6] & 0x0f) | 0x40
	bytes[8] = (bytes[8] & 0x3f) | 0x80
	hexValue := hex.EncodeToString(bytes)
	return hexValue[:8] + "-" + hexValue[8:12] + "-" + hexValue[12:16] + "-" + hexValue[16:20] + "-" + hexValue[20:]
}
