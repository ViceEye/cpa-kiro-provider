package pluginrpc

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
)

// Caller is the host callback exposed by the plugin ABI.
type Caller func(string, any) (json.RawMessage, error)

var caller Caller = func(string, any) (json.RawMessage, error) {
	return nil, errors.New("host callback is unavailable")
}

// SetCaller wires the host callback shared by all protocol adapters.
func SetCaller(value Caller) {
	if value != nil {
		caller = value
	}
}

func Call(method string, payload any) (json.RawMessage, error) {
	return caller(method, payload)
}

type HTTPRequest struct {
	HostCallbackID string      `json:"host_callback_id,omitempty"`
	Method         string      `json:"method"`
	URL            string      `json:"url"`
	Headers        http.Header `json:"headers,omitempty"`
	Body           []byte      `json:"body,omitempty"`
}

type HTTPResponse struct {
	StatusCode int         `json:"StatusCode"`
	Headers    http.Header `json:"Headers"`
	Body       []byte      `json:"Body"`
}

type HTTPStreamResponse struct {
	StatusCode int         `json:"status_code"`
	Headers    http.Header `json:"headers"`
	StreamID   string      `json:"stream_id"`
}

type HTTPStreamReadResponse struct {
	Payload []byte `json:"payload"`
	Error   string `json:"error"`
	Done    bool   `json:"done"`
}

func Do(req HTTPRequest) (HTTPResponse, error) {
	return DoWithCaller(caller, req)
}

func DoWithCaller(call Caller, req HTTPRequest) (HTTPResponse, error) {
	result, err := call("host.http.do", req)
	if err != nil {
		return HTTPResponse{}, err
	}
	var response HTTPResponse
	if err := json.Unmarshal(result, &response); err != nil {
		return HTTPResponse{}, err
	}
	return response, nil
}

func DoStream(req HTTPRequest) (HTTPStreamResponse, error) {
	return DoStreamWithCaller(caller, req)
}

func DoStreamWithCaller(call Caller, req HTTPRequest) (HTTPStreamResponse, error) {
	result, err := call("host.http.do_stream", req)
	if err != nil {
		return HTTPStreamResponse{}, err
	}
	var response HTTPStreamResponse
	if err := json.Unmarshal(result, &response); err != nil {
		return HTTPStreamResponse{}, err
	}
	return response, nil
}

func ReadStream(streamID string) (HTTPStreamReadResponse, error) {
	return ReadStreamWithCaller(caller, streamID)
}

func ReadStreamWithCaller(call Caller, streamID string) (HTTPStreamReadResponse, error) {
	result, err := call("host.http.stream_read", map[string]string{"stream_id": streamID})
	if err != nil {
		return HTTPStreamReadResponse{}, err
	}
	var response HTTPStreamReadResponse
	if err := json.Unmarshal(result, &response); err != nil {
		return HTTPStreamReadResponse{}, err
	}
	return response, nil
}

func ReadAllStream(streamID string) ([]byte, error) {
	return ReadAllStreamWithCaller(caller, streamID)
}

func ReadAllStreamWithCaller(call Caller, streamID string) ([]byte, error) {
	var body []byte
	for {
		chunk, err := ReadStreamWithCaller(call, streamID)
		if err != nil {
			return body, err
		}
		body = append(body, chunk.Payload...)
		if chunk.Error != "" {
			return body, errors.New(chunk.Error)
		}
		if chunk.Done {
			return body, nil
		}
	}
}

func CloseStream(streamID string) {
	CloseStreamWithCaller(caller, streamID)
}

func CloseStreamWithCaller(call Caller, streamID string) {
	if streamID == "" {
		return
	}
	_, _ = call("host.http.stream_close", map[string]string{"stream_id": streamID})
}

func EmitStream(streamID string, payload []byte) error {
	return EmitStreamWithCaller(caller, streamID, payload)
}

func EmitStreamWithCaller(call Caller, streamID string, payload []byte) error {
	_, err := call("host.stream.emit", map[string]any{"stream_id": streamID, "payload": payload})
	return err
}

func ClosePluginStream(streamID, errorMessage string) {
	ClosePluginStreamWithCaller(caller, streamID, errorMessage)
}

func ClosePluginStreamWithCaller(call Caller, streamID, errorMessage string) {
	_, _ = call("host.stream.close", map[string]any{"stream_id": streamID, "error": errorMessage})
}

type Envelope struct {
	OK     bool            `json:"ok"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *EnvelopeError  `json:"error,omitempty"`
}

type EnvelopeError struct {
	Code       string `json:"code"`
	Message    string `json:"message"`
	Retryable  bool   `json:"retryable,omitempty"`
	HTTPStatus int    `json:"http_status,omitempty"`
}

func OK(value any) ([]byte, error) {
	result, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return json.Marshal(Envelope{OK: true, Result: result})
}

func Error(code, message string, retryable bool, status int) []byte {
	raw, _ := json.Marshal(Envelope{OK: false, Error: &EnvelopeError{
		Code: code, Message: message, Retryable: retryable, HTTPStatus: status,
	}})
	return raw
}

func MustJSON(value any) []byte {
	raw, _ := json.Marshal(value)
	return raw
}

func JSONHeaders() http.Header {
	return http.Header{
		"Content-Type":  []string{"application/json; charset=utf-8"},
		"Cache-Control": []string{"no-store"},
	}
}

func ManagementJSON(status int, body map[string]any) []byte {
	return MustJSON(map[string]any{"ok": true, "result": map[string]any{
		"StatusCode": status, "Headers": JSONHeaders(), "Body": MustJSON(body),
	}})
}

func RandomID() string {
	data := make([]byte, 16)
	if _, err := rand.Read(data); err != nil {
		return fmt.Sprintf("%p", &data)
	}
	data[6] = (data[6] & 0x0f) | 0x40
	data[8] = (data[8] & 0x3f) | 0x80
	value := hex.EncodeToString(data)
	return value[:8] + "-" + value[8:12] + "-" + value[12:16] + "-" + value[16:20] + "-" + value[20:]
}
