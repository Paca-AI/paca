// Command paca-acp-bridge connects an ACP-type Paca agent to a coding CLI
// running on your own machine. See apps/acp-bridge/README.md.
package main

import (
	"os"

	"github.com/Paca-AI/paca/apps/acp-bridge/internal/cli"
)

// version is stamped at release-build time via
// `-ldflags "-X main.version=..."` (see .github/workflows/cd.yml's
// build-acp-bridge job) — "dev" for a plain local `go build`/`go run`.
var version = "dev"

func main() {
	os.Exit(cli.Main(os.Args[1:], version))
}
