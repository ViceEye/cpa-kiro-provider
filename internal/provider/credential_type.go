package provider

import (
	"encoding/json"

	"github.com/ViceEye/cpa-provider-nexus/internal/cline"
)

// credentialTypeMarker peeks the "type" discriminator from a StorageJSON /
// RawJSON payload so the dispatcher can hand non-kiro credentials to their
// protocol packages.
func credentialTypeMarker(raw []byte) string {
	if len(raw) == 0 {
		return ""
	}
	var probe struct {
		Type string `json:"type"`
		Kind string `json:"kind"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		return ""
	}
	if probe.Kind == cline.TypeMarker {
		return cline.TypeMarker
	}
	return probe.Type
}
