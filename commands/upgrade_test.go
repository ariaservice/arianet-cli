package commands

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/ariaservice/arianet-cli/internal/update"
)

// fakeRelease serves a GitHub-like release API plus real archives and a
// checksums.txt for a few versions and platforms.
type fakeRelease struct {
	srv      *httptest.Server
	latest   string
	files    map[string][]byte // download path -> body
	breakSum string            // asset whose checksum line is wrong
}

func tarGz(t *testing.T, name, body string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, f := range [][2]string{{"README.md", "readme"}, {name, body}} {
		tw.WriteHeader(&tar.Header{Name: f[0], Mode: 0o755, Size: int64(len(f[1])), Typeflag: tar.TypeReg})
		tw.Write([]byte(f[1]))
	}
	tw.Close()
	gz.Close()
	return buf.Bytes()
}

func zipOf(t *testing.T, name, body string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, _ := zw.Create(name)
	w.Write([]byte(body))
	zw.Close()
	return buf.Bytes()
}

func newFakeRelease(t *testing.T, latest string, versions ...string) *fakeRelease {
	t.Helper()
	f := &fakeRelease{latest: latest, files: map[string][]byte{}}
	platforms := [][2]string{{"linux", "amd64"}, {"linux", "arm64"}, {"darwin", "arm64"}, {"darwin", "amd64"}, {"windows", "amd64"}}
	for _, v := range versions {
		for _, p := range platforms {
			name := update.ArchiveName(v, p[0], p[1])
			body := fmt.Sprintf("arianet %s %s/%s", v, p[0], p[1])
			if p[0] == "windows" {
				f.files[v+"/"+name] = zipOf(t, "arianet.exe", body)
			} else {
				f.files[v+"/"+name] = tarGz(t, "arianet", body)
			}
		}
		f.files[v+"/"+update.PackageName(v, "amd64", "rpm")] = []byte("rpm " + v)
		f.files[v+"/"+update.SignatureAsset] = []byte("{}")
	}
	f.srv = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeRelease) checksums(v string) []byte {
	var b strings.Builder
	for k, body := range f.files {
		if !strings.HasPrefix(k, v+"/") || strings.HasSuffix(k, ".sigstore.json") {
			continue
		}
		name := strings.TrimPrefix(k, v+"/")
		s := sha256.Sum256(body)
		sum := hex.EncodeToString(s[:])
		if name == f.breakSum {
			sum = strings.Repeat("0", 64)
		}
		fmt.Fprintf(&b, "%s  %s\n", sum, name)
	}
	return []byte(b.String())
}

func (f *fakeRelease) release(v string) map[string]any {
	var assets []map[string]string
	for k := range f.files {
		if strings.HasPrefix(k, v+"/") {
			name := strings.TrimPrefix(k, v+"/")
			assets = append(assets, map[string]string{"name": name, "browser_download_url": f.srv.URL + "/download/" + k})
		}
	}
	assets = append(assets, map[string]string{"name": update.ChecksumsAsset, "browser_download_url": f.srv.URL + "/download/" + v + "/" + update.ChecksumsAsset})
	return map[string]any{"tag_name": "v" + v, "html_url": "https://example/v" + v, "assets": assets}
}

func (f *fakeRelease) serve(w http.ResponseWriter, r *http.Request) {
	p := r.URL.Path
	switch {
	case p == "/releases/latest":
		json.NewEncoder(w).Encode(f.release(f.latest))
	case strings.HasPrefix(p, "/releases/tags/v"):
		v := strings.TrimPrefix(p, "/releases/tags/v")
		if _, ok := f.files[v+"/"+update.SignatureAsset]; !ok {
			http.NotFound(w, r)
			return
		}
		json.NewEncoder(w).Encode(f.release(v))
	case strings.HasSuffix(p, "/"+update.ChecksumsAsset):
		v := strings.Split(strings.TrimPrefix(p, "/download/"), "/")[0]
		w.Write(f.checksums(v))
	case strings.HasPrefix(p, "/download/"):
		body, ok := f.files[strings.TrimPrefix(p, "/download/")]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Write(body)
	default:
		http.NotFound(w, r)
	}
}

type harness struct {
	u       *upgrader
	out     *bytes.Buffer
	errOut  *bytes.Buffer
	ran     [][]string
	checked []string
	target  string
}

func newHarness(t *testing.T, f *fakeRelease, goos, goarch string, method update.InstallMethod) *harness {
	t.Helper()
	h := &harness{out: &bytes.Buffer{}, errOut: &bytes.Buffer{}}
	exeName := update.BinaryName(goos)
	h.target = filepath.Join(t.TempDir(), exeName)
	if err := os.WriteFile(h.target, []byte("arianet 0.4.0 old"), 0o755); err != nil {
		t.Fatal(err)
	}
	h.u = &upgrader{
		out:      h.out,
		errOut:   h.errOut,
		current:  "0.4.0",
		goos:     goos,
		goarch:   goarch,
		exePath:  h.target,
		method:   method,
		releases: &update.Releases{API: f.srv.URL + "/releases", Client: f.srv.Client()},
		cache:    &update.Checker{CacheFile: filepath.Join(t.TempDir(), "update-check.json")},
		confirm:  func(string) bool { t.Error("unexpected prompt"); return false },
		runCmd: func(name string, args ...string) error {
			h.ran = append(h.ran, append([]string{name}, args...))
			return nil
		},
		selfCheck: func(path, version string) error {
			b, _ := os.ReadFile(path)
			if !strings.Contains(string(b), version) {
				return fmt.Errorf("self-check: %q lacks %s", b, version)
			}
			h.checked = append(h.checked, version)
			return nil
		},
		tempDir: t.TempDir(),
	}
	return h
}

func (h *harness) cachedVersion(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(h.u.cache.CacheFile)
	if err != nil {
		return ""
	}
	var r update.Release
	json.Unmarshal(b, &r)
	return r.Version
}

func (h *harness) targetBody(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(h.target)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func exitCode(err error) int {
	if err == nil {
		return 0
	}
	var ee *exitError
	if errors.As(err, &ee) {
		return ee.code
	}
	return 1
}

func TestUpgradeUpToDate(t *testing.T) {
	f := newFakeRelease(t, "0.4.0", "0.4.0")
	for _, check := range []bool{false, true} {
		h := newHarness(t, f, "linux", "amd64", update.InstallScript)
		err := h.u.run(context.Background(), upgradeOptions{check: check})
		if err != nil {
			t.Fatalf("check=%v: %v", check, err)
		}
		if got := h.out.String(); got != "arianet is up to date (v0.4.0)\n" {
			t.Errorf("check=%v: out = %q", check, got)
		}
		if h.targetBody(t) != "arianet 0.4.0 old" || len(h.ran) != 0 {
			t.Error("nothing should change")
		}
		if h.cachedVersion(t) != "0.4.0" {
			t.Error("lookup should refresh the cache")
		}
	}
}

func TestUpgradeAheadOfLatest(t *testing.T) {
	f := newFakeRelease(t, "0.4.0", "0.4.0")
	h := newHarness(t, f, "linux", "amd64", update.InstallScript)
	h.u.current = "0.5.0-rc.1"
	if err := h.u.run(context.Background(), upgradeOptions{}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(h.out.String(), "up to date (v0.5.0-rc.1; the latest release is v0.4.0)") {
		t.Errorf("out = %q", h.out)
	}
}

func TestUpgradeCheckReportsNewer(t *testing.T) {
	f := newFakeRelease(t, "0.5.0", "0.5.0")
	h := newHarness(t, f, "darwin", "arm64", update.InstallHomebrew)
	err := h.u.run(context.Background(), upgradeOptions{check: true})
	if exitCode(err) != exitCodeAvailable || err.Error() != "" {
		t.Fatalf("err = %v (code %d)", err, exitCode(err))
	}
	for _, want := range []string{"available: 0.4.0 → 0.5.0", "Installed with: Homebrew", "To upgrade, run: arianet upgrade", "releases/tag/v0.5.0"} {
		if !strings.Contains(h.out.String(), want) {
			t.Errorf("out lacks %q:\n%s", want, h.out)
		}
	}
	if len(h.ran) != 0 || h.targetBody(t) != "arianet 0.4.0 old" {
		t.Error("--check must not upgrade")
	}
}

func TestUpgradeDevBuild(t *testing.T) {
	f := newFakeRelease(t, "0.5.0", "0.5.0")
	h := newHarness(t, f, "linux", "amd64", update.InstallScript)
	h.u.current = "dev"
	if err := h.u.run(context.Background(), upgradeOptions{yes: true}); err == nil || !strings.Contains(err.Error(), "development build") {
		t.Errorf("err = %v", err)
	}
}

func TestUpgradeStandalonePerPlatform(t *testing.T) {
	f := newFakeRelease(t, "0.5.0", "0.4.0", "0.5.0")
	for _, p := range [][2]string{{"linux", "amd64"}, {"linux", "arm64"}, {"darwin", "arm64"}, {"darwin", "amd64"}, {"windows", "amd64"}} {
		h := newHarness(t, f, p[0], p[1], update.InstallScript)
		if err := h.u.run(context.Background(), upgradeOptions{yes: true}); err != nil {
			t.Fatalf("%s/%s: %v", p[0], p[1], err)
		}
		want := fmt.Sprintf("arianet 0.5.0 %s/%s", p[0], p[1])
		if got := h.targetBody(t); got != want {
			t.Errorf("%s/%s: target = %q, want %q", p[0], p[1], got, want)
		}
		if !strings.Contains(h.out.String(), "Upgraded arianet 0.4.0 → 0.5.0") {
			t.Errorf("%s/%s: out = %q", p[0], p[1], h.out)
		}
		if !strings.Contains(h.errOut.String(), "checksum OK") {
			t.Errorf("%s/%s: errOut = %q", p[0], p[1], h.errOut)
		}
		if len(h.checked) != 1 || h.checked[0] != "0.5.0" {
			t.Errorf("%s/%s: self-check = %v", p[0], p[1], h.checked)
		}
		if h.cachedVersion(t) != "0.5.0" {
			t.Errorf("%s/%s: cache not refreshed", p[0], p[1])
		}
		_, oldErr := os.Stat(update.OldPath(h.target))
		if p[0] == "windows" && oldErr != nil {
			t.Errorf("windows: previous exe should be moved aside to .old")
		}
		if p[0] != "windows" && oldErr == nil {
			t.Errorf("%s: no .old expected", p[0])
		}
		if left, _ := os.ReadDir(h.u.tempDir); len(left) != 0 {
			t.Errorf("%s/%s: temp files left: %v", p[0], p[1], left)
		}
	}
}

func TestUpgradeNoBuildForPlatform(t *testing.T) {
	f := newFakeRelease(t, "0.5.0", "0.5.0")
	for _, p := range [][2]string{{"freebsd", "amd64"}, {"linux", "386"}, {"linux", "arm"}, {"windows", "arm64"}} {
		h := newHarness(t, f, p[0], p[1], update.InstallScript)
		err := h.u.run(context.Background(), upgradeOptions{yes: true})
		if !errors.Is(err, update.ErrNoBuild) || !strings.Contains(err.Error(), p[0]+"/"+p[1]) {
			t.Errorf("%s/%s: err = %v", p[0], p[1], err)
		}
		if h.targetBody(t) != "arianet 0.4.0 old" {
			t.Errorf("%s/%s: target changed", p[0], p[1])
		}
	}
}

func TestUpgradeBadChecksumRefuses(t *testing.T) {
	f := newFakeRelease(t, "0.5.0", "0.5.0")
	for _, goos := range []string{"linux", "windows"} {
		f.breakSum = update.ArchiveName("0.5.0", goos, "amd64")
		h := newHarness(t, f, goos, "amd64", update.InstallScript)
		err := h.u.run(context.Background(), upgradeOptions{yes: true})
		if err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
			t.Errorf("%s: err = %v", goos, err)
		}
		if h.targetBody(t) != "arianet 0.4.0 old" {
			t.Errorf("%s: target changed", goos)
		}
		if h.cachedVersion(t) == "0.5.0" && strings.Contains(h.out.String(), "Upgraded") {
			t.Errorf("%s: must not report success", goos)
		}
	}
}

func TestUpgradeSignature(t *testing.T) {
	f := newFakeRelease(t, "0.5.0", "0.5.0")
	h := newHarness(t, f, "linux", "amd64", update.InstallScript)
	var verified []string
	h.u.verifySignature = func(checksums, bundle string) error {
		verified = append(verified, filepath.Base(bundle))
		return errors.New("no matching signature")
	}
	err := h.u.run(context.Background(), upgradeOptions{yes: true})
	if err == nil || !strings.Contains(err.Error(), "signature verification") || len(verified) != 1 {
		t.Fatalf("err = %v, verified = %v", err, verified)
	}
	if h.targetBody(t) != "arianet 0.4.0 old" {
		t.Error("target changed")
	}

	h = newHarness(t, f, "linux", "amd64", update.InstallScript)
	h.u.verifySignature = func(string, string) error { return nil }
	if err := h.u.run(context.Background(), upgradeOptions{yes: true}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(h.errOut.String(), "signature OK") {
		t.Errorf("errOut = %q", h.errOut)
	}
}

func TestUpgradeSelfCheckFailureKeepsOld(t *testing.T) {
	f := newFakeRelease(t, "0.5.0", "0.5.0")
	h := newHarness(t, f, "linux", "amd64", update.InstallScript)
	h.u.selfCheck = func(string, string) error { return errors.New("exec format error") }
	if err := h.u.run(context.Background(), upgradeOptions{yes: true}); err == nil || !strings.Contains(err.Error(), "unchanged") {
		t.Fatalf("err = %v", err)
	}
	if h.targetBody(t) != "arianet 0.4.0 old" {
		t.Error("target changed")
	}
}

func TestUpgradeVersionTargeting(t *testing.T) {
	f := newFakeRelease(t, "0.6.0", "0.3.0", "0.5.0", "0.6.0")

	h := newHarness(t, f, "linux", "amd64", update.InstallScript)
	if err := h.u.run(context.Background(), upgradeOptions{yes: true, version: "v0.5.0"}); err != nil {
		t.Fatal(err)
	}
	if h.targetBody(t) != "arianet 0.5.0 linux/amd64" {
		t.Errorf("target = %q", h.targetBody(t))
	}

	h = newHarness(t, f, "linux", "amd64", update.InstallScript)
	err := h.u.run(context.Background(), upgradeOptions{yes: true, version: "0.3.0"})
	if err == nil || !strings.Contains(err.Error(), "--allow-downgrade") {
		t.Fatalf("downgrade without flag: %v", err)
	}
	if h.targetBody(t) != "arianet 0.4.0 old" {
		t.Error("target changed")
	}

	if err := h.u.run(context.Background(), upgradeOptions{yes: true, version: "0.3.0", allowDowngrade: true}); err != nil {
		t.Fatal(err)
	}
	if h.targetBody(t) != "arianet 0.3.0 linux/amd64" {
		t.Errorf("target = %q", h.targetBody(t))
	}

	h = newHarness(t, f, "linux", "amd64", update.InstallScript)
	if err := h.u.run(context.Background(), upgradeOptions{yes: true, version: "v0.4.0"}); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("missing release: %v", err)
	}
	if err := h.u.run(context.Background(), upgradeOptions{yes: true, version: "newest"}); err == nil || !strings.Contains(err.Error(), "invalid version") {
		t.Errorf("invalid version: %v", err)
	}

	h = newHarness(t, f, "linux", "amd64", update.InstallScript)
	if err := h.u.run(context.Background(), upgradeOptions{check: true, version: "0.5.0"}); exitCode(err) != exitCodeAvailable {
		t.Errorf("check --version: %v", err)
	}
	if !strings.Contains(h.out.String(), "arianet upgrade --version v0.5.0") {
		t.Errorf("out = %q", h.out)
	}
	if h.cachedVersion(t) != "" {
		t.Error("a --version lookup must not overwrite the latest-release cache")
	}
}

func TestUpgradeHomebrew(t *testing.T) {
	f := newFakeRelease(t, "0.5.0", "0.5.0")
	h := newHarness(t, f, "darwin", "arm64", update.InstallHomebrew)
	if err := h.u.run(context.Background(), upgradeOptions{yes: true}); err != nil {
		t.Fatal(err)
	}
	if len(h.ran) != 1 || strings.Join(h.ran[0], " ") != "brew upgrade ariaservice/arianet/arianet" {
		t.Errorf("ran = %v", h.ran)
	}
	if h.cachedVersion(t) != "0.5.0" || !strings.Contains(h.out.String(), "0.4.0 → 0.5.0") {
		t.Errorf("out = %q", h.out)
	}

	h = newHarness(t, f, "linux", "amd64", update.InstallHomebrew)
	h.u.runCmd = func(string, ...string) error { return errors.New("exit status 1") }
	if err := h.u.run(context.Background(), upgradeOptions{yes: true}); err == nil || !strings.Contains(err.Error(), "brew upgrade failed") {
		t.Errorf("brew failure: %v", err)
	}

	h = newHarness(t, f, "darwin", "arm64", update.InstallHomebrew)
	if err := h.u.run(context.Background(), upgradeOptions{yes: true, version: "0.5.0"}); err == nil || !strings.Contains(err.Error(), "Homebrew") {
		t.Errorf("--version with brew: %v", err)
	}
}

func TestUpgradeApt(t *testing.T) {
	f := newFakeRelease(t, "0.5.0", "0.5.0")

	h := newHarness(t, f, "linux", "amd64", update.InstallApt)
	err := h.u.run(context.Background(), upgradeOptions{yes: true})
	if exitCode(err) != 1 || len(h.ran) != 0 {
		t.Fatalf("non-TTY apt: err = %v, ran = %v", err, h.ran)
	}
	if !strings.Contains(h.errOut.String(), update.AptUpgradeCommand) {
		t.Errorf("errOut = %q", h.errOut)
	}

	h = newHarness(t, f, "linux", "arm64", update.InstallApt)
	h.u.interactive = true
	prompted := ""
	h.u.confirm = func(p string) bool { prompted = p; return true }
	if err := h.u.run(context.Background(), upgradeOptions{}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(prompted, "0.4.0 → 0.5.0") {
		t.Errorf("prompt = %q", prompted)
	}
	want := []string{"sudo apt-get update", "sudo apt-get install --only-upgrade -y arianet"}
	if len(h.ran) != 2 || strings.Join(h.ran[0], " ") != want[0] || strings.Join(h.ran[1], " ") != want[1] {
		t.Errorf("ran = %v", h.ran)
	}
}

func TestUpgradeRpm(t *testing.T) {
	f := newFakeRelease(t, "0.5.0", "0.5.0")
	h := newHarness(t, f, "linux", "amd64", update.InstallRpm)
	if err := h.u.run(context.Background(), upgradeOptions{yes: true}); exitCode(err) != 1 || len(h.ran) != 0 {
		t.Fatalf("non-TTY rpm: %v", err)
	}
	if !strings.Contains(h.errOut.String(), "sudo rpm -U "+f.srv.URL) {
		t.Errorf("errOut = %q", h.errOut)
	}

	h = newHarness(t, f, "linux", "amd64", update.InstallRpm)
	h.u.interactive = true
	var pkgBody string
	h.u.runCmd = func(name string, args ...string) error {
		h.ran = append(h.ran, append([]string{name}, args...))
		b, _ := os.ReadFile(args[len(args)-1])
		pkgBody = string(b)
		return nil
	}
	if err := h.u.run(context.Background(), upgradeOptions{yes: true}); err != nil {
		t.Fatal(err)
	}
	if len(h.ran) != 1 || strings.Join(h.ran[0][:3], " ") != "sudo rpm -U" || pkgBody != "rpm 0.5.0" {
		t.Errorf("ran = %v, pkg = %q", h.ran, pkgBody)
	}
}

func TestUpgradePrompting(t *testing.T) {
	f := newFakeRelease(t, "0.5.0", "0.5.0")

	h := newHarness(t, f, "linux", "amd64", update.InstallScript)
	if err := h.u.run(context.Background(), upgradeOptions{}); err == nil || !strings.Contains(err.Error(), "--yes") {
		t.Errorf("non-TTY without --yes: %v", err)
	}
	if h.targetBody(t) != "arianet 0.4.0 old" {
		t.Error("target changed")
	}

	h = newHarness(t, f, "linux", "amd64", update.InstallScript)
	h.u.interactive = true
	h.u.confirm = func(string) bool { return false }
	if err := h.u.run(context.Background(), upgradeOptions{}); err == nil || !strings.Contains(err.Error(), "cancelled") {
		t.Errorf("declined: %v", err)
	}
	if h.targetBody(t) != "arianet 0.4.0 old" {
		t.Error("target changed")
	}

	h = newHarness(t, f, "linux", "amd64", update.InstallScript)
	h.u.interactive = true
	h.u.confirm = func(string) bool { return true }
	if err := h.u.run(context.Background(), upgradeOptions{}); err != nil {
		t.Fatal(err)
	}
	if h.targetBody(t) != "arianet 0.5.0 linux/amd64" {
		t.Error("not upgraded")
	}
}

func TestUpgradeNeedsElevation(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("needs a non-root Unix user")
	}
	f := newFakeRelease(t, "0.5.0", "0.5.0")
	for goos, want := range map[string]string{"linux": "sudo arianet upgrade", "windows": "Administrator"} {
		h := newHarness(t, f, goos, "amd64", update.InstallScript)
		dir := filepath.Dir(h.target)
		os.Chmod(dir, 0o555)
		err := h.u.run(context.Background(), upgradeOptions{yes: true})
		os.Chmod(dir, 0o755)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: err = %v", goos, err)
		}
		if h.targetBody(t) != "arianet 0.4.0 old" {
			t.Errorf("%s: target changed", goos)
		}
	}
}

func TestUpgradeCommandRegistered(t *testing.T) {
	c, _, err := rootCmd.Find([]string{"update"})
	if err != nil || c.Name() != "upgrade" {
		t.Fatalf("update alias: %v %v", c, err)
	}
	for _, flag := range []string{"check", "yes", "version", "allow-downgrade"} {
		if c.Flags().Lookup(flag) == nil {
			t.Errorf("missing --%s", flag)
		}
	}
	if c.Flags().ShorthandLookup("y") == nil {
		t.Error("missing -y")
	}
}
