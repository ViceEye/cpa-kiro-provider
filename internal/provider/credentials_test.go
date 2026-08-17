package provider

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseDesktopCredentialAndPersistedReference(t *testing.T) {
	source := filepath.Join(t.TempDir(), "kiro-token.json")
	raw := []byte(`{"refreshToken":"refresh-fake","accessToken":"access-fake","profileArn":"arn:aws:codewhisperer:us-west-2:000000000000:profile/test","region":"eu-west-1","expiresAt":"2030-01-01T00:00:00Z"}`)
	creds, errParse := parseCredentialJSON(raw, source, "reference")
	if errParse != nil {
		t.Fatalf("parseCredentialJSON() error = %v", errParse)
	}
	if len(creds) != 1 {
		t.Fatalf("credentials = %d, want 1", len(creds))
	}
	cred := creds[0]
	if cred.AuthType != "kiro_desktop" || cred.APIRegion != "us-west-2" || cred.SSORegion != "eu-west-1" {
		t.Fatalf("credential = %#v", cred)
	}

	storage, _ := json.Marshal(cred)
	reloaded, errReload := parseCredentialJSON(storage, filepath.Join(t.TempDir(), "cpa-auth.json"), "copy")
	if errReload != nil {
		t.Fatalf("parse persisted credential: %v", errReload)
	}
	if reloaded[0].SourcePath != source || reloaded[0].Mode != "reference" {
		t.Fatalf("persisted source changed: %#v", reloaded[0])
	}
}

func TestImportEnterpriseCredentialPairsSiblingRegistration(t *testing.T) {
	dir := t.TempDir()
	tokenPath := filepath.Join(dir, "token.json")
	if errWrite := os.WriteFile(tokenPath, []byte(`{"refreshToken":"enterprise-refresh","clientIdHash":"device-hash","region":"us-east-1"}`), 0o600); errWrite != nil {
		t.Fatal(errWrite)
	}
	if errWrite := os.WriteFile(filepath.Join(dir, "device-hash.json"), []byte(`{"clientId":"client-fake","clientSecret":"secret-fake"}`), 0o600); errWrite != nil {
		t.Fatal(errWrite)
	}
	creds, errImport := importCredentials(tokenPath, "reference")
	if errImport != nil {
		t.Fatalf("importCredentials() error = %v", errImport)
	}
	if len(creds) != 1 || creds[0].AuthType != "aws_sso_oidc" || creds[0].ClientID != "client-fake" {
		t.Fatalf("credential = %#v", creds)
	}
}

func TestLoadKiroCLISQLiteCredential(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data.sqlite3")
	db, errOpen := sql.Open("sqlite", path)
	if errOpen != nil {
		t.Fatal(errOpen)
	}
	statements := []string{
		`CREATE TABLE auth_kv (key TEXT PRIMARY KEY, value TEXT NOT NULL)`,
		`CREATE TABLE state (key TEXT PRIMARY KEY, value TEXT NOT NULL)`,
		`INSERT INTO auth_kv(key,value) VALUES ('kirocli:odic:token','{"access_token":"access-fake","refresh_token":"refresh-fake","region":"eu-central-1","expires_at":"2030-01-01T00:00:00Z"}')`,
		`INSERT INTO auth_kv(key,value) VALUES ('kirocli:odic:device-registration','{"client_id":"client-fake","client_secret":"secret-fake","region":"eu-central-1"}')`,
		`INSERT INTO state(key,value) VALUES ('api.codewhisperer.profile','{"arn":"arn:aws:codewhisperer:us-east-1:000000000000:profile/test"}')`,
	}
	for _, statement := range statements {
		if _, errExec := db.Exec(statement); errExec != nil {
			t.Fatal(errExec)
		}
	}
	if errClose := db.Close(); errClose != nil {
		t.Fatal(errClose)
	}

	cred, errLoad := loadSQLiteCredential(path, "reference")
	if errLoad != nil {
		t.Fatalf("loadSQLiteCredential() error = %v", errLoad)
	}
	if cred.SourceTokenKey != "kirocli:odic:token" || cred.AuthType != "aws_sso_oidc" || cred.APIRegion != "us-east-1" {
		t.Fatalf("credential = %#v", cred)
	}
}

func TestCredentialIDSurvivesRefreshTokenRotation(t *testing.T) {
	base := credential{ProfileARN: "arn:aws:codewhisperer:us-east-1:0:profile/test", SourcePath: "/credentials/token.json", RefreshToken: "old"}
	rotated := base
	rotated.RefreshToken = "new"
	if credentialID(base) != credentialID(rotated) {
		t.Fatalf("credential ID changed after refresh-token rotation")
	}
}

func TestRefreshCredentialDesktopAndOIDC(t *testing.T) {
	original := hostHTTPDoCall
	defer func() { hostHTTPDoCall = original }()
	var requests []hostHTTPRequest
	hostHTTPDoCall = func(req hostHTTPRequest) (hostHTTPResponse, error) {
		requests = append(requests, req)
		if strings.Contains(req.URL, "oidc.") {
			return hostHTTPResponse{StatusCode: 200, Body: []byte(`{"accessToken":"oidc-access","refreshToken":"oidc-rotated","expiresIn":7200}`)}, nil
		}
		return hostHTTPResponse{StatusCode: 200, Body: []byte(`{"accessToken":"desktop-access","refreshToken":"desktop-rotated","profileArn":"arn:aws:codewhisperer:us-east-1:0:profile/new","expiresIn":3600}`)}, nil
	}
	desktop, errDesktop := refreshCredential(credential{Mode: "copy", RefreshToken: "desktop-refresh", SSORegion: "us-east-1", Fingerprint: "fake"}, "callback")
	if errDesktop != nil {
		t.Fatalf("desktop refresh: %v", errDesktop)
	}
	if desktop.AccessToken != "desktop-access" || desktop.RefreshToken != "desktop-rotated" {
		t.Fatalf("desktop = %#v", desktop)
	}
	oidc, errOIDC := refreshCredential(credential{Mode: "copy", RefreshToken: "oidc-refresh", ClientID: "client-fake", ClientSecret: "secret-fake", SSORegion: "eu-west-1"}, "callback")
	if errOIDC != nil {
		t.Fatalf("OIDC refresh: %v", errOIDC)
	}
	if oidc.AccessToken != "oidc-access" || !strings.Contains(requests[1].URL, "oidc.eu-west-1.amazonaws.com") {
		t.Fatalf("OIDC = %#v request=%#v", oidc, requests[1])
	}
	if expires, _ := parseExpiry(oidc.ExpiresAt); expires.Before(time.Now().Add(60 * time.Minute)) {
		t.Fatalf("unexpected OIDC expiry %s", oidc.ExpiresAt)
	}
}

func TestFinalizeCredentialRepairsLegacyOAuthRegionCoupling(t *testing.T) {
	originalConfig := loadedConfig()
	t.Cleanup(func() { configValue.Store(originalConfig) })
	configValue.Store(pluginConfig{ImportMode: "copy", ModelPrefix: "kiro/"})

	cred := credential{
		SourceKind:   "oauth_device",
		RefreshToken: "fixture-refresh",
		ClientID:     "fixture-client",
		ClientSecret: "fixture-secret",
		SSORegion:    "eu-west-1",
		APIRegion:    "eu-west-1",
	}
	finalizeCredential(&cred)
	if cred.SSORegion != "eu-west-1" || cred.APIRegion != defaultRegion {
		t.Fatalf("repaired credential regions = sso:%q api:%q", cred.SSORegion, cred.APIRegion)
	}
}

func TestFinalizeCredentialKeepsExplicitOAuthAPIRegionOverride(t *testing.T) {
	originalConfig := loadedConfig()
	t.Cleanup(func() { configValue.Store(originalConfig) })
	configValue.Store(pluginConfig{ImportMode: "copy", ModelPrefix: "kiro/", APIRegion: "eu-west-1"})

	cred := credential{
		SourceKind:   "oauth_device",
		RefreshToken: "fixture-refresh",
		ClientID:     "fixture-client",
		ClientSecret: "fixture-secret",
		SSORegion:    "eu-west-1",
		APIRegion:    "eu-west-1",
	}
	finalizeCredential(&cred)
	if cred.APIRegion != "eu-west-1" {
		t.Fatalf("explicit API region override = %q", cred.APIRegion)
	}
}
