package provider

import _ "embed"

//go:embed console/index.html
var consoleHTML []byte

func consolePanelHTML() []byte { return consoleHTML }
