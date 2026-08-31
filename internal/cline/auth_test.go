package cline

import (
	"encoding/json"
	"testing"
)

func TestAuthDataUsesNexusIdentity(t *testing.T) {
	auth, err := authDataFromCredential(credential{Email: "fixture@example.com", RefreshToken: "fixture-refresh"})
	if err != nil {
		t.Fatal(err)
	}
	var stored credential
	if err := json.Unmarshal(auth.StorageJSON, &stored); err != nil {
		t.Fatal(err)
	}
	if auth.Provider != pluginProvider || stored.Type != pluginProvider || stored.Kind != TypeMarker {
		t.Fatalf("auth identity = provider:%q storage:%#v", auth.Provider, stored)
	}
}
