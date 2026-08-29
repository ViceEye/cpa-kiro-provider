package cline

// This file exposes the cline protocol entry points that the host plugin's
// dispatcher (internal/provider) calls when a credential's type is "cline".
// Each function receives the same raw plugin-method payload the dispatcher
// got and returns the plugin-method response envelope.
