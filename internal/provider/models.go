package provider

import (
	"encoding/json"
	"sort"
	"strconv"
	"strings"

	"github.com/ViceEye/cpa-provider-nexus/internal/chat"
	"github.com/ViceEye/cpa-provider-nexus/internal/cline"
)

var fallbackModels = []string{
	"auto",
	"claude-sonnet-4",
	"claude-sonnet-4.5",
	"claude-sonnet-4.6",
	"claude-haiku-4.5",
	"claude-opus-4.5",
	"claude-opus-4.6",
	"claude-opus-4.7",
	"deepseek-3.2",
	"glm-5",
	"minimax-m2.1",
	"minimax-m2.5",
	"qwen3-coder-next",
}

func staticModels() []modelInfo {
	models := append([]string(nil), fallbackModels...)
	models = append(models, loadedConfig().StaticModels...)
	return modelInfos(models)
}

func modelInfos(names []string) []modelInfo {
	seen := make(map[string]struct{})
	models := make([]modelInfo, 0, len(names))
	for _, nativeName := range names {
		nativeName = chat.NormalizeModelName(nativeName)
		if nativeName == "" {
			continue
		}
		id := "nexus/" + nativeName
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		models = append(models, modelInfo{
			ID: id, Object: "model", OwnedBy: providerID, Type: "chat",
			DisplayName: "Kiro " + nativeName, Name: nativeName,
			Description:     "Kiro runtime model " + nativeName,
			InputTokenLimit: 200000, OutputTokenLimit: 8192,
			ContextLength: 200000, MaxCompletionTokens: 8192,
			SupportedGenerationMethods: []string{"chat"},
			SupportedParameters:        []string{"messages", "stream", "tools", "tool_choice"},
			SupportedInputModalities:   []string{"text", "image"},
			SupportedOutputModalities:  []string{"text"}, UserDefined: true,
		})
	}
	sort.Slice(models, func(i, j int) bool { return models[i].ID < models[j].ID })
	return models
}

func catalogModelInfos(catalog []kiroCatalogModel) []modelInfo {
	seen := make(map[string]struct{})
	models := make([]modelInfo, 0, len(catalog))
	for _, item := range catalog {
		nativeName := chat.NormalizeModelName(item.ModelID)
		if nativeName == "" {
			continue
		}
		id := "nexus/" + nativeName
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}

		inputLimit := item.TokenLimits.MaxInputTokens
		if inputLimit <= 0 {
			inputLimit = 200000
		}
		outputLimit := item.TokenLimits.MaxOutputTokens
		if outputLimit <= 0 {
			outputLimit = 8192
		}

		modalities := make([]string, 0, len(item.SupportedInputTypes))
		for _, inputType := range item.SupportedInputTypes {
			switch strings.ToUpper(strings.TrimSpace(inputType)) {
			case "TEXT":
				modalities = appendUniqueString(modalities, "text")
			case "IMAGE":
				modalities = appendUniqueString(modalities, "image")
			}
		}
		if len(modalities) == 0 {
			modalities = []string{"text"}
		}

		displayName := strings.TrimSpace(item.ModelName)
		if displayName == "" {
			displayName = "Kiro " + nativeName
		}
		description := strings.TrimSpace(item.Description)
		if item.RateMultiplier > 0 && strings.TrimSpace(item.RateUnit) != "" {
			rate := strconv.FormatFloat(item.RateMultiplier, 'f', -1, 64)
			description = strings.TrimSpace(description + " (" + rate + " " + item.RateUnit + ")")
		}

		models = append(models, modelInfo{
			ID: id, Object: "model", OwnedBy: providerID, Type: "chat",
			DisplayName: displayName, Name: nativeName, Description: description,
			InputTokenLimit: inputLimit, OutputTokenLimit: outputLimit,
			ContextLength: inputLimit, MaxCompletionTokens: outputLimit,
			SupportedGenerationMethods: []string{"chat"},
			SupportedParameters:        []string{"messages", "stream", "tools", "tool_choice"},
			SupportedInputModalities:   modalities,
			SupportedOutputModalities:  []string{"text"}, UserDefined: true,
		})
	}
	sort.Slice(models, func(i, j int) bool { return models[i].ID < models[j].ID })
	return models
}

func appendUniqueString(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func modelsForAuth(raw []byte) ([]byte, error) {
	var req authModelRequest
	if errUnmarshal := json.Unmarshal(raw, &req); errUnmarshal != nil {
		return nil, errUnmarshal
	}
	if credentialTypeMarker(req.StorageJSON) == cline.TypeMarker {
		return cline.ModelsForAuth(raw)
	}
	cred, errCred := decodeCredential(req.StorageJSON)
	if errCred != nil {
		return okEnvelope(modelResponse{Provider: providerID, Models: staticModels()})
	}
	// The credential's content identity stays content-derived; the host record
	// ID can be file-based and must not leak into the stored JSON.
	if validCredentialID(cred.AuthID) == "" {
		cred.AuthID = credentialID(cred)
	}
	var authUpdate authData
	credentialUpdated := false
	if credentialNeedsRefresh(cred) {
		refreshed, errRefresh := refreshCredential(cred, req.HostCallbackID)
		if errRefresh == nil {
			cred = refreshed
			credentialUpdated = true
		}
	}
	if withProfile, discovered, errProfile := ensureProfileARN(cred, req.HostCallbackID); errProfile == nil {
		cred = withProfile
		credentialUpdated = credentialUpdated || discovered
	}
	if credentialUpdated {
		if updated, errAuth := authDataFromCredential(cred); errAuth == nil {
			// Empty ID/FileName lets the host keep the existing record identity.
			updated.ID = ""
			updated.FileName = ""
			authUpdate = updated
		}
	}
	models := discoverModels(cred, req.HostCallbackID)
	if len(models) == 0 {
		models = staticModels()
	}
	return okEnvelope(modelResponse{Provider: providerID, Models: models, AuthUpdate: authUpdate})
}

func discoverModels(cred credential, callbackID string) []modelInfo {
	if cred.AccessToken == "" {
		return nil
	}
	payload := map[string]any{"origin": "AI_EDITOR"}
	if cred.ProfileARN != "" {
		payload["profileArn"] = cred.ProfileARN
	}
	resp, errHTTP := callKiroService(
		cred,
		callbackID,
		loadedConfig().ModelDiscoveryURL,
		"AmazonCodeWhispererService.ListAvailableModels",
		payload,
	)
	if errHTTP != nil || resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil
	}
	var catalog kiroCatalogResponse
	if json.Unmarshal(resp.Body, &catalog) != nil {
		return nil
	}
	return catalogModelInfos(catalog.Models)
}

func modelExcluded(name string, excluded []string) bool {
	for _, item := range excluded {
		if strings.EqualFold(strings.TrimSpace(item), name) {
			return true
		}
	}
	return false
}
