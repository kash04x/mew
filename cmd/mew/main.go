// Command mew is a personal MCP server and its management CLI. The `serve`
// subcommand runs the MCP stdio server (bundling toolsets for internal
// systems like Redash and ClickUp); the remaining commands configure,
// diagnose, install, and update it.
package main

import (
	"mew/internal/cli"
	"mew/internal/version"
)

func main() {
	cli.Execute(version.Version)
}
