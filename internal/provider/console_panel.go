package provider

import _ "embed"

//go:embed console/index.html
var consoleHTML []byte

//go:embed console-ui/src/assets/nexus.svg
var nexusLogoSVG []byte

func consolePanelHTML() []byte { return consoleHTML }
