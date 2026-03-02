package cmd

import (
	"bytes"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestSetGroupedUsageRendersGroups(t *testing.T) {
	cmd := &cobra.Command{
		Use: "test [flags]",
		Run: func(cmd *cobra.Command, args []string) {},
	}
	cmd.Flags().String("name", "", "a name flag")
	cmd.Flags().Int("count", 0, "a count flag")
	cmd.Flags().Bool("verbose", false, "enable verbose output")

	setGroupedUsage(cmd, []flagGroup{
		{Title: "Required Flags", Names: []string{"name"}},
		{Title: "Optional Flags", Names: []string{"count", "verbose"}},
	})

	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	_ = cmd.Usage()
	out := buf.String()

	if !strings.Contains(out, "Required Flags:") {
		t.Errorf("expected 'Required Flags:' group header in output:\n%s", out)
	}
	if !strings.Contains(out, "Optional Flags:") {
		t.Errorf("expected 'Optional Flags:' group header in output:\n%s", out)
	}
	if !strings.Contains(out, "--name") {
		t.Errorf("expected --name flag in output:\n%s", out)
	}
	if !strings.Contains(out, "--count") {
		t.Errorf("expected --count flag in output:\n%s", out)
	}
	if !strings.Contains(out, "--verbose") {
		t.Errorf("expected --verbose flag in output:\n%s", out)
	}
}

func TestSetGroupedUsageUnclaimedFlagsInOther(t *testing.T) {
	cmd := &cobra.Command{
		Use: "test [flags]",
		Run: func(cmd *cobra.Command, args []string) {},
	}
	cmd.Flags().String("name", "", "a name flag")
	cmd.Flags().Bool("debug", false, "enable debug mode")

	setGroupedUsage(cmd, []flagGroup{
		{Title: "Required Flags", Names: []string{"name"}},
	})

	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	_ = cmd.Usage()
	out := buf.String()

	if !strings.Contains(out, "Other Flags:") {
		t.Errorf("expected 'Other Flags:' section for unclaimed flags:\n%s", out)
	}
	if !strings.Contains(out, "--debug") {
		t.Errorf("expected --debug in Other Flags:\n%s", out)
	}
}

func TestSetGroupedUsageGroupOrder(t *testing.T) {
	cmd := &cobra.Command{
		Use: "test [flags]",
		Run: func(cmd *cobra.Command, args []string) {},
	}
	cmd.Flags().String("alpha", "", "first")
	cmd.Flags().String("beta", "", "second")

	setGroupedUsage(cmd, []flagGroup{
		{Title: "Group A", Names: []string{"alpha"}},
		{Title: "Group B", Names: []string{"beta"}},
	})

	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	_ = cmd.Usage()
	out := buf.String()

	posA := strings.Index(out, "Group A:")
	posB := strings.Index(out, "Group B:")
	if posA < 0 || posB < 0 {
		t.Fatalf("expected both groups in output:\n%s", out)
	}
	if posA >= posB {
		t.Errorf("expected Group A before Group B in output:\n%s", out)
	}
}

func TestPermitsSearchHelpHasGroupedFlags(t *testing.T) {
	var buf bytes.Buffer
	permitsSearchCmd.SetOut(&buf)
	permitsSearchCmd.SetErr(&buf)
	_ = permitsSearchCmd.Usage()
	out := buf.String()

	for _, header := range []string{
		"Required Flags:",
		"Permit Filters:",
		"Property Filters:",
		"Contractor Filters:",
	} {
		if !strings.Contains(out, header) {
			t.Errorf("expected %q in permits search help output:\n%s", header, out)
		}
	}

	// Verify no flat "Flags:" section (Cobra default).
	lines := strings.Split(out, "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "Flags:" {
			t.Errorf("expected no flat 'Flags:' section, but found one:\n%s", out)
		}
	}
}
