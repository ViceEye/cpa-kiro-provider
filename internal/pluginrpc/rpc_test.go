package pluginrpc

import (
	"encoding/json"
	"testing"
)

func TestHTTPRequestUsesCPAWireKeys(t *testing.T) {
	raw, err := json.Marshal(HTTPRequest{
		HostCallbackID: "callback-1",
		Method:         "GET",
		URL:            "https://example.test",
	})
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["host_callback_id"] != "callback-1" {
		t.Fatalf("host callback key = %#v", payload)
	}
	if _, exists := payload["HostCallbackID"]; exists {
		t.Fatalf("legacy host callback key was emitted: %#v", payload)
	}
}

func TestManagementJSONKeepsHostResponseShape(t *testing.T) {
	raw := ManagementJSON(200, map[string]any{"status": "ok"})
	var envelope map[string]any
	if err := json.Unmarshal(raw, &envelope); err != nil {
		t.Fatal(err)
	}
	result := envelope["result"].(map[string]any)
	if result["StatusCode"] != float64(200) {
		t.Fatalf("status code = %#v", result["StatusCode"])
	}
	if result["Body"] == nil || result["Headers"] == nil {
		t.Fatalf("management response missing body or headers: %#v", result)
	}
}
