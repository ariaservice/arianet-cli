// Package update tells the user when a newer arianet release is published.
//
// The latest release is looked up on GitHub at most once per CacheTTL and
// remembered in the CLI config directory. The lookup runs in the background
// with a short timeout; it never delays or fails a command, and every error is
// silent. If the lookup has not finished when the command does, nothing is
// printed for that run.
package update

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	// EnvDisable turns the notifier off when set to any non-empty value.
	EnvDisable = "ARIANET_NO_UPDATE_NOTIFIER"

	// LatestReleaseURL is GitHub's latest stable release endpoint; it never
	// returns drafts or prereleases.
	LatestReleaseURL = "https://api.github.com/repos/ariaservice/arianet-cli/releases/latest"

	// CacheTTL is how long a looked-up release is trusted before asking again.
	CacheTTL = 24 * time.Hour

	// CheckTimeout bounds the background lookup.
	CheckTimeout = 2 * time.Second

	cacheFileName = "update-check.json"
)

// Release is the newest stable release as recorded in the cache.
type Release struct {
	Version   string    `json:"latest_version"`
	URL       string    `json:"url"`
	CheckedAt time.Time `json:"checked_at"`
}

// Checker looks up the latest release, going through the on-disk cache.
type Checker struct {
	CacheFile string
	URL       string
	Client    *http.Client
	Now       func() time.Time
}

// CacheFile returns the cache path inside the given config directory.
func CacheFile(configDir string) string {
	return filepath.Join(configDir, cacheFileName)
}

// Latest returns the newest stable release, from the cache when it is
// younger than CacheTTL and from the release API otherwise.
func (c *Checker) Latest(ctx context.Context) (*Release, error) {
	now := c.now()
	if cached, err := c.readCache(); err == nil && Fresh(cached, now) {
		return cached, nil
	}
	rel, err := c.fetch(ctx)
	if err != nil {
		return nil, err
	}
	rel.CheckedAt = now
	_ = c.writeCache(rel)
	return rel, nil
}

// Fresh reports whether a cached release is recent enough to trust.
func Fresh(r *Release, now time.Time) bool {
	if r == nil || r.Version == "" || r.CheckedAt.IsZero() {
		return false
	}
	age := now.Sub(r.CheckedAt)
	return age >= 0 && age < CacheTTL
}

func (c *Checker) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

func (c *Checker) readCache() (*Release, error) {
	b, err := os.ReadFile(c.CacheFile)
	if err != nil {
		return nil, err
	}
	var r Release
	if err := json.Unmarshal(b, &r); err != nil {
		return nil, err
	}
	return &r, nil
}

func (c *Checker) writeCache(r *Release) error {
	dir := filepath.Dir(c.CacheFile)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	b, err := json.Marshal(r)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".update-check-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), c.CacheFile)
}

type githubRelease struct {
	TagName    string `json:"tag_name"`
	HTMLURL    string `json:"html_url"`
	Draft      bool   `json:"draft"`
	Prerelease bool   `json:"prerelease"`
}

func (c *Checker) fetch(ctx context.Context) (*Release, error) {
	url := c.URL
	if url == "" {
		url = LatestReleaseURL
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "arianet-cli-update-check")

	client := c.Client
	if client == nil {
		client = &http.Client{Timeout: CheckTimeout}
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("release lookup: HTTP %d", resp.StatusCode)
	}

	var gr githubRelease
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&gr); err != nil {
		return nil, err
	}
	if gr.Draft || gr.Prerelease {
		return nil, errors.New("latest release is not a stable release")
	}
	v, ok := ParseVersion(gr.TagName)
	if !ok || v.Pre != "" {
		return nil, fmt.Errorf("unexpected release tag %q", gr.TagName)
	}
	return &Release{Version: v.String(), URL: gr.HTMLURL}, nil
}

// Version is a parsed semantic version.
type Version struct {
	Major, Minor, Patch int
	Pre                 string
}

func (v Version) String() string {
	s := fmt.Sprintf("%d.%d.%d", v.Major, v.Minor, v.Patch)
	if v.Pre != "" {
		s += "-" + v.Pre
	}
	return s
}

// ParseVersion parses "1.2.3", "v1.2.3", "1.2.3-rc.1" and "1.2.3+meta".
func ParseVersion(s string) (Version, bool) {
	s = strings.TrimPrefix(strings.TrimSpace(s), "v")
	if i := strings.IndexByte(s, '+'); i >= 0 {
		s = s[:i]
	}
	var v Version
	if i := strings.IndexByte(s, '-'); i >= 0 {
		v.Pre = s[i+1:]
		s = s[:i]
		if v.Pre == "" {
			return Version{}, false
		}
	}
	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return Version{}, false
	}
	nums := make([]int, 3)
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 || p == "" {
			return Version{}, false
		}
		nums[i] = n
	}
	v.Major, v.Minor, v.Patch = nums[0], nums[1], nums[2]
	return v, true
}

// Compare returns -1, 0 or 1 as a is older than, equal to or newer than b.
// A prerelease sorts before its release (1.0.0-rc.1 < 1.0.0).
func Compare(a, b Version) int {
	for _, d := range [][2]int{{a.Major, b.Major}, {a.Minor, b.Minor}, {a.Patch, b.Patch}} {
		if d[0] != d[1] {
			if d[0] < d[1] {
				return -1
			}
			return 1
		}
	}
	switch {
	case a.Pre == b.Pre:
		return 0
	case a.Pre == "":
		return 1
	case b.Pre == "":
		return -1
	case a.Pre < b.Pre:
		return -1
	default:
		return 1
	}
}

// IsNewer reports whether latest is a newer version than current. Anything
// that does not parse is never newer.
func IsNewer(current, latest string) bool {
	c, ok := ParseVersion(current)
	if !ok {
		return false
	}
	l, ok := ParseVersion(latest)
	if !ok {
		return false
	}
	return Compare(l, c) > 0
}

// Conditions are the facts that decide whether the notifier runs at all.
type Conditions struct {
	CurrentVersion string
	Output         string
	CommandPath    string
	StderrIsTTY    bool
	Getenv         func(string) string
}

// Enabled reports whether a release lookup and notice are allowed.
func Enabled(c Conditions) bool {
	getenv := c.Getenv
	if getenv == nil {
		getenv = os.Getenv
	}
	if getenv(EnvDisable) != "" || getenv("CI") != "" {
		return false
	}
	if !c.StderrIsTTY {
		return false
	}
	if strings.EqualFold(c.Output, "json") {
		return false
	}
	if _, ok := ParseVersion(c.CurrentVersion); !ok {
		return false // dev or unversioned build
	}
	for _, f := range strings.Fields(c.CommandPath) {
		switch f {
		case "completion", "__complete", "__completeNoDesc":
			return false
		}
	}
	return true
}

// IsTerminal reports whether f is a character device (a terminal).
func IsTerminal(f *os.File) bool {
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

// InstallMethod is how this binary was installed.
type InstallMethod int

const (
	InstallScript InstallMethod = iota
	InstallHomebrew
	InstallApt
)

var homebrewPrefixes = []string{"/opt/homebrew/", "/usr/local/Cellar/", "/home/linuxbrew/"}

// DetectInstall works out the install method from the resolved executable
// path. dpkgInfoDir is normally /var/lib/dpkg/info.
func DetectInstall(exePath, dpkgInfoDir string) InstallMethod {
	for _, p := range homebrewPrefixes {
		if strings.HasPrefix(exePath, p) {
			return InstallHomebrew
		}
	}
	if strings.Contains(exePath, "/Cellar/arianet/") {
		return InstallHomebrew
	}
	if ownedByDpkg(exePath, filepath.Join(dpkgInfoDir, "arianet.list")) {
		return InstallApt
	}
	return InstallScript
}

func ownedByDpkg(exePath, listFile string) bool {
	f, err := os.Open(listFile)
	if err != nil {
		return false
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if strings.TrimSpace(sc.Text()) == exePath {
			return true
		}
	}
	return false
}

// ExecutablePath returns this binary's path with symlinks resolved.
func ExecutablePath() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		return resolved
	}
	return exe
}

// UpgradeCommand is the command that upgrades an installation.
func UpgradeCommand(m InstallMethod) string {
	switch m {
	case InstallHomebrew:
		return "brew upgrade ariaservice/arianet/arianet"
	case InstallApt:
		return "sudo apt update && sudo apt install --only-upgrade arianet"
	default:
		return "curl -fsSL https://raw.githubusercontent.com/ariaservice/arianet-cli/main/scripts/install.sh | sh"
	}
}

// ReleaseURL is the release notes page for a version.
func ReleaseURL(version string) string {
	return "https://github.com/ariaservice/arianet-cli/releases/tag/v" + strings.TrimPrefix(version, "v")
}

// Notice is the text printed when latest is newer than current.
func Notice(current, latest string, m InstallMethod) string {
	cur := strings.TrimPrefix(current, "v")
	lat := strings.TrimPrefix(latest, "v")
	return fmt.Sprintf("\nA new release of arianet is available: %s → %s\nTo upgrade, run: %s\n%s\n",
		cur, lat, UpgradeCommand(m), ReleaseURL(lat))
}

// Background is a lookup started at command start.
type Background struct {
	current string
	done    chan *Release
}

// Start begins a lookup that gives up after CheckTimeout. A fresh cache entry
// is a small local file and is read right away, so a fast command still sees
// it; only a stale or missing cache goes to the network in the background.
func Start(c *Checker, current string) *Background {
	b := &Background{current: current, done: make(chan *Release, 1)}
	if cached, err := c.readCache(); err == nil && Fresh(cached, c.now()) {
		b.done <- cached
		return b
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), CheckTimeout)
		defer cancel()
		rel, err := c.Latest(ctx)
		if err != nil {
			rel = nil
		}
		b.done <- rel
	}()
	return b
}

// Ready returns the newer release if the lookup has already finished and
// found one; it never waits.
func (b *Background) Ready() *Release {
	if b == nil {
		return nil
	}
	select {
	case rel := <-b.done:
		if rel != nil && IsNewer(b.current, rel.Version) {
			return rel
		}
	default:
	}
	return nil
}
