// Package contract records, for every runnable shovels command, which of the
// API-only global flags that command honors and how it retrieves multiple
// records. Both answers live in one record per command, and a single record
// cannot disagree with itself.
//
// A command honors a flag if and only if it reads that flag and applies it.
// Making an HTTP request and calling Paginate are correlates, not the rule:
// version calls the API with a fixed timeout and honors nothing at all.
//
// The package holds no cobra dependency, which keeps it importable from the
// standalone schema generator: that generator is package main and cannot
// import package cmd.
package contract

import (
	"fmt"
	"maps"
	"slices"
)

// The API-only global flags, named by their long form without the dashes.
// --base-url is absent: every command that resolves config honors it.
const (
	FlagLimit      = "limit"
	FlagMaxRecords = "max-records"
	FlagNoRetry    = "no-retry"
	FlagTimeout    = "timeout"
	FlagDryRun     = "dry-run"
)

// APIOnlyFlags returns the five flags in the order root registers them. The
// order is fixed in a slice rather than derived from a map so that every
// listing built from it is identical between runs.
func APIOnlyFlags() []string {
	return []string{FlagLimit, FlagMaxRecords, FlagNoRetry, FlagTimeout, FlagDryRun}
}

// Mode is how a command retrieves multiple records.
type Mode string

const (
	// ModeNone collects no record set: the command either makes no request at
	// all, or makes exactly one whose result size the endpoint fixes. Either
	// way --limit and --max-records have nothing to bound.
	ModeNone Mode = "none"

	// ModeCursor follows the endpoint's continuation cursor until --limit is
	// satisfied, or until --max-records under --limit=all.
	ModeCursor Mode = "cursor"

	// ModeServerCapped returns at most Cap records and never a cursor. --limit
	// narrows the result below the cap; nothing widens it past.
	ModeServerCapped Mode = "server_capped"
)

// Record is one runnable command's contract. A non-runnable parent carries
// none: it runs no code of its own, so it reads no flag and retrieves no
// records.
type Record struct {
	// Honored lists the API-only flags the command reads and applies.
	Honored []string

	// Mode is the command's pagination mode.
	Mode Mode

	// Cap is the endpoint's fixed maximum record count, set only when Mode is
	// ModeServerCapped.
	Cap int
}

// Honors reports whether the command reads and applies the named flag.
func (r Record) Honors(flag string) bool {
	return slices.Contains(r.Honored, flag)
}

// Validate reports whether the record is internally consistent: it names only
// real flags, carries a known mode, and carries a cap exactly when the mode is
// ModeServerCapped. Cap is the one field with no meaningful zero value, so a
// ModeServerCapped record missing it is incomplete rather than defaulted.
func (r Record) Validate() error {
	honored := map[string]bool{}
	for _, flag := range r.Honored {
		if !slices.Contains(APIOnlyFlags(), flag) {
			return fmt.Errorf("honors %q, which is not an API-only flag", flag)
		}
		if honored[flag] {
			return fmt.Errorf("honors %q more than once", flag)
		}
		honored[flag] = true
	}

	switch r.Mode {
	case ModeNone, ModeCursor:
		if r.Cap != 0 {
			return fmt.Errorf("mode %q carries cap %d; only %q has a cap", r.Mode, r.Cap, ModeServerCapped)
		}
	case ModeServerCapped:
		if r.Cap <= 0 {
			return fmt.Errorf("mode %q requires a positive cap, got %d", r.Mode, r.Cap)
		}
	default:
		return fmt.Errorf("unknown pagination mode %q", r.Mode)
	}

	paginates := r.Mode != ModeNone
	for _, flag := range []string{FlagLimit, FlagMaxRecords} {
		if honored[flag] != paginates {
			return fmt.Errorf("mode %q disagrees with honoring %q", r.Mode, flag)
		}
	}
	return nil
}

// Lookup returns the record filed under a space-separated command path
// ("cities search"), and whether one exists. Absence means unclassified, not
// "honors nothing", so a caller finding none must fail open rather than read a
// missing record as an empty contract.
func Lookup(path string) (Record, bool) {
	record, ok := records[path]
	return record, ok
}

// All returns every filed record keyed by command path.
func All() map[string]Record {
	return maps.Clone(records)
}

// Register files the contract for a command that is compiled behind a build
// tag, whose record must live in the same build-tagged file as the command so
// the command tree and the classification agree under every build.
func Register(path string, record Record) {
	if _, exists := records[path]; exists {
		panic("contract: duplicate record for " + path)
	}
	records[path] = record
}
