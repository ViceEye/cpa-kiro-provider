package cline

import (
	"encoding/json"
	"net/http"

	"github.com/ViceEye/cpa-provider-nexus/internal/pluginrpc"
)

// Cline keeps these aliases local so its protocol code stays focused on the
// upstream API while the host callback wire format has one implementation.
type hostHTTPRequest = pluginrpc.HTTPRequest
type hostHTTPResponse = pluginrpc.HTTPResponse
type hostHTTPStreamResponse = pluginrpc.HTTPStreamResponse
type hostHTTPStreamReadResponse = pluginrpc.HTTPStreamReadResponse

var requestObserver func(authID, model string, success bool, message string)

// SetHostCaller is retained for package compatibility; all adapters now share
// the same host callback.
func SetHostCaller(caller func(string, any) (json.RawMessage, error)) {
	pluginrpc.SetCaller(caller)
}

// SetRequestObserver lets the provider package record completed Cline model
// requests without creating an import cycle.
func SetRequestObserver(observer func(authID, model string, success bool, message string)) {
	requestObserver = observer
}

func observeRequest(authID, model string, success bool, message string) {
	if requestObserver != nil {
		requestObserver(authID, model, success, message)
	}
}

func hostHTTP(req hostHTTPRequest) (hostHTTPResponse, error) {
	return pluginrpc.Do(req)
}

func callHost(method string, payload any) (json.RawMessage, error) {
	return pluginrpc.Call(method, payload)
}

func hostHTTPDoStream(req hostHTTPRequest) (hostHTTPStreamResponse, error) {
	return pluginrpc.DoStream(req)
}

func emitPluginStream(streamID string, payload []byte) error {
	return pluginrpc.EmitStream(streamID, payload)
}

func closePluginStream(streamID, errorMessage string) {
	pluginrpc.ClosePluginStream(streamID, errorMessage)
}

func readHostHTTPStream(streamID string) (hostHTTPStreamReadResponse, error) {
	return pluginrpc.ReadStream(streamID)
}

func readAllHostHTTPStream(streamID string) ([]byte, error) {
	return pluginrpc.ReadAllStream(streamID)
}

func closeHostHTTPStream(streamID string) {
	pluginrpc.CloseStream(streamID)
}

type envelope = pluginrpc.Envelope
type envelopeError = pluginrpc.EnvelopeError

func okEnvelope(value any) ([]byte, error) {
	return pluginrpc.OK(value)
}

func errorEnvelope(code, message string, retryable bool, status int) []byte {
	return pluginrpc.Error(code, message, retryable, status)
}

func pluginError(err error) []byte {
	if typed, ok := err.(statusError); ok {
		return errorEnvelope(typed.Code, typed.Error(), typed.Retryable, typed.HTTPStatus)
	}
	return errorEnvelope("plugin_error", err.Error(), false, http.StatusInternalServerError)
}

func mustJSON(value any) []byte {
	return pluginrpc.MustJSON(value)
}

func managementJSON(status int, body map[string]any) []byte {
	return pluginrpc.ManagementJSON(status, body)
}

func jsonHeaders() http.Header {
	return pluginrpc.JSONHeaders()
}

func randomID() string {
	return pluginrpc.RandomID()
}

// ErrorEnvelope is the exported form used by callers at the plugin boundary.
func ErrorEnvelope(code, message string, retryable bool, status int) []byte {
	return errorEnvelope(code, message, retryable, status)
}
