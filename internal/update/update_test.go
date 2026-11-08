package update

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestParseAndCompare(t *testing.T) {
	cases := []struct {
		current, latest string
		newer           bool
	}{
		{"0.3.1", "0.4.0", true},
		{"0.3.1", "v0.4.0", true},
		{"v0.3.1", "0.3.2", true},
		{"0.3.1", "1.0.0", true},
		{"0.9.0", "0.10.0", true},
		{"0.4.0", "0.4.0", false},
		{"0.4.0", "0.3.9", false},
		{"0.4.0-rc.1", "0.4.0", true},
		{"0.4.0", "0.4.1-rc.1", true},
		{"0.3.1-3-gabc1234", "0.3.1", true},
		{"0.3.1+meta", "0.3.1", false},
		{"dev", "0.4.0", false},
		{"0.3.1", "garbage", false},
		{"0.3", "0.4.0", false},
	}
	for _, c := range cases {
		if got := IsNewer(c.current, c.latest); got != c.newer {
			t.Errorf("IsNewer(%q, %q) = %v, want %v", c.current, c.latest, got, c.newer)
		}
	}
	if _, ok := ParseVersion("1.2.x"); ok {
		t.Error("1.2.x parsed")
	}
	if v, ok := ParseVersion("v1.2.3-rc.1+build"); !ok || v.String() != "1.2.3-rc.1" {
		t.Errorf("ParseVersion = %v %v", v, ok)
	}
}

func TestFresh(t *testing.T) {
	now := time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name string
		r    *Release
		want bool
	}{
		{"nil", nil, false},
		{"just checked", &Release{Version: "0.4.0", CheckedAt: now.Add(-time.Minute)}, true},
		{"23h59m", &Release{Version: "0.4.0", CheckedAt: now.Add(-CacheTTL + time.Minute)}, true},
		{"24h", &Release{Version: "0.4.0", CheckedAt: now.Add(-CacheTTL)}, false},
		{"future clock", &Release{Version: "0.4.0", CheckedAt: now.Add(time.Hour)}, false},
		{"no version", &Release{CheckedAt: now}, false},
	}
	for _, c := range cases {
		if got := Fresh(c.r, now); got != c.want {
			t.Errorf("%s: Fresh = %v, want %v", c.name, got, c.want)
		}
	}
}

func releaseServer(t *testing.T, body map[string]any, hits *int32) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(hits, 1)
		if r.Header.Get("Authorization") != "" {
			t.Error("release lookup must be unauthenticated")
		}
		_ = json.NewEncoder(w).Encode(body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestLatestUsesCacheFor24Hours(t *testing.T) {
	var hits int32
	srv := releaseServer(t, map[string]any{"tag_name": "v0.4.0", "html_url": "https://example/v0.4.0"}, &hits)
	now := time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)
	c := &Checker{CacheFile: filepath.Join(t.TempDir(), "sub", "update-check.json"), URL: srv.URL, Now: func() time.Time { return now }}

	rel, err := c.Latest(context.Background())
	if err != nil || rel.Version != "0.4.0" {
		t.Fatalf("Latest = %v, %v", rel, err)
	}
	now = now.Add(23 * time.Hour)
	if _, err := c.Latest(context.Background()); err != nil {
		t.Fatal(err)
	}
	if hits != 1 {
		t.Fatalf("expected 1 API call within 24h, got %d", hits)
	}
	now = now.Add(2 * time.Hour)
	if _, err := c.Latest(context.Background()); err != nil {
		t.Fatal(err)
	}
	if hits != 2 {
		t.Fatalf("expected a refresh after 24h, got %d calls", hits)
	}
	b, _ := os.ReadFile(c.CacheFile)
	if !strings.Contains(string(b), `"latest_version":"0.4.0"`) || !strings.Contains(string(b), "checked_at") {
		t.Fatalf("cache content: %s", b)
	}
}

func TestLatestRejectsPrereleaseAndErrors(t *testing.T) {
	for name, body := range map[string]map[string]any{
		"prerelease flag": {"tag_name": "v0.5.0", "prerelease": true},
		"draft":           {"tag_name": "v0.5.0", "draft": true},
		"prerelease tag":  {"tag_name": "v0.5.0-rc.1"},
		"bad tag":         {"tag_name": "latest"},
	} {
		var hits int32
		srv := releaseServer(t, body, &hits)
		c := &Checker{CacheFile: filepath.Join(t.TempDir(), "c.json"), URL: srv.URL}
		if rel, err := c.Latest(context.Background()); err == nil {
			t.Errorf("%s: expected error, got %v", name, rel)
		}
		if _, err := os.Stat(c.CacheFile); err == nil {
			t.Errorf("%s: failed lookup must not be cached", name)
		}
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "rate limited", http.StatusForbidden)
	}))
	defer srv.Close()
	c := &Checker{CacheFile: filepath.Join(t.TempDir(), "c.json"), URL: srv.URL}
	if _, err := c.Latest(context.Background()); err == nil {
		t.Error("expected error on HTTP 403")
	}
}

func TestBackgroundNeverWaits(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"tag_name": "v0.4.0"})
	}))
	defer srv.Close()
	defer close(release)

	c := &Checker{CacheFile: filepath.Join(t.TempDir(), "c.json"), URL: srv.URL}
	b := Start(c, "0.3.1")
	start := time.Now()
	if rel := b.Ready(); rel != nil {
		t.Fatalf("unfinished lookup returned %v", rel)
	}
	if time.Since(start) > 100*time.Millisecond {
		t.Fatal("Ready waited for the lookup")
	}
}

func TestBackgroundReportsNewer(t *testing.T) {
	var hits int32
	srv := releaseServer(t, map[string]any{"tag_name": "v0.4.0"}, &hits)
	for current, want := range map[string]bool{"0.3.1": true, "0.4.0": false} {
		c := &Checker{CacheFile: filepath.Join(t.TempDir(), "c.json"), URL: srv.URL}
		b := Start(c, current)
		var rel *Release
		deadline := time.Now().Add(CheckTimeout + time.Second)
		for time.Now().Before(deadline) {
			select {
			case r := <-b.done:
				b.done <- r
			case <-time.After(10 * time.Millisecond):
				continue
			}
			rel = b.Ready()
			break
		}
		if (rel != nil) != want {
			t.Errorf("current %s: Ready = %v, want newer=%v", current, rel, want)
		}
	}
	var nilBackground *Background
	if nilBackground.Ready() != nil {
		t.Error("nil Background must report nothing")
	}
}

func TestBackgroundFreshCacheIsReadyAtOnce(t *testing.T) {
	c := &Checker{CacheFile: filepath.Join(t.TempDir(), "c.json"), URL: "http://127.0.0.1:1/unreachable"}
	if err := c.writeCache(&Release{Version: "0.4.0", CheckedAt: time.Now().Add(-time.Hour)}); err != nil {
		t.Fatal(err)
	}
	rel := Start(c, "0.3.1").Ready()
	if rel == nil || rel.Version != "0.4.0" {
		t.Fatalf("fresh cache must be ready immediately, got %v", rel)
	}
}

func TestEnabled(t *testing.T) {
	env := func(m map[string]string) func(string) string {
		return func(k string) string { return m[k] }
	}
	base := Conditions{CurrentVersion: "0.3.1", Output: "table", CommandPath: "arianet region list", StderrIsTTY: true, Getenv: env(nil)}
	if !Enabled(base) {
		t.Fatal("base case should be enabled")
	}
	mods := map[string]func(*Conditions){
		"json output":     func(c *Conditions) { c.Output = "json" },
		"JSON output":     func(c *Conditions) { c.Output = "JSON" },
		"not a tty":       func(c *Conditions) { c.StderrIsTTY = false },
		"CI":              func(c *Conditions) { c.Getenv = env(map[string]string{"CI": "true"}) },
		"opt out":         func(c *Conditions) { c.Getenv = env(map[string]string{EnvDisable: "1"}) },
		"dev build":       func(c *Conditions) { c.CurrentVersion = "dev" },
		"empty version":   func(c *Conditions) { c.CurrentVersion = "" },
		"completion":      func(c *Conditions) { c.CommandPath = "arianet completion zsh" },
		"shell completer": func(c *Conditions) { c.CommandPath = "arianet __complete region" },
		"upgrade":         func(c *Conditions) { c.CommandPath = "arianet upgrade" },
		"update alias":    func(c *Conditions) { c.CommandPath = "arianet update" },
	}
	for name, mod := range mods {
		c := base
		mod(&c)
		if Enabled(c) {
			t.Errorf("%s: should be disabled", name)
		}
	}
	v := base
	v.CommandPath = "arianet version"
	if !Enabled(v) {
		t.Error("version command may show the notice")
	}
}

func TestDetectInstall(t *testing.T) {
	dpkg := t.TempDir()
	if err := os.WriteFile(filepath.Join(dpkg, "arianet.list"), []byte("/.\n/usr\n/usr/bin\n/usr/bin/arianet\n"), 0644); err != nil {
		t.Fatal(err)
	}
	empty := t.TempDir()
	cases := []struct {
		path, dpkgDir string
		want          InstallMethod
	}{
		{"/opt/homebrew/Cellar/arianet/0.3.1/bin/arianet", empty, InstallHomebrew},
		{"/usr/local/Cellar/arianet/0.3.1/bin/arianet", empty, InstallHomebrew},
		{"/home/linuxbrew/.linuxbrew/Cellar/arianet/0.3.1/bin/arianet", empty, InstallHomebrew},
		{"/custom/brew/Cellar/arianet/0.3.1/bin/arianet", empty, InstallHomebrew},
		{"/usr/bin/arianet", dpkg, InstallApt},
		{"/usr/bin/arianet", empty, InstallScript},
		{"/usr/local/bin/arianet", dpkg, InstallScript},
		{"/home/me/bin/arianet", empty, InstallScript},
	}
	for _, c := range cases {
		if got := DetectInstall(c.path, c.dpkgDir); got != c.want {
			t.Errorf("DetectInstall(%q) = %v, want %v", c.path, got, c.want)
		}
	}
}

func TestNotice(t *testing.T) {
	got := Notice("0.3.1", "v0.4.0")
	want := "\nA new release of arianet is available: 0.3.1 → 0.4.0\n" +
		"To upgrade, run: arianet upgrade\n" +
		"https://github.com/ariaservice/arianet-cli/releases/tag/v0.4.0\n"
	if got != want {
		t.Errorf("Notice =\n%q\nwant\n%q", got, want)
	}
}

func TestUpgradeCommandPerMethod(t *testing.T) {
	cases := map[InstallMethod]string{
		InstallHomebrew: "brew upgrade ariaservice/arianet/arianet",
		InstallApt:      "sudo apt-get update && sudo apt-get install --only-upgrade -y arianet",
		InstallScript:   "arianet upgrade",
		InstallRpm:      "arianet upgrade",
	}
	for m, want := range cases {
		if got := UpgradeCommand(m); got != want {
			t.Errorf("UpgradeCommand(%v) = %q, want %q", m, got, want)
		}
	}
}
