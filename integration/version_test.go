//go:build integration

package integration

import "testing"

// The version gate decides whether an assertion runs against the binary under
// test, so an ordering mistake either skips a release that should be checked or
// asserts against one that legitimately predates the output. These cases need no
// key, no network, and no credits.

func TestPrereleaseSortsBeforeTheVersionItQualifies(t *testing.T) {
	got := compareVersions("0.9.0-rc.1", "0.9.0")

	if got != -1 {
		t.Errorf("compareVersions(0.9.0-rc.1, 0.9.0) = %d, want -1: a release candidate precedes its release", got)
	}
}

func TestAnEarlierReleaseSortsBefore(t *testing.T) {
	got := compareVersions("0.8.4", "0.9.0")

	if got != -1 {
		t.Errorf("compareVersions(0.8.4, 0.9.0) = %d, want -1", got)
	}
}

func TestComponentsCompareNumericallyNotLexically(t *testing.T) {
	got := compareVersions("0.10.0", "0.9.0")

	if got != 1 {
		t.Errorf("compareVersions(0.10.0, 0.9.0) = %d, want 1: 10 is above 9", got)
	}
}

func TestAVersionEqualsItself(t *testing.T) {
	got := compareVersions("0.9.0", "0.9.0")

	if got != 0 {
		t.Errorf("compareVersions(0.9.0, 0.9.0) = %d, want 0", got)
	}
}

func TestALeadingVIsNotPartOfTheNumber(t *testing.T) {
	got := compareVersions("v0.9.0", "0.9.0")

	if got != 0 {
		t.Errorf("compareVersions(v0.9.0, 0.9.0) = %d, want 0: the tag prefix is not a version component", got)
	}
}
