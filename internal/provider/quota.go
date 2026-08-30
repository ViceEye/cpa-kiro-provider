package provider

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/ViceEye/cpa-kiro-provider/internal/cline"
	"github.com/ViceEye/cpa-kiro-provider/internal/jsonx"
)

type quotaAccount struct {
	AuthIndex        string                `json:"auth_index"`
	Name             string                `json:"name"`
	Label            string                `json:"label,omitempty"`
	Status           string                `json:"status"`
	Error            string                `json:"error,omitempty"`
	Subscription     string                `json:"subscription,omitempty"`
	SubscriptionType string                `json:"subscription_type,omitempty"`
	OverageStatus    string                `json:"overage_status,omitempty"`
	DaysUntilReset   int64                 `json:"days_until_reset,omitempty"`
	NextReset        string                `json:"next_reset,omitempty"`
	Usage            []quotaUsageBreakdown `json:"usage,omitempty"`
}

type quotaUsageBreakdown struct {
	ResourceType    string  `json:"resource_type"`
	DisplayName     string  `json:"display_name"`
	Unit            string  `json:"unit,omitempty"`
	Currency        string  `json:"currency,omitempty"`
	CurrentUsage    float64 `json:"current_usage"`
	UsageLimit      float64 `json:"usage_limit"`
	Remaining       float64 `json:"remaining"`
	UsagePercent    float64 `json:"usage_percent"`
	CurrentOverages float64 `json:"current_overages,omitempty"`
	OverageCap      float64 `json:"overage_cap,omitempty"`
	OverageRate     float64 `json:"overage_rate,omitempty"`
	OverageCharges  float64 `json:"overage_charges,omitempty"`
	NextReset       string  `json:"next_reset,omitempty"`
}

func registerManagement() ([]byte, error) {
	return okEnvelope(map[string]any{
		"Routes": []any{
			map[string]any{
				"Method":      http.MethodPost,
				"Path":        "plugins/kiro-provider/oauth/relogin/start",
				"Description": "Starts Kiro OAuth again and replaces an existing Kiro credential.",
			},
			map[string]any{
				"Method":      http.MethodGet,
				"Path":        "plugins/kiro-provider/oauth/relogin/status",
				"Description": "Polls a Kiro credential replacement login.",
			},
			map[string]any{
				"Method":      http.MethodGet,
				"Path":        "plugins/kiro-provider/quota",
				"Description": "Returns sanitized Kiro subscription and credit quota data for all Kiro auths.",
			},
			map[string]any{
				"Method":      http.MethodPost,
				"Path":        "plugins/kiro-provider/quotaRequest",
				"Description": "Refreshes sanitized Kiro subscription and credit quota data for all Kiro auths.",
			},
			map[string]any{
				"Method":      http.MethodGet,
				"Path":        "plugins/kiro-provider/credentials",
				"Description": "Returns sanitized CPA credential records and request statistics.",
			},
			map[string]any{"Method": http.MethodPost, "Path": "plugins/kiro-provider/console/oauth/start", "Description": "Starts provider OAuth from the Kiro Console."},
			map[string]any{"Method": http.MethodGet, "Path": "plugins/kiro-provider/console/oauth/status", "Description": "Polls provider OAuth from the Kiro Console."},
			map[string]any{"Method": http.MethodPost, "Path": "plugins/kiro-provider/console/oauth/status", "Description": "Submits a browser OAuth callback from the Kiro Console."},
			map[string]any{
				"Method":      http.MethodPost,
				"Path":        "plugins/kiro-provider/oauth/callback",
				"Description": "Accepts a Kiro browser callback and continues organization sign-in through AWS SSO OIDC.",
			},
		},
		"Resources": []any{
			map[string]any{
				"Path":        "console",
				"Menu":        "Kiro Console",
				"Description": "View Kiro credentials, request activity, quotas, and OAuth.",
			},
		},
	})
}

func handleManagement(raw []byte) ([]byte, error) {
	var req managementRequest
	if errUnmarshal := json.Unmarshal(raw, &req); errUnmarshal != nil {
		return nil, fmt.Errorf("decode Kiro management request: %w", errUnmarshal)
	}
	if isBrowserCallbackResourcePath(req.Path) {
		return handleBrowserCallbackResource(req)
	}
	if isConsoleResourcePath(req.Path) {
		return okEnvelope(managementResponse{
			StatusCode: http.StatusOK,
			Headers:    http.Header{"Content-Type": []string{"text/html; charset=utf-8"}, "Cache-Control": []string{"no-store"}},
			Body:       consolePanelHTML(),
		})
	}
	path := normalizeManagementPath(req.Path)
	if path == "/plugins/kiro-provider/oauth/relogin/start" {
		return handleReloginStart(req)
	}
	if path == "/plugins/kiro-provider/oauth/relogin/status" {
		return handleReloginStatus(req)
	}
	if path == "/plugins/kiro-provider/console/oauth/start" {
		return handleConsoleOAuthStart(req)
	}
	if path == "/plugins/kiro-provider/console/oauth/status" {
		return handleConsoleOAuthStatus(req)
	}
	if path == "/plugins/kiro-provider/oauth/callback" {
		return handleBrowserCallbackManagement(req)
	}
	if path == "/plugins/kiro-provider/credentials" {
		if !strings.EqualFold(req.Method, http.MethodGet) {
			return okEnvelope(managementResponse{StatusCode: http.StatusMethodNotAllowed, Headers: jsonHeaders(), Body: mustJSON(map[string]any{"error": "method_not_allowed"})})
		}
		return handleCredentialRecords()
	}
	refreshRequest := path == "/plugins/kiro-provider/quotaRequest"
	if refreshRequest {
		path = "/plugins/kiro-provider/quota"
	}
	if path != "/plugins/kiro-provider/quota" {
		return okEnvelope(managementResponse{
			StatusCode: http.StatusNotFound,
			Headers:    jsonHeaders(),
			Body:       mustJSON(map[string]any{"error": "not_found"}),
		})
	}
	if !strings.EqualFold(req.Method, http.MethodGet) && !(refreshRequest && strings.EqualFold(req.Method, http.MethodPost)) {
		return okEnvelope(managementResponse{
			StatusCode: http.StatusMethodNotAllowed,
			Headers:    jsonHeaders(),
			Body:       mustJSON(map[string]any{"error": "method_not_allowed"}),
		})
	}
	accounts, errQuota := loadKiroQuotas(req.HostCallbackID)
	if errQuota != nil {
		return okEnvelope(managementResponse{
			StatusCode: http.StatusBadGateway,
			Headers:    jsonHeaders(),
			Body:       mustJSON(map[string]any{"error": "quota_query_failed", "message": errQuota.Error()}),
		})
	}
	return okEnvelope(managementResponse{
		StatusCode: http.StatusOK,
		Headers:    jsonHeaders(),
		Body: mustJSON(map[string]any{
			"provider":     providerID,
			"generated_at": time.Now().UTC().Format(time.RFC3339),
			"accounts":     accounts,
		}),
	})
}

func handleCredentialRecords() ([]byte, error) {
	result, errList := callHostCall("host.auth.list", map[string]any{})
	if errList != nil {
		return okEnvelope(managementResponse{StatusCode: http.StatusBadGateway, Headers: jsonHeaders(), Body: mustJSON(map[string]any{"error": "credential_list_failed", "message": errList.Error()})})
	}
	var list hostAuthListResponse
	if errUnmarshal := json.Unmarshal(result, &list); errUnmarshal != nil {
		return okEnvelope(managementResponse{StatusCode: http.StatusBadGateway, Headers: jsonHeaders(), Body: mustJSON(map[string]any{"error": "credential_list_failed"})})
	}
	// host.auth.list carries no request stats, so merge the plugin's in-memory
	// counters keyed by each credential's stable auth_id.
	records := make([]map[string]any, 0, len(list.Files))
	for _, entry := range list.Files {
		record := map[string]any{
			"id": entry.ID, "auth_index": entry.AuthIndex, "name": entry.Name,
			"type": entry.Type, "provider": entry.Provider, "label": entry.Label,
			"disabled": entry.Disabled,
		}
		if authID := credentialAuthID(entry); authID != "" {
			if snap, ok := statFor(authID); ok {
				record["success"] = snap.Success
				record["failed"] = snap.Failure
				record["history"] = snap.History
				if snap.LastRequest != "" {
					record["last_request"] = snap.LastRequest
				}
			}
		}
		records = append(records, record)
	}
	return okEnvelope(managementResponse{StatusCode: http.StatusOK, Headers: jsonHeaders(), Body: mustJSON(map[string]any{
		"generated_at": time.Now().UTC().Format(time.RFC3339), "credentials": records,
	})})
}

// credentialAuthID resolves the stable kiro auth_id for a listed credential so
// in-memory request stats can be matched to it.
func credentialAuthID(entry hostAuthFileEntry) string {
	if entry.AuthIndex == "" {
		return ""
	}
	result, err := callHostCall("host.auth.get", map[string]any{"auth_index": entry.AuthIndex})
	if err != nil {
		return ""
	}
	var auth hostAuthGetResponse
	if json.Unmarshal(result, &auth) != nil {
		return ""
	}
	cred, errCred := decodeCredential(auth.JSON)
	if errCred != nil {
		return ""
	}
	if stable := validCredentialID(cred.AuthID); stable != "" {
		return stable
	}
	nameID := strings.TrimSuffix(strings.TrimSpace(auth.Name), ".json")
	if stable := validCredentialID(nameID); stable != "" {
		return stable
	}
	return credentialID(cred)
}

func isBrowserCallbackResourcePath(value string) bool {
	path := strings.TrimRight(strings.TrimSpace(value), "/")
	return path == "/v0/resource/plugins/kiro-provider/oauth" || path == "/v0/resource/plugins/kiro-provider/oauth/signin/callback"
}

func isConsoleResourcePath(value string) bool {
	path := strings.TrimRight(strings.TrimSpace(value), "/")
	return path == "/v0/resource/plugins/kiro-provider/console"
}

func normalizeManagementPath(value string) string {
	path := "/" + strings.Trim(strings.TrimSpace(value), "/")
	if strings.HasPrefix(path, "/v0/management/") {
		path = strings.TrimPrefix(path, "/v0/management")
	} else if path == "/v0/management" {
		path = "/"
	}
	return path
}

func loadKiroQuotas(callbackID string) ([]quotaAccount, error) {
	result, errList := callHostCall("host.auth.list", map[string]any{})
	if errList != nil {
		return nil, fmt.Errorf("list Kiro auths: %w", errList)
	}
	var list hostAuthListResponse
	if errUnmarshal := json.Unmarshal(result, &list); errUnmarshal != nil {
		return nil, fmt.Errorf("decode Kiro auth list: %w", errUnmarshal)
	}
	accounts := make([]quotaAccount, 0)
	for _, entry := range list.Files {
		if entry.Disabled || (!strings.EqualFold(entry.Provider, providerID) && !strings.EqualFold(entry.Type, providerID)) {
			continue
		}
		account := quotaAccount{AuthIndex: entry.AuthIndex, Name: entry.Name, Label: entry.Label, Status: "error"}
		if entry.AuthIndex == "" {
			account.Error = "Kiro auth has no runtime auth index"
			accounts = append(accounts, account)
			continue
		}
		// Cline credentials multiplex through this plugin — route their quota
		// to the cline protocol layer.
		if storageJSON, typeMarker := credentialStorageJSONByIndex(callbackID, entry.AuthIndex); typeMarker == cline.TypeMarker {
			raw := mustJSON(map[string]any{
				"Body":           mustJSON(map[string]any{"StorageJSON": json.RawMessage(storageJSON), "auth_index": entry.AuthIndex, "host_callback_id": callbackID}),
				"HostCallbackID": callbackID,
			})
			clineRaw, errUsage := cline.Usage(raw)
			if errUsage != nil {
				account.Error = errUsage.Error()
				accounts = append(accounts, account)
				continue
			}
			clineAccounts, errCline := parseClineUsageEnvelope(clineRaw)
			if errCline != nil {
				account.Error = errCline.Error()
			} else if len(clineAccounts) == 1 {
				account = clineAccounts[0]
				account.AuthIndex = entry.AuthIndex
				account.Name = entry.Name
			} else {
				account.Error = "cline quota returned no account"
			}
			accounts = append(accounts, account)
			continue
		}
		if errLoad := loadKiroQuotaAccount(&account, callbackID); errLoad != nil {
			account.Error = errLoad.Error()
		} else {
			account.Status = "ok"
		}
		accounts = append(accounts, account)
	}
	return accounts, nil
}

func loadKiroQuotaAccount(account *quotaAccount, callbackID string) error {
	result, errGet := callHostCall("host.auth.get", map[string]any{"auth_index": account.AuthIndex})
	if errGet != nil {
		return fmt.Errorf("read auth: %w", errGet)
	}
	var auth hostAuthGetResponse
	if errUnmarshal := json.Unmarshal(result, &auth); errUnmarshal != nil {
		return fmt.Errorf("decode auth: %w", errUnmarshal)
	}
	cred, errCredential := decodeCredential(auth.JSON)
	if errCredential != nil {
		return errCredential
	}
	if cred.AuthID == "" {
		nameID := strings.TrimSuffix(strings.TrimSpace(auth.Name), ".json")
		if stableID := validCredentialID(nameID); stableID != "" {
			cred.AuthID = stableID
		} else {
			cred.AuthID = credentialID(cred)
		}
	}
	if credentialNeedsRefresh(cred) {
		refreshed, errRefresh := refreshCredential(cred, callbackID)
		if errRefresh != nil {
			return errRefresh
		}
		cred = refreshed
		persistCredentialByNameBestEffort(auth.Name, cred)
	}
	withProfile, discovered, errProfile := ensureProfileARN(cred, callbackID)
	if errProfile != nil {
		return errProfile
	}
	cred = withProfile
	if discovered {
		persistCredentialByNameBestEffort(auth.Name, cred)
	}
	resp, errHTTP := callKiroUsageLimits(cred, callbackID, loadedConfig().UsageURL)
	if errHTTP != nil {
		return fmt.Errorf("query Kiro quota: %w", errHTTP)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return upstreamStatusError("Kiro GetUsageLimits failed", resp.StatusCode, resp.Body)
	}
	var usage kiroUsageLimitsResponse
	if errUnmarshal := json.Unmarshal(resp.Body, &usage); errUnmarshal != nil {
		return fmt.Errorf("decode Kiro quota: %w", errUnmarshal)
	}
	account.Subscription = usage.SubscriptionInfo.SubscriptionTitle
	account.SubscriptionType = usage.SubscriptionInfo.Type
	account.OverageStatus = usage.OverageConfiguration.OverageStatus
	account.DaysUntilReset = usage.DaysUntilReset
	account.NextReset = unixFloatTime(usage.NextDateReset)
	for _, item := range usage.UsageBreakdownList {
		remaining := item.UsageLimit - item.CurrentUsage
		if remaining < 0 {
			remaining = 0
		}
		percent := float64(0)
		if item.UsageLimit > 0 {
			percent = item.CurrentUsage / item.UsageLimit * 100
		}
		account.Usage = append(account.Usage, quotaUsageBreakdown{
			ResourceType: item.ResourceType,
			DisplayName:  jsonx.NonEmpty(item.DisplayName, item.DisplayNamePlural),
			Unit:         item.Unit, Currency: item.Currency,
			CurrentUsage: item.CurrentUsage, UsageLimit: item.UsageLimit,
			Remaining: remaining, UsagePercent: percent,
			CurrentOverages: item.CurrentOverages, OverageCap: item.OverageCap,
			OverageRate: item.OverageRate, OverageCharges: item.OverageCharges,
			NextReset: unixFloatTime(item.NextDateReset),
		})
	}
	return nil
}

func callKiroUsageLimits(cred credential, callbackID, configuredURL string) (hostHTTPResponse, error) {
	endpoint, errEndpoint := kiroUsageEndpoint(configuredURL, cred.APIRegion, cred.ProfileARN)
	if errEndpoint != nil {
		return hostHTTPResponse{}, errEndpoint
	}
	clientID := "KiroIDE-0.7.45-" + jsonx.NonEmpty(strings.TrimSpace(cred.Fingerprint), "unknown")
	return hostHTTPDoCall(hostHTTPRequest{
		HostCallbackID: callbackID,
		Method:         http.MethodGet,
		URL:            endpoint,
		Headers: http.Header{
			"Authorization":               []string{"Bearer " + cred.AccessToken},
			"User-Agent":                  []string{"aws-sdk-js/1.0.0 ua/2.1 os/linux lang/js md/nodejs api/codewhispererruntime#1.0.0 m/N,E " + clientID},
			"X-Amz-User-Agent":            []string{"aws-sdk-js/1.0.0 " + clientID},
			"X-Amzn-Codewhisperer-Optout": []string{"true"},
			"Amz-Sdk-Invocation-Id":       []string{randomID()},
			"Amz-Sdk-Request":             []string{"attempt=1; max=1"},
			"Connection":                  []string{"close"},
		},
	})
}

func kiroUsageEndpoint(configuredURL, region, profileARN string) (string, error) {
	region = jsonx.NonEmpty(strings.ToLower(strings.TrimSpace(region)), defaultRegion)
	fallback := "https://q." + region + ".amazonaws.com/getUsageLimits"
	endpoint := configuredRegionURL(configuredURL, fallback, region)
	parsed, errParse := url.Parse(strings.TrimSpace(endpoint))
	if errParse != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("invalid Kiro usage endpoint")
	}
	if parsed.Path == "" || parsed.Path == "/" {
		parsed.Path = "/getUsageLimits"
	}
	query := parsed.Query()
	query.Set("origin", "AI_EDITOR")
	query.Set("resourceType", "AGENTIC_REQUEST")
	if profileARN = strings.TrimSpace(profileARN); profileARN != "" {
		query.Set("profileArn", profileARN)
	} else {
		query.Del("profileArn")
	}
	parsed.RawQuery = query.Encode()
	return parsed.String(), nil
}

func persistCredentialByNameBestEffort(name string, cred credential) {
	name = strings.TrimSpace(name)
	if name == "" {
		return
	}
	if !strings.HasSuffix(strings.ToLower(name), ".json") {
		name += ".json"
	}
	raw, errMarshal := json.Marshal(cred)
	if errMarshal != nil {
		return
	}
	_, _ = callHostCall("host.auth.save", map[string]any{"name": name, "json": json.RawMessage(raw)})
}

func unixFloatTime(value float64) string {
	if value <= 0 {
		return ""
	}
	seconds := int64(value)
	nanos := int64((value - float64(seconds)) * float64(time.Second))
	return time.Unix(seconds, nanos).UTC().Format(time.RFC3339)
}

func jsonHeaders() http.Header {
	return http.Header{
		"Content-Type":  []string{"application/json; charset=utf-8"},
		"Cache-Control": []string{"no-store"},
	}
}

func mustJSON(value any) []byte {
	raw, _ := json.Marshal(value)
	return raw
}


// credentialStorageJSONByIndex fetches the raw auth-file JSON for an
// auth_index and returns it together with the credential type marker.
func credentialStorageJSONByIndex(callbackID, authIndex string) (string, string) {
	result, err := callHostCall("host.auth.get", map[string]any{"auth_index": authIndex})
	if err != nil {
		return "", ""
	}
	var auth hostAuthGetResponse
	if err := json.Unmarshal(result, &auth); err != nil {
		return "", ""
	}
	return string(auth.JSON), credentialTypeMarker(auth.JSON)
}

// parseClineUsageEnvelope unwraps the cline.Usage management envelope into
// quota accounts.
func parseClineUsageEnvelope(raw []byte) ([]quotaAccount, error) {
	var env struct {
		OK     bool `json:"ok"`
		Result struct {
			Body json.RawMessage `json:"Body"`
		} `json:"result"`
	}
	if err := json.Unmarshal(raw, &env); err != nil || !env.OK {
		return nil, fmt.Errorf("cline quota envelope invalid")
	}
	var bodyStr string
	if err := json.Unmarshal(env.Result.Body, &bodyStr); err != nil {
		return nil, fmt.Errorf("cline quota body not a string")
	}
	var parsed struct {
		Accounts []quotaAccount `json:"accounts"`
	}
	if err := json.Unmarshal([]byte(bodyStr), &parsed); err != nil {
		return nil, fmt.Errorf("decode cline quota accounts: %w", err)
	}
	return parsed.Accounts, nil
}
