package cmd

import (
	"bytes"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// --- Error conditions ---

func TestUnclassifiedCommandAdvertisesEveryAPIOnlyFlag(t *testing.T) {
	root := &cobra.Command{Use: "shovels"}
	unclassified := &cobra.Command{Use: "unclassified", Run: func(*cobra.Command, []string) {}}
	root.AddCommand(unclassified)

	advertised := advertisedAPIOnlyFlags(unclassified)

	for _, flag := range []string{"limit", "max-records", "no-retry", "timeout", "dry-run"} {
		if !advertised[flag] {
			t.Errorf("a command with no contract record should still advertise --%s, got %v", flag, advertised)
		}
	}
}

// --- Edge cases ---

// version honors none of the five and renders through cobra's default usage
// template, whose Global Flags expression is substituted for the filter. A
// cobra release renaming that expression makes the substitution a silent
// no-op, and --limit comes back; --base-url proves the section still renders.
func TestDefaultUsageTemplateAdvertisesOnlyTheHonoredGlobalFlags(t *testing.T) {
	usage := versionCmd.UsageString()

	if strings.Contains(usage, "--limit") {
		t.Errorf("version usage should not advertise the unhonored --limit, got:\n%s", usage)
	}
	if !strings.Contains(usage, "--base-url") {
		t.Errorf("version usage should advertise --base-url, got:\n%s", usage)
	}
}

// Every command wired to writeGroupedUsage honors all five flags, so only a
// command posed against a filtered record shows that path filters too.
func TestGroupedUsageAdvertisesOnlyTheHonoredGlobalFlags(t *testing.T) {
	root := &cobra.Command{Use: "shovels"}
	root.PersistentFlags().String("limit", "50", "maximum records")
	root.PersistentFlags().Bool("dry-run", false, "print the request")
	config := &cobra.Command{Use: "config"}
	set := &cobra.Command{Use: "set", Run: func(*cobra.Command, []string) {}}
	set.Flags().Bool("force", false, "overwrite an existing value")
	config.AddCommand(set)
	root.AddCommand(config)
	setGroupedUsage(set, []flagGroup{{Title: "Required Flags", Names: []string{"force"}}})
	var usage bytes.Buffer
	set.SetOut(&usage)

	_ = set.Usage()

	if !strings.Contains(usage.String(), "--dry-run") {
		t.Errorf("grouped usage should advertise the honored --dry-run, got:\n%s", usage.String())
	}
	if strings.Contains(usage.String(), "--limit") {
		t.Errorf("grouped usage should not advertise the unhonored --limit, got:\n%s", usage.String())
	}
}
