package cline

// pluginProvider is the CPA provider identity of the host plugin. Cline
// connections share this identity and use Kind for internal dispatch.
const pluginProvider = "nexus"

// TypeMarker is the credential kind discriminator stored in StorageJSON.
const TypeMarker = "cline"
