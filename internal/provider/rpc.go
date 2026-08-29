package provider

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"

	"gopkg.in/yaml.v3"
)

var configValue atomic.Value

var (
	hostHTTPDoCall         = hostHTTPDo
	hostHTTPDoStreamCall   = hostHTTPDoStream
	readHostHTTPStreamCall = readHostHTTPStream
	callHostCall           = func(string, any) (json.RawMessage, error) { return nil, errors.New("host callback is unavailable") }
)

func SetHostCaller(caller func(string, any) (json.RawMessage, error)) {
	if caller != nil {
		callHostCall = caller
	}
}

func init() {
	configValue.Store(pluginConfig{ImportMode: "reference", LoginMode: defaultLoginMode, ModelPrefix: "kiro/"})
}

func loadedConfig() pluginConfig {
	config, _ := configValue.Load().(pluginConfig)
	if config.ImportMode == "" {
		config.ImportMode = "reference"
	}
	if config.ModelPrefix == "" {
		config.ModelPrefix = "kiro/"
	}
	config.LoginMode = normalizeLoginMode(config.LoginMode)
	if config.SSOStartURL == "" {
		config.SSOStartURL = defaultSSOStartURL
	}
	if config.BrowserSignInURL == "" {
		config.BrowserSignInURL = defaultSignInURL
	}
	if config.BrowserRedirectURI == "" {
		config.BrowserRedirectURI = defaultRedirectURI
	}
	if config.DesktopTokenURL == "" {
		config.DesktopTokenURL = defaultTokenURL
	}
	return config
}

func normalizeLoginMode(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "aws-device", "device", "device-code":
		return "aws-device"
	default:
		return defaultLoginMode
	}
}

func applyConfig(raw []byte) {
	if len(raw) == 0 {
		return
	}
	var root map[string]any
	if yaml.Unmarshal(raw, &root) != nil {
		return
	}
	// CPA passes a plugin-scoped YAML document to plugin.register and
	// plugin.reconfigure. Accept a complete CPA config as well so the plugin can
	// be exercised directly in protocol tests and by older hosts.
	entry := root
	if plugins, okPlugins := root["plugins"].(map[string]any); okPlugins {
		if configs, okConfigs := plugins["configs"].(map[string]any); okConfigs {
			if nested, okEntry := configs[pluginName].(map[string]any); okEntry {
				entry = nested
			}
		}
	}
	encoded, errMarshal := json.Marshal(entry)
	if errMarshal != nil {
		return
	}
	config := loadedConfig()
	if json.Unmarshal(encoded, &config) == nil {
		config.ImportMode = normalizeMode(config.ImportMode)
		config.LoginMode = normalizeLoginMode(config.LoginMode)
		configValue.Store(config)
	}
}

func registration(raw []byte) ([]byte, error) {
	var req struct {
		ConfigYAML []byte `json:"config_yaml"`
	}
	_ = json.Unmarshal(raw, &req)
	applyConfig(req.ConfigYAML)
	return okEnvelope(map[string]any{
		"schema_version": 3,
		"metadata": map[string]any{
			"Name": "Kiro", "Version": pluginVersion, "Author": "cpa-kiro-provider contributors",
			"GitHubRepository": "https://github.com/ViceEye/cpa-kiro-provider",
			"Logo":             "https://kiro.dev/favicon.ico",
			"ConfigFields": []any{
				map[string]any{"Name": "import_mode", "Type": "enum", "EnumValues": []string{"reference", "copy"}, "Description": "Default credential import ownership mode."},
				map[string]any{"Name": "login_mode", "Type": "enum", "EnumValues": []string{"kiro-browser", "aws-device"}, "Description": "First-login flow. aws-device supports Builder ID and organization accounts and is recommended for remote CPA servers."},
				map[string]any{"Name": "static_models", "Type": "array", "Description": "Additional Kiro runtime model IDs."},
				map[string]any{"Name": "api_region", "Type": "string", "Description": "Kiro runtime region, usually us-east-1; independent of the AWS SSO region."},
				map[string]any{"Name": "sso_region", "Type": "string", "Description": "Fallback AWS SSO OIDC region."},
				map[string]any{"Name": "sso_start_url", "Type": "string", "Description": "Determines the aws-device account type: https://view.awsapps.com/start for Builder ID, or the organization's AWS access portal URL for IAM Identity Center."},
				map[string]any{"Name": "browser_redirect_uri", "Type": "string", "Description": "Used only by browser login modes. Production Kiro requires localhost (default http://localhost:3128) or an app.kiro.dev subdomain."},
				map[string]any{"Name": "runtime_base_url", "Type": "string", "Description": "Optional Kiro runtime base URL override for private gateways and tests."},
				map[string]any{"Name": "model_discovery_url", "Type": "string", "Description": "Optional Kiro ListAvailableModels service endpoint override. Defaults to https://q.{region}.amazonaws.com/."},
				map[string]any{"Name": "usage_url", "Type": "string", "Description": "Optional Kiro GetUsageLimits service endpoint override. Defaults to https://q.{region}.amazonaws.com/."},
			},
		},
		"capabilities": map[string]any{
			"auth_provider": true, "model_provider": true, "executor": true, "command_line_plugin": true,
			"management_api":       true,
			"executor_model_scope": "oauth", "executor_input_formats": []string{"chat-completions"}, "executor_output_formats": []string{"chat-completions"},
		},
	})
}

func executeCommandLine(raw []byte) ([]byte, error) {
	var req commandLineExecutionRequest
	if errUnmarshal := json.Unmarshal(raw, &req); errUnmarshal != nil {
		return nil, errUnmarshal
	}
	pathFlag := req.Flags["kiro-import"]
	if triggered, exists := req.TriggeredFlags["kiro-import"]; exists {
		pathFlag = triggered
	}
	path := strings.TrimSpace(pathFlag.Value)
	if path == "" {
		return okEnvelope(commandLineExecutionResponse{Stderr: []byte("--kiro-import requires a credential file or directory\n"), ExitCode: 2})
	}
	mode := loadedConfig().ImportMode
	if modeFlag, exists := req.Flags["kiro-import-mode"]; exists && strings.TrimSpace(modeFlag.Value) != "" {
		mode = modeFlag.Value
	}
	mode = normalizeMode(mode)
	creds, errImport := importCredentials(path, mode)
	if errImport != nil {
		return okEnvelope(commandLineExecutionResponse{Stderr: []byte(errImport.Error() + "\n"), ExitCode: 1})
	}
	auths := make([]authData, 0, len(creds))
	for _, cred := range creds {
		auth, errAuth := authDataFromCredential(cred)
		if errAuth != nil {
			return nil, errAuth
		}
		// Let the host derive the file-based record ID from FileName so the
		// imported file, its manager record, and later auth.parse scans agree.
		auth.ID = ""
		auths = append(auths, auth)
	}
	message := fmt.Sprintf("Imported %d Kiro account(s) in %s mode.\n", len(auths), mode)
	return okEnvelope(commandLineExecutionResponse{Stdout: []byte(message), Auths: auths, ExitCode: 0})
}

func hostHTTPDo(req hostHTTPRequest) (hostHTTPResponse, error) {
	result, errCall := callHostCall("host.http.do", req)
	if errCall != nil {
		return hostHTTPResponse{}, errCall
	}
	var response hostHTTPResponse
	if errJSON := json.Unmarshal(result, &response); errJSON != nil {
		return response, errJSON
	}
	return response, nil
}

func hostHTTPDoStream(req hostHTTPRequest) (hostHTTPStreamResponse, error) {
	result, errCall := callHostCall("host.http.do_stream", req)
	if errCall != nil {
		return hostHTTPStreamResponse{}, errCall
	}
	var response hostHTTPStreamResponse
	if errJSON := json.Unmarshal(result, &response); errJSON != nil {
		return response, errJSON
	}
	return response, nil
}

func readHostHTTPStream(streamID string) (hostHTTPStreamReadResponse, error) {
	result, errCall := callHostCall("host.http.stream_read", map[string]string{"stream_id": streamID})
	if errCall != nil {
		return hostHTTPStreamReadResponse{}, errCall
	}
	var response hostHTTPStreamReadResponse
	if errJSON := json.Unmarshal(result, &response); errJSON != nil {
		return response, errJSON
	}
	return response, nil
}

func readAllHostHTTPStream(streamID string) ([]byte, error) {
	var body []byte
	for {
		chunk, errRead := readHostHTTPStream(streamID)
		if errRead != nil {
			return body, errRead
		}
		body = append(body, chunk.Payload...)
		if chunk.Error != "" {
			return body, fmt.Errorf("%s", chunk.Error)
		}
		if chunk.Done {
			return body, nil
		}
	}
}

func closeHostHTTPStream(streamID string) {
	if streamID == "" {
		return
	}
	_, _ = callHostCall("host.http.stream_close", map[string]string{"stream_id": streamID})
}

func emitPluginStream(streamID string, payload []byte) error {
	_, errCall := callHostCall("host.stream.emit", map[string]any{"stream_id": streamID, "payload": payload})
	return errCall
}

func closePluginStream(streamID, errorMessage string) {
	_, _ = callHostCall("host.stream.close", map[string]any{"stream_id": streamID, "error": errorMessage})
}

func okEnvelope(value any) ([]byte, error) {
	result, errMarshal := json.Marshal(value)
	if errMarshal != nil {
		return nil, errMarshal
	}
	return json.Marshal(envelope{OK: true, Result: result})
}

func errorEnvelope(code, message string, retryable bool, status int) []byte {
	raw, _ := json.Marshal(envelope{OK: false, Error: &envelopeError{Code: code, Message: message, Retryable: retryable, HTTPStatus: status}})
	return raw
}

func randomID() string {
	bytes := make([]byte, 16)
	if _, errRead := rand.Read(bytes); errRead != nil {
		return hex.EncodeToString([]byte(fmt.Sprintf("%p", &bytes)))
	}
	bytes[6] = (bytes[6] & 0x0f) | 0x40
	bytes[8] = (bytes[8] & 0x3f) | 0x80
	hexValue := hex.EncodeToString(bytes)
	return hexValue[:8] + "-" + hexValue[8:12] + "-" + hexValue[12:16] + "-" + hexValue[16:20] + "-" + hexValue[20:]
}

func pluginHTTPStatus(err error) int {
	if typed, ok := err.(statusError); ok {
		return typed.HTTPStatus
	}
	return http.StatusInternalServerError
}
