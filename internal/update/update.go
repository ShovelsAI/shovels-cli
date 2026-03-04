// Package update provides background autoupdate via GitHub Releases.
// The goroutine checks for a newer stable version, downloads it with
// checksum validation, and atomically replaces the running binary.
// All failures are silent — the user's command is never interrupted.
package update

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	selfupdate "github.com/creativeprojects/go-selfupdate"
)

const (
	// repo is the GitHub owner/repo slug for release detection.
	repo = "ShovelsAI/shovels-cli"

	// cacheFileName is written inside the config directory to throttle
	// update checks to once every 24 hours.
	cacheFileName = ".update-check"

	// checkInterval is the minimum duration between update checks.
	checkInterval = 24 * time.Hour

	// Timeout is the maximum wall-clock time the update goroutine may
	// consume before the program exits without waiting further.
	Timeout = 10 * time.Second
)

// Result carries the outcome of a background update check back to the
// caller so the stderr notice can be printed after stdout completes.
type Result struct {
	// Updated is true when a new version was downloaded and applied.
	Updated bool
	// OldVersion is the version before the update.
	OldVersion string
	// NewVersion is the version after the update.
	NewVersion string
}

// Updater abstracts go-selfupdate operations for testing.
type Updater interface {
	DetectLatest(ctx context.Context, repo selfupdate.Repository) (*selfupdate.Release, bool, error)
	UpdateTo(ctx context.Context, rel *selfupdate.Release, cmdPath string) error
}

// Clock abstracts time operations for testing.
type Clock interface {
	Now() time.Time
}

// realClock uses the real system clock.
type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }

// VersionCompareFunc determines whether a release is newer than the
// current version and returns the release's version string. If nil,
// uses release.GreaterThan and release.Version().
type VersionCompareFunc func(release *selfupdate.Release, current string) (isNewer bool, newVersion string)

// Options configures a background update check.
type Options struct {
	// CurrentVersion is the running binary version (e.g., "0.2.1").
	CurrentVersion string
	// ConfigDir is the directory where the cache file lives
	// (e.g., ~/.config/shovels).
	ConfigDir string
	// Updater performs the actual GitHub API call and binary replacement.
	// If nil, a real go-selfupdate updater is created.
	Updater Updater
	// Clock provides the current time. If nil, uses system clock.
	Clock Clock
	// ExePath overrides the binary path for testing. If empty, uses
	// os.Executable().
	ExePath string
	// VersionCompare overrides the default release.GreaterThan check.
	// Used in tests where the Release's internal semver field cannot be
	// set from outside the library.
	VersionCompare VersionCompareFunc
}

// CacheExpired returns true when the cache file is missing or older than
// checkInterval. If clk is nil, the system clock is used.
func CacheExpired(configDir string, clk Clock) bool {
	if clk == nil {
		clk = realClock{}
	}
	path := filepath.Join(configDir, cacheFileName)
	info, err := os.Stat(path)
	if err != nil {
		return true
	}
	return clk.Now().Sub(info.ModTime()) >= checkInterval
}

// touchCache creates or updates the cache file's modification time.
// It creates parent directories if they don't exist (e.g., first run
// before config dir is established).
func touchCache(configDir string) {
	path := filepath.Join(configDir, cacheFileName)
	now := time.Now()
	if err := os.Chtimes(path, now, now); err != nil {
		// File or directory may not exist yet; ensure parents exist.
		if mkErr := os.MkdirAll(configDir, 0o700); mkErr != nil {
			return
		}
		f, createErr := os.Create(path)
		if createErr != nil {
			return
		}
		f.Close()
	}
}

// newUpdater creates a real go-selfupdate updater with checksum
// validation and pre-release filtering disabled.
func newUpdater() (Updater, error) {
	u, err := selfupdate.NewUpdater(selfupdate.Config{
		Validator: &selfupdate.ChecksumValidator{
			UniqueFilename: "checksums.txt",
		},
		Prerelease: false,
	})
	if err != nil {
		return nil, err
	}
	return u, nil
}

// Check runs a synchronous update check + apply. It is designed to be
// called from a goroutine. The caller provides a context with deadline
// for the 10s timeout budget.
//
// Returns a Result indicating whether an update was applied, or nil
// if no update occurred (including all error paths).
func Check(ctx context.Context, opts Options) *Result {
	clk := opts.Clock
	if clk == nil {
		clk = realClock{}
	}

	if !CacheExpired(opts.ConfigDir, clk) {
		return nil
	}

	updater := opts.Updater
	if updater == nil {
		var err error
		updater, err = newUpdater()
		if err != nil {
			return nil
		}
	}

	slug := selfupdate.ParseSlug(repo)
	release, found, err := updater.DetectLatest(ctx, slug)
	if err != nil || !found {
		// Touch cache even on failure to avoid hammering GitHub on every
		// invocation when the API is unreachable.
		touchCache(opts.ConfigDir)
		return nil
	}

	cmpFn := opts.VersionCompare
	if cmpFn == nil {
		cmpFn = func(r *selfupdate.Release, cur string) (bool, string) {
			return r.GreaterThan(cur), r.Version()
		}
	}
	newer, newVersion := cmpFn(release, opts.CurrentVersion)
	if !newer {
		touchCache(opts.ConfigDir)
		return nil
	}

	exePath := opts.ExePath
	if exePath == "" {
		exePath, err = os.Executable()
		if err != nil {
			return nil
		}
	}

	if err := updater.UpdateTo(ctx, release, exePath); err != nil {
		// Touch cache so a persistent failure (permission denied, timeout)
		// doesn't retry on every invocation — retry tomorrow instead.
		touchCache(opts.ConfigDir)
		return nil
	}

	touchCache(opts.ConfigDir)
	return &Result{
		Updated:    true,
		OldVersion: opts.CurrentVersion,
		NewVersion: newVersion,
	}
}

// NoticeMessage formats the stderr update notice.
func NoticeMessage(r *Result) string {
	if r == nil || !r.Updated {
		return ""
	}
	return fmt.Sprintf("Updated shovels v%s \u2192 v%s\n", r.OldVersion, r.NewVersion)
}
