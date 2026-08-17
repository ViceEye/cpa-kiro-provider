package provider

import "net/http"

func HandleMethod(method string, raw []byte) ([]byte, error) {
	switch method {
	case "plugin.register", "plugin.reconfigure":
		return registration(raw)
	case "auth.identifier", "executor.identifier":
		return okEnvelope(map[string]string{"identifier": providerID})
	case "auth.parse":
		return parseAuth(raw)
	case "auth.refresh":
		return refreshAuth(raw)
	case "auth.login.start":
		return startLogin(raw)
	case "auth.login.poll":
		return pollLogin(raw)
	case "model.static":
		return okEnvelope(modelResponse{Provider: providerID, Models: staticModels()})
	case "model.for_auth":
		return modelsForAuth(raw)
	case "executor.execute":
		return executeRequest(raw)
	case "executor.execute_stream":
		return executeStream(raw)
	case "executor.count_tokens":
		return countTokens(raw)
	case "executor.http_request":
		return executorHTTPRequest(raw)
	case "command_line.register":
		return okEnvelope(map[string]any{"Flags": []any{
			map[string]any{"Name": "kiro-import", "Usage": "Import Kiro IDE, kiro-cli, Amazon Q, or AWS SSO credentials", "Type": "string"},
			map[string]any{"Name": "kiro-import-mode", "Usage": "Kiro credential ownership mode: reference or copy", "Type": "string", "DefaultValue": "reference"},
		}})
	case "command_line.execute":
		return executeCommandLine(raw)
	case "management.register":
		return registerManagement()
	case "management.handle":
		return handleManagement(raw)
	default:
		return errorEnvelope("unknown_method", "unknown method: "+method, false, http.StatusNotFound), nil
	}
}

func ErrorEnvelope(code, message string, retryable bool, status int) []byte {
	return errorEnvelope(code, message, retryable, status)
}
