package agent

import "embed"

// AgentConfigs contains the embedded agent configuration files.
//
//go:embed agents/*.md
var AgentConfigs embed.FS
