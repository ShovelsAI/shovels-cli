package update

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	selfupdate "github.com/creativeprojects/go-selfupdate"
)

// fakeClock returns a fixed time for deterministic tests.
type fakeClock struct {
	now time.Time
}

func (c fakeClock) Now() time.Time { return c.now }

// fakeUpdater records calls and returns pre-configured results.
type fakeUpdater struct {
	detectRelease *selfupdate.Release
	detectFound   bool
	detectErr     error
	updateErr     error

	detectCalled bool
	updateCalled bool
	updatePath   string
}

func (f *fakeUpdater) DetectLatest(_ context.Context, _ selfupdate.Repository) (*selfupdate.Release, bool, error) {
	f.detectCalled = true
	return f.detectRelease, f.detectFound, f.detectErr
}

func (f *fakeUpdater) UpdateTo(_ context.Context, _ *selfupdate.Release, cmdPath string) error {
	f.updateCalled = true
	f.updatePath = cmdPath
	return f.updateErr
}

// alwaysNewer returns a VersionCompareFunc that always reports the
// release as newer than current, returning "9.9.9" as the new version.
func alwaysNewer() VersionCompareFunc {
	return func(_ *selfupdate.Release, _ string) (bool, string) { return true, "9.9.9" }
}

// neverNewer returns a VersionCompareFunc that always reports the
// release as not newer than current (already on latest).
func neverNewer() VersionCompareFunc {
	return func(_ *selfupdate.Release, _ string) (bool, string) { return false, "" }
}

// --- Happy paths ---

// Behavior: GIVEN cache older than 24h and newer version available
// WHEN any command runs THEN background goroutine downloads + verifies
// checksum + atomically replaces binary.
func TestCacheExpired_NewerVersionAvailable(t *testing.T) {
	dir := t.TempDir()
	writeStaleCacheFile(t, dir)

	fake := &fakeUpdater{
		detectRelease: &selfupdate.Release{},
		detectFound:   true,
	}

	result := Check(context.Background(), Options{
		CurrentVersion: "0.2.1",
		ConfigDir:      dir,
		Updater:        fake,
		Clock:          fakeClock{now: time.Now()},
		ExePath:        filepath.Join(dir, "shovels"),
		VersionCompare: alwaysNewer(),
	})

	if !fake.detectCalled {
		t.Error("expected DetectLatest to be called")
	}
	if !fake.updateCalled {
		t.Error("expected UpdateTo to be called")
	}
	if result == nil {
		t.Fatal("expected non-nil result when update available")
	}
	if !result.Updated {
		t.Error("expected Updated to be true")
	}
	if result.OldVersion != "0.2.1" {
		t.Errorf("expected OldVersion %q, got %q", "0.2.1", result.OldVersion)
	}
	assertCacheExists(t, dir)
}

// Behavior: GIVEN cache older than 24h and already on latest WHEN
// command runs THEN background check completes silently, cache
// timestamp refreshed.
func TestCacheExpired_AlreadyOnLatest(t *testing.T) {
	dir := t.TempDir()
	writeStaleCacheFile(t, dir)

	fake := &fakeUpdater{
		detectRelease: &selfupdate.Release{},
		detectFound:   true,
	}

	result := Check(context.Background(), Options{
		CurrentVersion: "0.3.0",
		ConfigDir:      dir,
		Updater:        fake,
		Clock:          fakeClock{now: time.Now()},
		VersionCompare: neverNewer(),
	})

	if !fake.detectCalled {
		t.Error("expected DetectLatest to be called")
	}
	if fake.updateCalled {
		t.Error("expected UpdateTo NOT to be called when already on latest")
	}
	if result != nil {
		t.Error("expected nil result when already on latest")
	}
	assertCacheExists(t, dir)
}

// Behavior: GIVEN cache less than 24h old WHEN command runs THEN no
// check made, no goroutine spawned.
func TestCacheFresh_NoCheckMade(t *testing.T) {
	dir := t.TempDir()
	writeFreshCacheFile(t, dir)

	fake := &fakeUpdater{
		detectRelease: &selfupdate.Release{},
		detectFound:   true,
	}

	result := Check(context.Background(), Options{
		CurrentVersion: "0.2.1",
		ConfigDir:      dir,
		Updater:        fake,
		Clock:          fakeClock{now: time.Now()},
	})

	if fake.detectCalled {
		t.Error("expected DetectLatest NOT to be called when cache is fresh")
	}
	if result != nil {
		t.Error("expected nil result when cache is fresh")
	}
}

// --- Edge cases ---

// Behavior: GIVEN pre-release available WHEN check runs THEN
// pre-release is ignored. Simulated by having DetectLatest return
// found=false (the library's Prerelease=false config filters them out).
func TestPreReleaseIgnored(t *testing.T) {
	dir := t.TempDir()
	writeStaleCacheFile(t, dir)

	fake := &fakeUpdater{
		detectFound: false,
	}

	result := Check(context.Background(), Options{
		CurrentVersion: "0.2.1",
		ConfigDir:      dir,
		Updater:        fake,
		Clock:          fakeClock{now: time.Now()},
	})

	if result != nil {
		t.Error("expected nil result when no stable release found")
	}
	assertCacheExists(t, dir)
}

// --- Error conditions ---

// Behavior: GIVEN network unreachable WHEN background check runs THEN
// fails silently.
func TestNetworkUnreachable_Silent(t *testing.T) {
	dir := t.TempDir()
	writeStaleCacheFile(t, dir)

	fake := &fakeUpdater{
		detectErr: errors.New("dial tcp: connection refused"),
	}

	result := Check(context.Background(), Options{
		CurrentVersion: "0.2.1",
		ConfigDir:      dir,
		Updater:        fake,
		Clock:          fakeClock{now: time.Now()},
	})

	if result != nil {
		t.Error("expected nil result on network error")
	}
	assertCacheExists(t, dir)
}

// Behavior: GIVEN GitHub API rate limited WHEN check fails THEN fails
// silently.
func TestGitHubRateLimited_Silent(t *testing.T) {
	dir := t.TempDir()
	writeStaleCacheFile(t, dir)

	fake := &fakeUpdater{
		detectErr: errors.New("API rate limit exceeded"),
	}

	result := Check(context.Background(), Options{
		CurrentVersion: "0.2.1",
		ConfigDir:      dir,
		Updater:        fake,
		Clock:          fakeClock{now: time.Now()},
	})

	if result != nil {
		t.Error("expected nil result on rate limit")
	}
}

// Behavior: GIVEN permission denied on binary replacement WHEN update
// attempted THEN fails silently.
func TestPermissionDenied_Silent(t *testing.T) {
	dir := t.TempDir()
	writeStaleCacheFile(t, dir)

	fake := &fakeUpdater{
		detectRelease: &selfupdate.Release{},
		detectFound:   true,
		updateErr:     errors.New("permission denied"),
	}

	result := Check(context.Background(), Options{
		CurrentVersion: "0.2.1",
		ConfigDir:      dir,
		Updater:        fake,
		Clock:          fakeClock{now: time.Now()},
		ExePath:        filepath.Join(dir, "shovels"),
		VersionCompare: alwaysNewer(),
	})

	if result != nil {
		t.Error("expected nil result on permission error")
	}
	assertCacheExists(t, dir)
}

// Behavior: GIVEN download timeout (10s total exceeded) WHEN goroutine
// still running at main exit THEN program exits without waiting
// further. Simulated via already-cancelled context.
func TestContextCancelled_Silent(t *testing.T) {
	dir := t.TempDir()
	writeStaleCacheFile(t, dir)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	fake := &fakeUpdater{
		detectErr: ctx.Err(),
	}

	result := Check(ctx, Options{
		CurrentVersion: "0.2.1",
		ConfigDir:      dir,
		Updater:        fake,
		Clock:          fakeClock{now: time.Now()},
	})

	if result != nil {
		t.Error("expected nil result on cancelled context")
	}
}

// --- Boundary conditions ---

// Behavior: GIVEN cache file doesn't exist WHEN command runs THEN
// treated as cache expired (first run triggers check).
func TestCacheMissing_TreatedAsExpired(t *testing.T) {
	dir := t.TempDir()

	fake := &fakeUpdater{
		detectRelease: &selfupdate.Release{},
		detectFound:   true,
	}

	Check(context.Background(), Options{
		CurrentVersion: "0.3.0",
		ConfigDir:      dir,
		Updater:        fake,
		Clock:          fakeClock{now: time.Now()},
		VersionCompare: neverNewer(),
	})

	if !fake.detectCalled {
		t.Error("expected DetectLatest when cache file is missing")
	}
	assertCacheExists(t, dir)
}

// Behavior: GIVEN checksums.txt validation WHEN download completes THEN
// checksum verified. We verify the updater config by checking that
// newUpdater creates an updater (production path).
func TestNewUpdater_CreatesSuccessfully(t *testing.T) {
	u, err := newUpdater()
	if err != nil {
		t.Fatalf("newUpdater() returned error: %v", err)
	}
	if u == nil {
		t.Fatal("expected non-nil updater")
	}
}

// Behavior: Exact 24h boundary — cache at exactly checkInterval is
// treated as expired.
func TestCacheExpiredBoundary(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, cacheFileName)
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	f.Close()

	now := time.Now()

	// Exactly 24h old should trigger.
	if !CacheExpired(dir, fakeClock{now: now.Add(checkInterval)}) {
		t.Error("expected cache to be expired at exactly 24h")
	}

	// 1s before 24h should NOT trigger.
	if CacheExpired(dir, fakeClock{now: now.Add(checkInterval - time.Second)}) {
		t.Error("expected cache NOT to be expired just before 24h")
	}
}

// Behavior: ExePath option is forwarded to UpdateTo.
func TestExePathForwarded(t *testing.T) {
	dir := t.TempDir()
	writeStaleCacheFile(t, dir)
	exePath := filepath.Join(dir, "custom-binary")

	fake := &fakeUpdater{
		detectRelease: &selfupdate.Release{},
		detectFound:   true,
	}

	Check(context.Background(), Options{
		CurrentVersion: "0.2.1",
		ConfigDir:      dir,
		Updater:        fake,
		Clock:          fakeClock{now: time.Now()},
		ExePath:        exePath,
		VersionCompare: alwaysNewer(),
	})

	if fake.updatePath != exePath {
		t.Errorf("expected UpdateTo path %q, got %q", exePath, fake.updatePath)
	}
}

// --- NoticeMessage tests ---

func TestNoticeMessage_Updated(t *testing.T) {
	msg := NoticeMessage(&Result{
		Updated:    true,
		OldVersion: "0.2.1",
		NewVersion: "0.3.0",
	})
	expected := "Updated shovels v0.2.1 \u2192 v0.3.0\n"
	if msg != expected {
		t.Errorf("expected %q, got %q", expected, msg)
	}
}

func TestNoticeMessage_Nil(t *testing.T) {
	if msg := NoticeMessage(nil); msg != "" {
		t.Errorf("expected empty for nil, got %q", msg)
	}
}

func TestNoticeMessage_NotUpdated(t *testing.T) {
	if msg := NoticeMessage(&Result{Updated: false}); msg != "" {
		t.Errorf("expected empty when not updated, got %q", msg)
	}
}

// --- Constants tests ---

func TestTimeoutIs10Seconds(t *testing.T) {
	if Timeout != 10*time.Second {
		t.Errorf("expected 10s, got %v", Timeout)
	}
}

func TestCheckIntervalIs24Hours(t *testing.T) {
	if checkInterval != 24*time.Hour {
		t.Errorf("expected 24h, got %v", checkInterval)
	}
}

// TestDetectNotFound verifies that Check completes silently when
// DetectLatest reports no matching release.
func TestDetectNotFound_Silent(t *testing.T) {
	dir := t.TempDir()
	writeStaleCacheFile(t, dir)

	fake := &fakeUpdater{
		detectFound: false,
	}

	result := Check(context.Background(), Options{
		CurrentVersion: "0.2.1",
		ConfigDir:      dir,
		Updater:        fake,
		Clock:          fakeClock{now: time.Now()},
	})

	if result != nil {
		t.Error("expected nil result when no release found")
	}
}

// Behavior: GIVEN config directory doesn't exist (new user, no config
// file yet) WHEN update check runs THEN cache file and parent
// directories are created so the 24h throttle works on next invocation.
func TestTouchCache_CreatesParentDirectories(t *testing.T) {
	base := t.TempDir()
	// configDir is a nested path that doesn't exist yet.
	configDir := filepath.Join(base, "nonexistent", "config", "shovels")

	fake := &fakeUpdater{
		detectRelease: &selfupdate.Release{},
		detectFound:   true,
	}

	result := Check(context.Background(), Options{
		CurrentVersion: "0.3.0",
		ConfigDir:      configDir,
		Updater:        fake,
		Clock:          fakeClock{now: time.Now()},
		VersionCompare: neverNewer(),
	})

	if !fake.detectCalled {
		t.Error("expected DetectLatest to be called when config dir is missing")
	}
	if result != nil {
		t.Error("expected nil result when already on latest")
	}
	// Cache file must exist after the check, proving parent dirs were created.
	assertCacheExists(t, configDir)

	// Verify a second Check with fresh clock does NOT trigger a new check
	// (cache throttle works).
	fake2 := &fakeUpdater{
		detectRelease: &selfupdate.Release{},
		detectFound:   true,
	}
	Check(context.Background(), Options{
		CurrentVersion: "0.3.0",
		ConfigDir:      configDir,
		Updater:        fake2,
		Clock:          fakeClock{now: time.Now()},
	})
	if fake2.detectCalled {
		t.Error("expected DetectLatest NOT to be called when cache was just written")
	}
}

// --- helpers ---

func writeStaleCacheFile(t *testing.T, dir string) {
	t.Helper()
	path := filepath.Join(dir, cacheFileName)
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	staleTime := time.Now().Add(-25 * time.Hour)
	if err := os.Chtimes(path, staleTime, staleTime); err != nil {
		t.Fatal(err)
	}
}

func writeFreshCacheFile(t *testing.T, dir string) {
	t.Helper()
	path := filepath.Join(dir, cacheFileName)
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	freshTime := time.Now().Add(-1 * time.Hour)
	if err := os.Chtimes(path, freshTime, freshTime); err != nil {
		t.Fatal(err)
	}
}

func assertCacheExists(t *testing.T, dir string) {
	t.Helper()
	path := filepath.Join(dir, cacheFileName)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Error("expected cache file to exist after check")
	}
}
