package provider

import "testing"

func TestApplyConfigAcceptsPluginScopedYAML(t *testing.T) {
	original := loadedConfig()
	t.Cleanup(func() { configValue.Store(original) })

	applyConfig([]byte(`
enabled: true
priority: 100
login_mode: kiro-browser
runtime_base_url: http://kiro-mock:18080
model_discovery_url: http://kiro-mock:18080/ListAvailableModels
`))

	got := loadedConfig()
	if got.RuntimeBaseURL != "http://kiro-mock:18080" {
		t.Fatalf("RuntimeBaseURL = %q", got.RuntimeBaseURL)
	}
	if got.LoginMode != "kiro-browser" || got.BrowserRedirectURI != defaultRedirectURI || got.DesktopTokenURL != defaultTokenURL {
		t.Fatalf("browser login defaults = mode:%q redirect:%q token:%q", got.LoginMode, got.BrowserRedirectURI, got.DesktopTokenURL)
	}
	if got.ModelDiscoveryURL != "http://kiro-mock:18080/ListAvailableModels" {
		t.Fatalf("ModelDiscoveryURL = %q", got.ModelDiscoveryURL)
	}
}

func TestApplyConfigAcceptsCompleteCPAYAML(t *testing.T) {
	original := loadedConfig()
	t.Cleanup(func() { configValue.Store(original) })

	applyConfig([]byte(`
plugins:
  configs:
    kiro-provider:
      import_mode: copy
      api_region: eu-west-1
      static_models:
        - fixture-extra
`))

	got := loadedConfig()
	if got.ImportMode != "copy" {
		t.Fatalf("ImportMode = %q", got.ImportMode)
	}
	if got.APIRegion != "eu-west-1" {
		t.Fatalf("APIRegion = %q", got.APIRegion)
	}
	if len(got.StaticModels) != 1 || got.StaticModels[0] != "fixture-extra" {
		t.Fatalf("StaticModels = %#v", got.StaticModels)
	}
}
