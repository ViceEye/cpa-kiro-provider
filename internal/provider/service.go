package provider

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/ViceEye/cpa-kiro-provider/internal/jsonx"
)

func kiroServiceEndpoint(configured, region string) string {
	region = jsonx.NonEmpty(region, defaultRegion)
	fallback := "https://q." + region + ".amazonaws.com/"
	endpoint := configuredRegionURL(configured, fallback, region)
	if !strings.HasSuffix(endpoint, "/") {
		endpoint += "/"
	}
	return endpoint
}

func kiroServiceHeaders(cred credential, target string) http.Header {
	return http.Header{
		"Authorization":               []string{"Bearer " + cred.AccessToken},
		"Content-Type":                []string{"application/x-amz-json-1.0"},
		"X-Amz-Target":                []string{target},
		"User-Agent":                  []string{"Kiro-CLI"},
		"X-Amzn-Codewhisperer-Optout": []string{"true"},
		"Amz-Sdk-Invocation-Id":       []string{randomID()},
		"Amz-Sdk-Request":             []string{"attempt=1; max=3"},
	}
}

func callKiroService(cred credential, callbackID, configuredURL, target string, payload any) (hostHTTPResponse, error) {
	body, errMarshal := json.Marshal(payload)
	if errMarshal != nil {
		return hostHTTPResponse{}, fmt.Errorf("encode Kiro service request: %w", errMarshal)
	}
	return hostHTTPDoCall(hostHTTPRequest{
		HostCallbackID: callbackID,
		Method:         http.MethodPost,
		URL:            kiroServiceEndpoint(configuredURL, cred.APIRegion),
		Headers:        kiroServiceHeaders(cred, target),
		Body:           body,
	})
}

func ensureProfileARN(cred credential, callbackID string) (credential, bool, error) {
	if strings.TrimSpace(cred.ProfileARN) != "" {
		return cred, false, nil
	}
	if strings.TrimSpace(cred.AccessToken) == "" {
		return cred, false, statusError{
			Code: "missing_access_token", Message: "Kiro profile discovery requires an access token",
			HTTPStatus: http.StatusUnauthorized,
		}
	}
	resp, errHTTP := callKiroService(
		cred,
		callbackID,
		loadedConfig().ModelDiscoveryURL,
		"AmazonCodeWhispererService.ListAvailableProfiles",
		map[string]any{},
	)
	if errHTTP != nil {
		return cred, false, statusError{
			Code: "profile_discovery_failed", Message: "Kiro profile discovery request failed",
			Retryable: true, HTTPStatus: http.StatusBadGateway, Cause: errHTTP,
		}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return cred, false, upstreamStatusError("Kiro ListAvailableProfiles failed", resp.StatusCode, resp.Body)
	}
	var result struct {
		Profiles []map[string]any `json:"profiles"`
	}
	if errDecode := json.Unmarshal(resp.Body, &result); errDecode != nil {
		return cred, false, statusError{
			Code: "invalid_profile_response", Message: "Kiro profile discovery returned invalid JSON",
			Retryable: true, HTTPStatus: http.StatusBadGateway, Cause: errDecode,
		}
	}
	for _, profile := range result.Profiles {
		if arn := jsonx.String(profile, "arn", "profileArn", "profile_arn"); arn != "" {
			cred.ProfileARN = arn
			finalizeCredential(&cred)
			return cred, true, nil
		}
	}
	return cred, false, statusError{
		Code: "profile_not_found", Message: "Kiro profile discovery returned no available profile",
		HTTPStatus: http.StatusBadGateway,
	}
}
