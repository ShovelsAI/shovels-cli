package main

import (
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/shovels-ai/shovels-cli/cmd"
)

// Build-time variables injected via ldflags (GoReleaser populates these).
// For local builds without ldflags, init() populates commit and date from git.
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func init() {
	if commit == "none" {
		if out, err := exec.Command("git", "rev-parse", "--short", "HEAD").Output(); err == nil {
			commit = strings.TrimSpace(string(out))
		}
	}
	if date == "unknown" {
		date = time.Now().UTC().Format(time.RFC3339)
	}
}

func main() {
	cmd.SetVersionInfo(version, commit, date)
	os.Exit(cmd.Execute())
}
