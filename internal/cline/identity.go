package cline

// pluginProvider is the CPA provider identity of the host plugin
// (kiro-provider registers as "kiro"). Cline connections live under this
// identity; the credential's own type field ("cline") is what the plugin
// dispatches on.
const pluginProvider = "kiro"

// TypeMarker is the credential type discriminator stored in StorageJSON.
const TypeMarker = "cline"
