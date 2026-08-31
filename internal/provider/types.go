package provider

import (
	"encoding/json"
	"net/http"
	"time"
)

const (
	providerID    = "nexus"
	pluginName    = "cpa-provider-nexus"
	pluginVersion = "0.9.0"
	defaultRegion = "us-east-1"
	nexusLogoPath = "/v0/resource/plugins/" + pluginName + "/icon.svg"

	defaultSSOStartURL    = "https://view.awsapps.com/start"
	defaultLoginMode      = "kiro-browser"
	defaultSignInURL      = "https://app.kiro.dev/signin"
	defaultRedirectURI    = "http://localhost:3128"
	defaultIDCRedirectURI = "http://localhost:3128/signin/callback"
	defaultTokenURL       = "https://prod.us-east-1.auth.desktop.kiro.dev/oauth/token"
	deviceGrantType       = "urn:ietf:params:oauth:grant-type:device_code"
	oidcClientName        = "Amazon Q Developer for command line"
	oidcClientType        = "public"
)

var defaultOIDCScopes = []string{
	"codewhisperer:completions",
	"codewhisperer:analysis",
	"codewhisperer:conversations",
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

type hostConfigSummary struct {
	AuthDir          string                  `json:"AuthDir"`
	ProxyURL         string                  `json:"ProxyURL"`
	ForceModelPrefix bool                    `json:"ForceModelPrefix"`
	OAuthModelAlias  map[string][]modelAlias `json:"OAuthModelAlias"`
	ExcludedModels   map[string][]string     `json:"ExcludedModels"`
}

type modelAlias struct {
	Name  string `json:"Name"`
	Alias string `json:"Alias"`
}

type authData struct {
	Provider         string            `json:"Provider"`
	ID               string            `json:"ID"`
	FileName         string            `json:"FileName"`
	Label            string            `json:"Label"`
	Prefix           string            `json:"Prefix,omitempty"`
	ProxyURL         string            `json:"ProxyURL,omitempty"`
	Disabled         bool              `json:"Disabled,omitempty"`
	StorageJSON      []byte            `json:"StorageJSON"`
	Metadata         map[string]any    `json:"Metadata,omitempty"`
	Attributes       map[string]string `json:"Attributes,omitempty"`
	NextRefreshAfter time.Time         `json:"NextRefreshAfter,omitempty"`
}

type credential struct {
	Type           string   `json:"type,omitempty"`
	Kind           string   `json:"kind,omitempty"`
	Version        int      `json:"version"`
	AuthID         string   `json:"auth_id,omitempty"`
	AuthType       string   `json:"auth_type"`
	Mode           string   `json:"mode"`
	SourcePath     string   `json:"source_path,omitempty"`
	SourceKind     string   `json:"source_kind,omitempty"`
	SourceTokenKey string   `json:"source_token_key,omitempty"`
	AccessToken    string   `json:"access_token,omitempty"`
	RefreshToken   string   `json:"refresh_token"`
	ClientID       string   `json:"client_id,omitempty"`
	ClientSecret   string   `json:"client_secret,omitempty"`
	ClientIDHash   string   `json:"client_id_hash,omitempty"`
	ProfileARN     string   `json:"profile_arn,omitempty"`
	SSORegion      string   `json:"sso_region,omitempty"`
	APIRegion      string   `json:"api_region,omitempty"`
	ExpiresAt      string   `json:"expires_at,omitempty"`
	Scopes         []string `json:"scopes,omitempty"`
	Label          string   `json:"label,omitempty"`
	Fingerprint    string   `json:"fingerprint,omitempty"`
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

type authLoginStartRequest struct {
	Provider       string            `json:"Provider"`
	BaseURL        string            `json:"BaseURL"`
	Host           hostConfigSummary `json:"Host"`
	Metadata       map[string]any    `json:"Metadata"`
	HostCallbackID string            `json:"host_callback_id,omitempty"`
}

type authLoginStartResponse struct {
	Provider  string         `json:"Provider"`
	URL       string         `json:"URL"`
	State     string         `json:"State"`
	ExpiresAt time.Time      `json:"ExpiresAt"`
	Metadata  map[string]any `json:"Metadata,omitempty"`
}

type authLoginPollRequest struct {
	Provider       string            `json:"Provider"`
	State          string            `json:"State"`
	Host           hostConfigSummary `json:"Host"`
	Metadata       map[string]any    `json:"Metadata"`
	HostCallbackID string            `json:"host_callback_id,omitempty"`
}

type authLoginPollResponse struct {
	Status  string     `json:"Status"`
	Message string     `json:"Message,omitempty"`
	Auth    authData   `json:"Auth,omitempty"`
	Auths   []authData `json:"Auths,omitempty"`
}

type deviceLoginState struct {
	Version               int      `json:"version"`
	LoginMode             string   `json:"login_mode,omitempty"`
	State                 string   `json:"state"`
	ClientID              string   `json:"client_id"`
	ClientSecret          string   `json:"client_secret"`
	ClientSecretExpiresAt int64    `json:"client_secret_expires_at,omitempty"`
	DeviceCode            string   `json:"device_code"`
	UserCode              string   `json:"user_code,omitempty"`
	Region                string   `json:"region"`
	APIRegion             string   `json:"api_region,omitempty"`
	StartURL              string   `json:"start_url"`
	ExpiresAt             string   `json:"expires_at"`
	Interval              int      `json:"interval"`
	Scopes                []string `json:"scopes"`
}

type browserLoginState struct {
	Version      int    `json:"version"`
	LoginMode    string `json:"login_mode"`
	State        string `json:"state"`
	CodeVerifier string `json:"code_verifier"`
	RedirectURI  string `json:"redirect_uri"`
	TokenURL     string `json:"token_url"`
	APIRegion    string `json:"api_region"`
	ExpiresAt    string `json:"expires_at"`
}

type idcBrowserLoginState struct {
	Version      int      `json:"version"`
	LoginMode    string   `json:"login_mode"`
	State        string   `json:"state"`
	CodeVerifier string   `json:"code_verifier"`
	RedirectURI  string   `json:"redirect_uri"`
	ClientID     string   `json:"client_id"`
	ClientSecret string   `json:"client_secret"`
	Region       string   `json:"region"`
	APIRegion    string   `json:"api_region,omitempty"`
	StartURL     string   `json:"start_url"`
	ExpiresAt    string   `json:"expires_at"`
	Scopes       []string `json:"scopes"`
}

type oauthCallbackPayload struct {
	Code  string `json:"code"`
	State string `json:"state"`
	Error string `json:"error"`
}

type modelInfo struct {
	ID                         string   `json:"ID"`
	Object                     string   `json:"Object"`
	Created                    int64    `json:"Created,omitempty"`
	OwnedBy                    string   `json:"OwnedBy"`
	Type                       string   `json:"Type,omitempty"`
	DisplayName                string   `json:"DisplayName"`
	Name                       string   `json:"Name,omitempty"`
	Description                string   `json:"Description,omitempty"`
	InputTokenLimit            int64    `json:"InputTokenLimit,omitempty"`
	OutputTokenLimit           int64    `json:"OutputTokenLimit,omitempty"`
	SupportedGenerationMethods []string `json:"SupportedGenerationMethods"`
	ContextLength              int64    `json:"ContextLength,omitempty"`
	MaxCompletionTokens        int64    `json:"MaxCompletionTokens,omitempty"`
	SupportedParameters        []string `json:"SupportedParameters,omitempty"`
	SupportedInputModalities   []string `json:"SupportedInputModalities,omitempty"`
	SupportedOutputModalities  []string `json:"SupportedOutputModalities,omitempty"`
	UserDefined                bool     `json:"UserDefined"`
}

type kiroCatalogModel struct {
	ModelID             string   `json:"modelId"`
	ModelName           string   `json:"modelName"`
	Description         string   `json:"description"`
	RateMultiplier      float64  `json:"rateMultiplier"`
	RateUnit            string   `json:"rateUnit"`
	SupportedInputTypes []string `json:"supportedInputTypes"`
	TokenLimits         struct {
		MaxInputTokens  int64 `json:"maxInputTokens"`
		MaxOutputTokens int64 `json:"maxOutputTokens"`
	} `json:"tokenLimits"`
}

type kiroCatalogResponse struct {
	Models       []kiroCatalogModel `json:"models"`
	DefaultModel *kiroCatalogModel  `json:"defaultModel,omitempty"`
}

type kiroUsageBreakdown struct {
	ResourceType      string  `json:"resourceType"`
	DisplayName       string  `json:"displayName"`
	DisplayNamePlural string  `json:"displayNamePlural"`
	Unit              string  `json:"unit"`
	Currency          string  `json:"currency"`
	CurrentUsage      float64 `json:"currentUsageWithPrecision"`
	UsageLimit        float64 `json:"usageLimitWithPrecision"`
	CurrentOverages   float64 `json:"currentOveragesWithPrecision"`
	OverageCap        float64 `json:"overageCapWithPrecision"`
	OverageRate       float64 `json:"overageRate"`
	OverageCharges    float64 `json:"overageCharges"`
	NextDateReset     float64 `json:"nextDateReset"`
}

type kiroUsageLimitsResponse struct {
	DaysUntilReset   int64   `json:"daysUntilReset"`
	NextDateReset    float64 `json:"nextDateReset"`
	SubscriptionInfo struct {
		SubscriptionTitle string `json:"subscriptionTitle"`
		Type              string `json:"type"`
		OverageCapability string `json:"overageCapability"`
		UpgradeCapability string `json:"upgradeCapability"`
	} `json:"subscriptionInfo"`
	OverageConfiguration struct {
		OverageStatus string `json:"overageStatus"`
	} `json:"overageConfiguration"`
	UsageBreakdownList []kiroUsageBreakdown `json:"usageBreakdownList"`
}

type modelResponse struct {
	Provider   string      `json:"Provider"`
	Models     []modelInfo `json:"Models"`
	AuthUpdate authData    `json:"AuthUpdate,omitempty"`
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

type executorRequest struct {
	AuthID          string              `json:"AuthID"`
	AuthProvider    string              `json:"AuthProvider"`
	Model           string              `json:"Model"`
	Format          string              `json:"Format"`
	Stream          bool                `json:"Stream"`
	Alt             string              `json:"Alt"`
	Headers         http.Header         `json:"Headers"`
	Query           map[string][]string `json:"Query"`
	OriginalRequest []byte              `json:"OriginalRequest"`
	SourceFormat    string              `json:"SourceFormat"`
	Payload         []byte              `json:"Payload"`
	Metadata        map[string]any      `json:"Metadata"`
	StorageJSON     []byte              `json:"StorageJSON"`
	AuthMetadata    map[string]any      `json:"AuthMetadata"`
	AuthAttributes  map[string]string   `json:"AuthAttributes"`
	StreamID        string              `json:"stream_id,omitempty"`
	HostCallbackID  string              `json:"host_callback_id,omitempty"`
}

type executorResponse struct {
	Payload  []byte         `json:"Payload"`
	Headers  http.Header    `json:"Headers,omitempty"`
	Metadata map[string]any `json:"Metadata,omitempty"`
}

type executorHTTPResponse struct {
	StatusCode int         `json:"StatusCode"`
	Headers    http.Header `json:"Headers"`
	Body       []byte      `json:"Body"`
}

type commandLineFlagValue struct {
	Name  string `json:"Name"`
	Type  string `json:"Type"`
	Value string `json:"Value"`
	Set   bool   `json:"Set"`
}

type commandLineExecutionRequest struct {
	Program        string                          `json:"Program"`
	Args           []string                        `json:"Args"`
	ConfigPath     string                          `json:"ConfigPath"`
	Host           hostConfigSummary               `json:"Host"`
	Flags          map[string]commandLineFlagValue `json:"Flags"`
	TriggeredFlags map[string]commandLineFlagValue `json:"TriggeredFlags"`
}

type commandLineExecutionResponse struct {
	Stdout   []byte     `json:"Stdout,omitempty"`
	Stderr   []byte     `json:"Stderr,omitempty"`
	Auths    []authData `json:"Auths,omitempty"`
	ExitCode int        `json:"ExitCode"`
}

type hostHTTPRequest struct {
	HostCallbackID string      `json:"host_callback_id,omitempty"`
	Method         string      `json:"method"`
	URL            string      `json:"url"`
	Headers        http.Header `json:"headers,omitempty"`
	Body           []byte      `json:"body,omitempty"`
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

type managementRequest struct {
	Method         string              `json:"Method"`
	Path           string              `json:"Path"`
	Headers        http.Header         `json:"Headers"`
	Query          map[string][]string `json:"Query"`
	Body           []byte              `json:"Body"`
	HostCallbackID string              `json:"host_callback_id,omitempty"`
}

type reloginRequest struct {
	AuthIndex string `json:"auth_index"`
}

type reloginStatusRequest struct {
	State string `json:"state"`
}

type managementResponse struct {
	StatusCode int         `json:"StatusCode"`
	Headers    http.Header `json:"Headers,omitempty"`
	Body       []byte      `json:"Body,omitempty"`
}

type hostAuthFileEntry struct {
	ID        string `json:"id,omitempty"`
	AuthIndex string `json:"auth_index,omitempty"`
	Name      string `json:"name"`
	Type      string `json:"type,omitempty"`
	Provider  string `json:"provider,omitempty"`
	Label     string `json:"label,omitempty"`
	Disabled  bool   `json:"disabled,omitempty"`
}

type hostAuthListResponse struct {
	Files []hostAuthFileEntry `json:"files"`
}

type hostAuthGetResponse struct {
	AuthIndex string          `json:"auth_index"`
	Name      string          `json:"name,omitempty"`
	Path      string          `json:"path,omitempty"`
	JSON      json.RawMessage `json:"json"`
}

type pluginConfig struct {
	ImportMode         string   `json:"import_mode"`
	LoginMode          string   `json:"login_mode"`
	StaticModels       []string `json:"static_models"`
	APIRegion          string   `json:"api_region"`
	SSORegion          string   `json:"sso_region"`
	ModelPrefix        string   `json:"model_prefix"`
	Fingerprint        string   `json:"fingerprint"`
	RuntimeBaseURL     string   `json:"runtime_base_url"`
	ModelDiscoveryURL  string   `json:"model_discovery_url"`
	UsageURL           string   `json:"usage_url"`
	DesktopRefreshURL  string   `json:"desktop_refresh_url"`
	OIDCRefreshURL     string   `json:"oidc_refresh_url"`
	SSOStartURL        string   `json:"sso_start_url"`
	BrowserSignInURL   string   `json:"browser_signin_url"`
	BrowserRedirectURI string   `json:"browser_redirect_uri"`
	DesktopTokenURL    string   `json:"desktop_token_url"`
}
