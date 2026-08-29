package cline

import (
	"net/http"
	"time"
)

const (
	providerID = "cline"

	apiBase     = "https://api.cline.bot"
	chatPath    = "/api/v1/chat/completions"
	modelsPath  = "/api/v1/models"
	refreshPath = "/api/v1/auth/refresh"
	mePath      = "/api/v1/users/me"
	balanceFmt  = "/api/v1/users/%s/balance"
	modelPrefix = "cline/"
)

// clineIdentityHeaders mirrors the identity headers the Cline client sends.
var clineIdentityHeaders = map[string]string{
	"HTTP-Referer":  "https://cline.bot",
	"X-Title":       "Cline",
	"X-CLIENT-TYPE": "cline-provider",
}

// statusErr builds a statusError.
func statusErr(code, message string, retryable bool, status int) statusError {
	return statusError{Code: code, Message: message, Retryable: retryable, HTTPStatus: status}
}

// credential is the plugin's stored credential JSON (StorageJSON).
type credential struct {
	Type          string `json:"type"`
	Version       int    `json:"version"`
	AuthID        string `json:"auth_id,omitempty"`
	Email         string `json:"email,omitempty"`
	AccessToken   string `json:"access_token"`
	RefreshToken  string `json:"refresh_token"`
	ExpiresAt     string `json:"expires_at,omitempty"`
	LastRefreshAt string `json:"last_refresh_at,omitempty"`
}

// clineTokenResponse is the /auth/refresh payload subset.
type clineTokenResponse struct {
	Data struct {
		AccessToken  string `json:"accessToken"`
		RefreshToken string `json:"refreshToken"`
		ExpiresAt    string `json:"expiresAt"`
		ExpiresIn    int    `json:"expiresIn"`
	} `json:"data"`
}

// clineUser is the /users/me payload subset.
type clineUser struct {
	Data struct {
		ID          string `json:"id"`
		Email       string `json:"email"`
		DisplayName string `json:"displayName"`
	} `json:"data"`
}

// clineBalance is the /users/{uid}/balance payload subset.
type clineBalance struct {
	Data struct {
		Balance float64 `json:"balance"`
	} `json:"data"`
}

// authData mirrors pluginapi.AuthData (host connection record).
type authData struct {
	Provider         string            `json:"Provider"`
	ID               string            `json:"ID"`
	FileName         string            `json:"FileName"`
	Label            string            `json:"Label"`
	StorageJSON      []byte            `json:"StorageJSON"`
	Metadata         map[string]any    `json:"Metadata,omitempty"`
	Attributes       map[string]string `json:"Attributes,omitempty"`
	NextRefreshAfter time.Time         `json:"NextRefreshAfter,omitempty"`
}

// modelInfo mirrors pluginapi.ModelInfo.
type modelInfo struct {
	ID          string `json:"ID"`
	Object      string `json:"Object"`
	OwnedBy     string `json:"OwnedBy"`
	DisplayName string `json:"DisplayName"`
	Type        string `json:"Type,omitempty"`
}

// statusError carries an HTTP status so CPA can drive cooldown/retry.
type statusError struct {
	Code       string
	Message    string
	Retryable  bool
	HTTPStatus int
}

func (e statusError) Error() string { return e.Message }

type hostConfigSummary struct {
	AuthDir string `json:"AuthDir"`
}

type authParseRequest struct {
	Provider string            `json:"Provider"`
	Path     string            `json:"Path"`
	FileName string            `json:"FileName"`
	RawJSON  []byte            `json:"RawJSON"`
	Host     hostConfigSummary `json:"Host"`
}

type authParseResponse struct {
	Handled bool       `json:"Handled"`
	Auth    authData   `json:"Auth,omitempty"`
	Auths   []authData `json:"Auths,omitempty"`
}

type authRefreshRequest struct {
	AuthID         string            `json:"AuthID"`
	AuthProvider   string            `json:"AuthProvider"`
	StorageJSON    []byte            `json:"StorageJSON"`
	Metadata       map[string]any    `json:"Metadata"`
	Attributes     map[string]string `json:"Attributes"`
	Host           hostConfigSummary `json:"Host"`
	HostCallbackID string            `json:"host_callback_id,omitempty"`
}

type authRefreshResponse struct {
	Auth             authData  `json:"Auth"`
	NextRefreshAfter time.Time `json:"NextRefreshAfter"`
}

type authModelRequest struct {
	AuthID         string            `json:"AuthID"`
	AuthProvider   string            `json:"AuthProvider"`
	StorageJSON    []byte            `json:"StorageJSON"`
	Metadata       map[string]any    `json:"Metadata"`
	Attributes     map[string]string `json:"Attributes"`
	Host           hostConfigSummary `json:"Host"`
	HostCallbackID string            `json:"host_callback_id,omitempty"`
}

type modelResponse struct {
	Provider string      `json:"Provider"`
	Models   []modelInfo `json:"Models"`
}

type executorRequest struct {
	AuthID          string              `json:"AuthID"`
	AuthProvider    string              `json:"AuthProvider"`
	Model           string              `json:"Model"`
	Format          string              `json:"Format"`
	Stream          bool                `json:"Stream"`
	Headers         http.Header         `json:"Headers"`
	SourceFormat    string              `json:"SourceFormat"`
	Payload         []byte              `json:"Payload"`
	StorageJSON     []byte              `json:"StorageJSON"`
	StreamID        string              `json:"stream_id,omitempty"`
	HostCallbackID  string              `json:"host_callback_id,omitempty"`
}

type executorResponse struct {
	Payload  []byte         `json:"Payload"`
	Headers  http.Header    `json:"Headers,omitempty"`
	Metadata map[string]any `json:"Metadata,omitempty"`
}

type managementRequest struct {
	Method         string              `json:"Method"`
	Path           string              `json:"Path"`
	Headers        http.Header         `json:"Headers"`
	Query          map[string][]string `json:"Query"`
	Body           []byte              `json:"Body"`
	HostCallbackID string              `json:"host_callback_id,omitempty"`
}

type managementResponse struct {
	StatusCode int         `json:"StatusCode"`
	Headers    http.Header `json:"Headers,omitempty"`
	Body       []byte      `json:"Body,omitempty"`
}

// freeModels is the curated free-tier catalog (verified available on the
// Cline connection). Live discovery (GET /models, ~396 entries) exists but
// the free tier only serves a subset.
var freeModels = []modelInfo{
	{ID: "z-ai/glm-5.3-flash", Object: "model", OwnedBy: "z-ai", DisplayName: "GLM 5.3 Flash (Free)"},
	{ID: "anthropic/claude-opus-4.7", Object: "model", OwnedBy: "anthropic", DisplayName: "Claude Opus 4.7"},
	{ID: "anthropic/claude-sonnet-4.6", Object: "model", OwnedBy: "anthropic", DisplayName: "Claude Sonnet 4.6"},
	{ID: "anthropic/claude-opus-4.6", Object: "model", OwnedBy: "anthropic", DisplayName: "Claude Opus 4.6"},
	{ID: "openai/gpt-5.3-codex", Object: "model", OwnedBy: "openai", DisplayName: "GPT-5.3 Codex"},
	{ID: "openai/gpt-5.4", Object: "model", OwnedBy: "openai", DisplayName: "GPT-5.4"},
	{ID: "google/gemini-3.1-pro-preview", Object: "model", OwnedBy: "google", DisplayName: "Gemini 3.1 Pro Preview"},
	{ID: "google/gemini-3.1-flash-lite-preview", Object: "model", OwnedBy: "google", DisplayName: "Gemini 3.1 Flash Lite Preview"},
	{ID: "kwaipilot/kat-coder-pro", Object: "model", OwnedBy: "kwaipilot", DisplayName: "KAT Coder Pro"},
}
