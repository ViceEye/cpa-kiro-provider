package provider

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"

	"github.com/ViceEye/cpa-provider-nexus/internal/pluginrpc"
	"gopkg.in/yaml.v3"
)

var configValue atomic.Value

var (
	hostHTTPDoCall       = func(req hostHTTPRequest) (hostHTTPResponse, error) { return pluginrpc.DoWithCaller(callHostCall, req) }
	hostHTTPDoStreamCall = func(req hostHTTPRequest) (hostHTTPStreamResponse, error) {
		return pluginrpc.DoStreamWithCaller(callHostCall, req)
	}
	readHostHTTPStreamCall = func(streamID string) (hostHTTPStreamReadResponse, error) {
		return pluginrpc.ReadStreamWithCaller(callHostCall, streamID)
	}
	callHostCall = pluginrpc.Call
)

func SetHostCaller(caller func(string, any) (json.RawMessage, error)) {
	if caller != nil {
		pluginrpc.SetCaller(caller)
	}
}

func init() {
	configValue.Store(pluginConfig{ImportMode: "reference", LoginMode: defaultLoginMode, ModelPrefix: "nexus/"})
}

func loadedConfig() pluginConfig {
	config, _ := configValue.Load().(pluginConfig)
	if config.ImportMode == "" {
		config.ImportMode = "reference"
	}
	if config.ModelPrefix == "" {
		config.ModelPrefix = "nexus/"
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
		configureQuotaTriggerSchedules(config.QuotaTriggers)
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
			"Name": "Nexus", "Version": pluginVersion, "Author": "cpa-provider-nexus contributors",
			"GitHubRepository": "https://github.com/ViceEye/cpa-provider-nexus",
			"Logo":             nexusLogoPath,
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
				map[string]any{"Name": "quota_triggers", "Type": "array", "Description": "Daily Codex and Antigravity model activation plans persisted in the Nexus console."},
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

func readAllHostHTTPStream(streamID string) ([]byte, error) {
	return pluginrpc.ReadAllStreamWithCaller(callHostCall, streamID)
}

func closeHostHTTPStream(streamID string) {
	pluginrpc.CloseStreamWithCaller(callHostCall, streamID)
}

func emitPluginStream(streamID string, payload []byte) error {
	return pluginrpc.EmitStreamWithCaller(callHostCall, streamID, payload)
}

func closePluginStream(streamID, errorMessage string) {
	pluginrpc.ClosePluginStreamWithCaller(callHostCall, streamID, errorMessage)
}

func okEnvelope(value any) ([]byte, error) {
	return pluginrpc.OK(value)
}

func errorEnvelope(code, message string, retryable bool, status int) []byte {
	return pluginrpc.Error(code, message, retryable, status)
}

func mustJSON(value any) []byte {
	return pluginrpc.MustJSON(value)
}

func jsonHeaders() http.Header {
	return pluginrpc.JSONHeaders()
}

func managementJSON(status int, body map[string]any) []byte {
	return pluginrpc.ManagementJSON(status, body)
}

func randomID() string {
	return pluginrpc.RandomID()
}

func pluginHTTPStatus(err error) int {
	if typed, ok := err.(statusError); ok {
		return typed.HTTPStatus
	}
	return http.StatusInternalServerError
}
