package main

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

var sqliteTokenKeys = []string{
	"kirocli:social:token",
	"kirocli:odic:token",
	"codewhisperer:odic:token",
}

var sqliteRegistrationKeys = []string{
	"kirocli:odic:device-registration",
	"codewhisperer:odic:device-registration",
}

func importCredentials(path, mode string) ([]credential, error) {
	mode = normalizeMode(mode)
	info, errStat := os.Stat(path)
	if errStat != nil {
		return nil, fmt.Errorf("inspect Kiro credential source: %w", errStat)
	}
	if !info.IsDir() {
		creds, errLoad := importCredentialFile(path, mode)
		if errLoad != nil {
			return nil, errLoad
		}
		return deduplicateCredentials(creds), nil
	}

	var files []string
	errWalk := filepath.WalkDir(path, func(candidate string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(candidate))
		if ext == ".json" || ext == ".sqlite" || ext == ".sqlite3" || ext == ".db" {
			files = append(files, candidate)
		}
		return nil
	})
	if errWalk != nil {
		return nil, fmt.Errorf("scan Kiro credential directory: %w", errWalk)
	}
	sort.Strings(files)

	registrations := make(map[string]map[string]any)
	for _, candidate := range files {
		if strings.ToLower(filepath.Ext(candidate)) != ".json" {
			continue
		}
		raw, errRead := os.ReadFile(candidate)
		if errRead != nil {
			continue
		}
		var object map[string]any
		if json.Unmarshal(raw, &object) == nil && stringValue(object, "clientId") != "" && stringValue(object, "clientSecret") != "" {
			registrations[strings.TrimSuffix(filepath.Base(candidate), filepath.Ext(candidate))] = object
		}
	}

	var out []credential
	var importErrors []error
	for _, candidate := range files {
		creds, errLoad := importCredentialFile(candidate, mode)
		if errLoad != nil {
			if !errors.Is(errLoad, errUnrecognizedCredential) {
				importErrors = append(importErrors, errLoad)
			}
			continue
		}
		for index := range creds {
			if creds[index].ClientIDHash != "" && (creds[index].ClientID == "" || creds[index].ClientSecret == "") {
				if reg := registrations[creds[index].ClientIDHash]; reg != nil {
					creds[index].ClientID = stringValue(reg, "clientId", "client_id")
					creds[index].ClientSecret = stringValue(reg, "clientSecret", "client_secret")
				}
			}
		}
		out = append(out, creds...)
	}
	out = deduplicateCredentials(out)
	if len(out) == 0 {
		if len(importErrors) > 0 {
			return nil, fmt.Errorf("no usable Kiro credentials found: %v", importErrors[0])
		}
		return nil, fmt.Errorf("no usable Kiro credentials found in %s", path)
	}
	return out, nil
}

var errUnrecognizedCredential = errors.New("unrecognized Kiro credential source")

func importCredentialFile(path, mode string) ([]credential, error) {
	ext := strings.ToLower(filepath.Ext(path))
	if ext == ".sqlite" || ext == ".sqlite3" || ext == ".db" {
		cred, errLoad := loadSQLiteCredential(path, mode)
		if errLoad != nil {
			return nil, errLoad
		}
		return []credential{cred}, nil
	}
	raw, errRead := os.ReadFile(path)
	if errRead != nil {
		return nil, fmt.Errorf("read Kiro credential %s: %w", path, errRead)
	}
	creds, errParse := parseCredentialJSON(raw, path, mode)
	if errParse != nil {
		return nil, errParse
	}
	for index := range creds {
		resolveSiblingRegistration(&creds[index])
	}
	return creds, nil
}

func parseCredentialJSON(raw []byte, sourcePath, mode string) ([]credential, error) {
	var value any
	if errUnmarshal := json.Unmarshal(raw, &value); errUnmarshal != nil {
		return nil, fmt.Errorf("decode Kiro credential JSON %s: %w", sourcePath, errUnmarshal)
	}
	var objects []map[string]any
	switch typed := value.(type) {
	case map[string]any:
		if accounts, ok := typed["accounts"].([]any); ok {
			for _, item := range accounts {
				if object, okObject := item.(map[string]any); okObject {
					objects = append(objects, object)
				}
			}
		} else {
			objects = append(objects, typed)
		}
	case []any:
		for _, item := range typed {
			if object, okObject := item.(map[string]any); okObject {
				objects = append(objects, object)
			}
		}
	default:
		return nil, errUnrecognizedCredential
	}

	var out []credential
	for _, object := range objects {
		cred := credentialFromMap(object, sourcePath, "json", mode)
		if cred.RefreshToken == "" {
			continue
		}
		out = append(out, cred)
	}
	if len(out) == 0 {
		return nil, errUnrecognizedCredential
	}
	return out, nil
}

func credentialFromMap(data map[string]any, sourcePath, sourceKind, mode string) credential {
	persistedSourcePath := stringValue(data, "source_path")
	if persistedSourcePath == "" {
		persistedSourcePath = sourcePath
	}
	persistedSourceKind := stringValue(data, "source_kind")
	if persistedSourceKind == "" {
		persistedSourceKind = sourceKind
	}
	persistedMode := stringValue(data, "mode")
	if persistedMode == "" {
		persistedMode = mode
	}
	cred := credential{
		Version:        1,
		AuthID:         stringValue(data, "authId", "auth_id"),
		Mode:           normalizeMode(persistedMode),
		SourcePath:     persistedSourcePath,
		SourceKind:     persistedSourceKind,
		SourceTokenKey: stringValue(data, "source_token_key"),
		AccessToken:    stringValue(data, "accessToken", "access_token"),
		RefreshToken:   stringValue(data, "refreshToken", "refresh_token"),
		ClientID:       stringValue(data, "clientId", "client_id"),
		ClientSecret:   stringValue(data, "clientSecret", "client_secret"),
		ClientIDHash:   stringValue(data, "clientIdHash", "client_id_hash"),
		ProfileARN:     stringValue(data, "profileArn", "profile_arn", "arn"),
		SSORegion:      stringValue(data, "ssoRegion", "sso_region", "region"),
		APIRegion:      stringValue(data, "apiRegion", "api_region"),
		ExpiresAt:      expiryValue(data, "expiresAt", "expires_at", "expiresIn", "expires_in"),
		Label:          stringValue(data, "label", "name", "accountName"),
		Fingerprint:    stringValue(data, "fingerprint"),
	}
	if scopes, okScopes := data["scopes"].([]any); okScopes {
		for _, scope := range scopes {
			if text, okText := scope.(string); okText {
				cred.Scopes = append(cred.Scopes, text)
			}
		}
	}
	finalizeCredential(&cred)
	return cred
}

func resolveSiblingRegistration(cred *credential) {
	if cred == nil || cred.ClientIDHash == "" || (cred.ClientID != "" && cred.ClientSecret != "") || cred.SourcePath == "" {
		return
	}
	registrationPath := filepath.Join(filepath.Dir(cred.SourcePath), cred.ClientIDHash+".json")
	raw, errRead := os.ReadFile(registrationPath)
	if errRead != nil {
		return
	}
	var registration map[string]any
	if json.Unmarshal(raw, &registration) != nil {
		return
	}
	cred.ClientID = stringValue(registration, "clientId", "client_id")
	cred.ClientSecret = stringValue(registration, "clientSecret", "client_secret")
	finalizeCredential(cred)
}

func loadSQLiteCredential(path, mode string) (credential, error) {
	db, errOpen := sql.Open("sqlite", "file:"+filepath.ToSlash(path)+"?mode=ro")
	if errOpen != nil {
		return credential{}, fmt.Errorf("open Kiro SQLite credential %s: %w", path, errOpen)
	}
	defer db.Close()

	var tokenKey, tokenJSON string
	for _, key := range sqliteTokenKeys {
		errQuery := db.QueryRow("SELECT value FROM auth_kv WHERE key = ?", key).Scan(&tokenJSON)
		if errQuery == nil {
			tokenKey = key
			break
		}
		if !errors.Is(errQuery, sql.ErrNoRows) {
			return credential{}, fmt.Errorf("read Kiro SQLite token %s: %w", path, errQuery)
		}
	}
	if tokenJSON == "" {
		return credential{}, errUnrecognizedCredential
	}
	var tokenData map[string]any
	if errJSON := json.Unmarshal([]byte(tokenJSON), &tokenData); errJSON != nil {
		return credential{}, fmt.Errorf("decode Kiro SQLite token %s: %w", path, errJSON)
	}
	cred := credentialFromMap(tokenData, path, "sqlite", mode)
	cred.SourceTokenKey = tokenKey

	for _, key := range sqliteRegistrationKeys {
		var registrationJSON string
		if db.QueryRow("SELECT value FROM auth_kv WHERE key = ?", key).Scan(&registrationJSON) != nil {
			continue
		}
		var registration map[string]any
		if json.Unmarshal([]byte(registrationJSON), &registration) == nil {
			cred.ClientID = stringValue(registration, "clientId", "client_id")
			cred.ClientSecret = stringValue(registration, "clientSecret", "client_secret")
			if cred.SSORegion == "" {
				cred.SSORegion = stringValue(registration, "region")
			}
		}
		break
	}

	var profileJSON string
	if db.QueryRow("SELECT value FROM state WHERE key = 'api.codewhisperer.profile'").Scan(&profileJSON) == nil {
		var profile map[string]any
		if json.Unmarshal([]byte(profileJSON), &profile) == nil {
			if arn := stringValue(profile, "arn"); arn != "" {
				cred.ProfileARN = arn
				if region := regionFromARN(arn); region != "" {
					cred.APIRegion = region
				}
			}
		}
	}
	finalizeCredential(&cred)
	if cred.RefreshToken == "" {
		return credential{}, errUnrecognizedCredential
	}
	return cred, nil
}

func reloadReferencedCredential(current credential) (credential, error) {
	if current.Mode != "reference" || strings.TrimSpace(current.SourcePath) == "" {
		return current, nil
	}
	creds, errLoad := importCredentialFile(current.SourcePath, current.Mode)
	if errLoad != nil {
		return current, fmt.Errorf("reload referenced Kiro credential: %w", errLoad)
	}
	if len(creds) == 0 {
		return current, fmt.Errorf("referenced Kiro credential contains no account")
	}
	best := creds[0]
	for _, candidate := range creds {
		if current.SourceTokenKey != "" && candidate.SourceTokenKey == current.SourceTokenKey {
			best = candidate
			break
		}
		if current.ProfileARN != "" && candidate.ProfileARN == current.ProfileARN {
			best = candidate
			break
		}
	}
	if best.Label == "" {
		best.Label = current.Label
	}
	if best.Fingerprint == "" {
		best.Fingerprint = current.Fingerprint
	}
	if best.AuthID == "" {
		best.AuthID = current.AuthID
	}
	return best, nil
}

func finalizeCredential(cred *credential) {
	if cred == nil {
		return
	}
	if cred.Type == "" {
		cred.Type = providerID
	}
	if cred.SSORegion == "" {
		cred.SSORegion = defaultRegion
	}
	if cred.APIRegion == "" {
		cred.APIRegion = regionFromARN(cred.ProfileARN)
	}
	if cred.APIRegion == "" {
		cred.APIRegion = defaultRegion
	}
	if strings.HasPrefix(cred.SourceKind, "oauth_") &&
		strings.EqualFold(cred.APIRegion, cred.SSORegion) &&
		!strings.EqualFold(cred.APIRegion, defaultRegion) &&
		strings.TrimSpace(loadedConfig().APIRegion) == "" {
		// v0.5.1 coupled the organization SSO region to the Kiro runtime
		// region. Repair those persisted OAuth credentials unless an explicit
		// api_region override says otherwise.
		cred.APIRegion = defaultRegion
	}
	if cred.ClientID != "" && cred.ClientSecret != "" {
		cred.AuthType = "aws_sso_oidc"
	} else {
		cred.AuthType = "kiro_desktop"
	}
	if cred.Label == "" {
		cred.Label = "Kiro Account"
	}
	if cred.Fingerprint == "" {
		sum := sha256.Sum256([]byte(cred.ProfileARN + "\x00" + cred.ClientID + "\x00" + cred.SourcePath))
		cred.Fingerprint = hex.EncodeToString(sum[:])
	}
}

func authDataFromCredential(cred credential) (authData, error) {
	finalizeCredential(&cred)
	id := credentialID(cred)
	cred.AuthID = id
	storage, errMarshal := json.Marshal(cred)
	if errMarshal != nil {
		return authData{}, fmt.Errorf("encode Kiro credential storage: %w", errMarshal)
	}
	nextRefresh := nextRefreshTime(cred)
	return authData{
		Provider:         providerID,
		ID:               id,
		FileName:         id + ".json",
		Label:            cred.Label,
		StorageJSON:      storage,
		Metadata:         map[string]any{"auth_type": cred.AuthType, "source_kind": cred.SourceKind},
		Attributes:       map[string]string{"auth_provider": providerID, "api_region": cred.APIRegion},
		NextRefreshAfter: nextRefresh,
	}, nil
}

func credentialID(cred credential) string {
	if stable := validCredentialID(cred.AuthID); stable != "" {
		return stable
	}
	identity := cred.ProfileARN + "\x00" + cred.ClientID + "\x00" + cred.SourcePath + "\x00" + cred.SourceTokenKey
	if strings.Trim(identity, "\x00") == "" {
		identity = cred.RefreshToken
	}
	sum := sha256.Sum256([]byte(identity))
	return "kiro-" + hex.EncodeToString(sum[:10])
}

func validCredentialID(value string) string {
	value = strings.TrimSpace(value)
	if len(value) != len("kiro-")+20 || !strings.HasPrefix(value, "kiro-") {
		return ""
	}
	for _, char := range value[len("kiro-"):] {
		if (char < '0' || char > '9') && (char < 'a' || char > 'f') {
			return ""
		}
	}
	return value
}

func nextRefreshTime(cred credential) time.Time {
	expires, errParse := parseExpiry(cred.ExpiresAt)
	if errParse != nil || expires.IsZero() {
		return time.Now().UTC()
	}
	refreshAt := expires.Add(-10 * time.Minute)
	if refreshAt.Before(time.Now().UTC()) {
		return time.Now().UTC()
	}
	return refreshAt
}

func parseExpiry(value string) (time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, errors.New("expiration is empty")
	}
	if integer, errInt := strconv.ParseInt(value, 10, 64); errInt == nil {
		if integer > 10_000_000_000 {
			integer /= 1000
		}
		return time.Unix(integer, 0).UTC(), nil
	}
	trimmed := value
	if dot := strings.Index(trimmed, "."); dot >= 0 {
		if zone := strings.IndexAny(trimmed[dot:], "Z+-"); zone >= 0 {
			absoluteZone := dot + zone
			fraction := trimmed[dot+1 : absoluteZone]
			if len(fraction) > 9 {
				trimmed = trimmed[:dot+1] + fraction[:9] + trimmed[absoluteZone:]
			}
		}
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02T15:04:05"} {
		if parsed, errParse := time.Parse(layout, trimmed); errParse == nil {
			return parsed.UTC(), nil
		}
	}
	return time.Time{}, fmt.Errorf("unsupported expiration format")
}

func expiryValue(data map[string]any, keys ...string) string {
	for _, key := range keys {
		value, exists := data[key]
		if !exists || value == nil {
			continue
		}
		switch typed := value.(type) {
		case string:
			return typed
		case float64:
			if strings.Contains(strings.ToLower(key), "expiresin") || strings.Contains(strings.ToLower(key), "expires_in") {
				return time.Now().UTC().Add(time.Duration(typed) * time.Second).Format(time.RFC3339)
			}
			return strconv.FormatInt(int64(typed), 10)
		}
	}
	return ""
}

func stringValue(data map[string]any, keys ...string) string {
	for _, key := range keys {
		value, exists := data[key]
		if !exists || value == nil {
			continue
		}
		if text, okText := value.(string); okText {
			return strings.TrimSpace(text)
		}
	}
	return ""
}

func textValue(data map[string]any, keys ...string) string {
	for _, key := range keys {
		value, exists := data[key]
		if !exists || value == nil {
			continue
		}
		if text, okText := value.(string); okText {
			return text
		}
	}
	return ""
}

func regionFromARN(arn string) string {
	parts := strings.Split(arn, ":")
	if len(parts) > 3 && strings.Count(parts[3], "-") >= 2 {
		return parts[3]
	}
	return ""
}

func normalizeMode(mode string) string {
	if strings.EqualFold(strings.TrimSpace(mode), "copy") {
		return "copy"
	}
	return "reference"
}

func deduplicateCredentials(creds []credential) []credential {
	seen := make(map[string]struct{})
	out := make([]credential, 0, len(creds))
	for _, cred := range creds {
		if cred.RefreshToken == "" {
			continue
		}
		id := credentialID(cred)
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, cred)
	}
	return out
}
