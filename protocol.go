// Package thothmemory carries repository-root assets that binaries embed.
package thothmemory

import _ "embed"

// AgentProtocol is THOTH_LITE_AGENT_PROTOCOL.md, the one official text agents
// follow. Binaries install it; the file in the repository is the source.
//
//go:embed THOTH_LITE_AGENT_PROTOCOL.md
var AgentProtocol string
