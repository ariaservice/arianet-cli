package update

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
)

func TestDetectPerOS(t *testing.T) {
	dpkg := t.TempDir()
	if err := os.WriteFile(filepath.Join(dpkg, "arianet.list"), []byte("/usr/bin/arianet\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rpmYes := func(string) bool { return true }
	cases := []struct {
		name string
		env  InstallEnv
		want InstallMethod
	}{
		{"macOS arm brew", InstallEnv{GOOS: "darwin", ExePath: "/opt/homebrew/Cellar/arianet/0.4.0/bin/arianet"}, InstallHomebrew},
		{"macOS intel brew", InstallEnv{GOOS: "darwin", ExePath: "/usr/local/Cellar/arianet/0.4.0/bin/arianet"}, InstallHomebrew},
		{"macOS install.sh", InstallEnv{GOOS: "darwin", ExePath: "/usr/local/bin/arianet"}, InstallScript},
		{"macOS ignores dpkg/rpm", InstallEnv{GOOS: "darwin", ExePath: "/usr/bin/arianet", DpkgInfoDir: dpkg, RpmOwns: rpmYes}, InstallScript},
		{"linuxbrew", InstallEnv{GOOS: "linux", ExePath: "/home/linuxbrew/.linuxbrew/Cellar/arianet/0.4.0/bin/arianet"}, InstallHomebrew},
		{"linuxbrew custom prefix", InstallEnv{GOOS: "linux", ExePath: "/srv/brew/Cellar/arianet/0.4.0/bin/arianet"}, InstallHomebrew},
		{"linux apt", InstallEnv{GOOS: "linux", ExePath: "/usr/bin/arianet", DpkgInfoDir: dpkg}, InstallApt},
		{"linux rpm", InstallEnv{GOOS: "linux", ExePath: "/usr/bin/arianet", DpkgInfoDir: t.TempDir(), RpmOwns: rpmYes}, InstallRpm},
		{"linux apt wins over rpm", InstallEnv{GOOS: "linux", ExePath: "/usr/bin/arianet", DpkgInfoDir: dpkg, RpmOwns: rpmYes}, InstallApt},
		{"linux install.sh", InstallEnv{GOOS: "linux", ExePath: "/home/me/.local/bin/arianet", DpkgInfoDir: dpkg, RpmOwns: func(string) bool { return false }}, InstallScript},
		{"windows exe", InstallEnv{GOOS: "windows", ExePath: `C:\Users\me\bin\arianet.exe`}, InstallScript},
		{"windows never brew", InstallEnv{GOOS: "windows", ExePath: "/opt/homebrew/Cellar/arianet/0.4.0/bin/arianet"}, InstallScript},
		{"freebsd standalone", InstallEnv{GOOS: "freebsd", ExePath: "/usr/local/bin/arianet", DpkgInfoDir: dpkg}, InstallScript},
	}
	for _, c := range cases {
		if got := Detect(c.env); got != c.want {
			t.Errorf("%s: Detect = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestArchiveNameMatchesGoreleaser(t *testing.T) {
	cases := []struct{ goos, goarch, want string }{
		{"darwin", "arm64", "arianet_0.5.0_darwin_arm64.tar.gz"},
		{"darwin", "amd64", "arianet_0.5.0_darwin_amd64.tar.gz"},
		{"linux", "amd64", "arianet_0.5.0_linux_amd64.tar.gz"},
		{"linux", "arm64", "arianet_0.5.0_linux_arm64.tar.gz"},
		{"linux", "386", "arianet_0.5.0_linux_386.tar.gz"},
		{"linux", "arm", "arianet_0.5.0_linux_arm.tar.gz"},
		{"freebsd", "amd64", "arianet_0.5.0_freebsd_amd64.tar.gz"},
		{"windows", "amd64", "arianet_0.5.0_windows_amd64.zip"},
		{"windows", "386", "arianet_0.5.0_windows_386.zip"},
	}
	for _, c := range cases {
		if got := ArchiveName("v0.5.0", c.goos, c.goarch); got != c.want {
			t.Errorf("ArchiveName(%s/%s) = %q, want %q", c.goos, c.goarch, got, c.want)
		}
	}
	if got := PackageName("0.5.0", "arm64", "rpm"); got != "arianet_0.5.0_linux_arm64.rpm" {
		t.Errorf("PackageName = %q", got)
	}
	if BinaryName("windows") != "arianet.exe" || BinaryName("linux") != "arianet" {
		t.Error("BinaryName")
	}
}

func sha(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

func TestChecksum(t *testing.T) {
	data := []byte("archive bytes")
	sums := []byte(sha(data) + "  arianet_0.5.0_linux_amd64.tar.gz\n" +
		strings.Repeat("0", 64) + "  arianet_0.5.0_darwin_arm64.tar.gz\n")
	if err := VerifyChecksum(sums, "arianet_0.5.0_linux_amd64.tar.gz", sha(data)); err != nil {
		t.Errorf("good checksum rejected: %v", err)
	}
	if err := VerifyChecksum(sums, "arianet_0.5.0_linux_amd64.tar.gz", strings.ToUpper(sha(data))); err != nil {
		t.Errorf("upper-case checksum rejected: %v", err)
	}
	if err := VerifyChecksum(sums, "arianet_0.5.0_darwin_arm64.tar.gz", sha(data)); err == nil || !strings.Contains(err.Error(), "mismatch") {
		t.Errorf("bad checksum accepted: %v", err)
	}
	if err := VerifyChecksum(sums, "arianet_0.5.0_windows_amd64.zip", sha(data)); err == nil || !strings.Contains(err.Error(), "not listed") {
		t.Errorf("missing entry accepted: %v", err)
	}
	if _, err := ChecksumFor([]byte("xyz  a.tar.gz\n"), "a.tar.gz"); err == nil {
		t.Error("malformed checksum accepted")
	}
}

type entry struct {
	name string
	body string
	dir  bool
}

func makeTarGz(t *testing.T, path string, entries []entry) {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, e := range entries {
		h := &tar.Header{Name: e.name, Mode: 0o755, Size: int64(len(e.body)), Typeflag: tar.TypeReg}
		if e.dir {
			h = &tar.Header{Name: e.name, Mode: 0o755, Typeflag: tar.TypeDir}
		}
		if err := tw.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
		if !e.dir {
			if _, err := tw.Write([]byte(e.body)); err != nil {
				t.Fatal(err)
			}
		}
	}
	tw.Close()
	gz.Close()
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

func makeZip(t *testing.T, path string, entries []entry) {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, e := range entries {
		name := e.name
		if e.dir {
			name = strings.TrimSuffix(name, "/") + "/"
		}
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if !e.dir {
			w.Write([]byte(e.body))
		}
	}
	zw.Close()
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestExtractBinary(t *testing.T) {
	dir := t.TempDir()
	files := []entry{
		{name: "completions/", dir: true},
		{name: "completions/arianet.bash", body: "complete"},
		{name: "README.md", body: "readme"},
		{name: "LICENSE", body: "mit"},
	}

	tgz := filepath.Join(dir, "a.tar.gz")
	makeTarGz(t, tgz, append(files, entry{name: "arianet", body: "unix-binary"}))
	if b, err := ExtractBinary(tgz, "arianet"); err != nil || string(b) != "unix-binary" {
		t.Errorf("tar.gz: %q %v", b, err)
	}

	nested := filepath.Join(dir, "n.tar.gz")
	makeTarGz(t, nested, []entry{{name: "arianet/", dir: true}, {name: "arianet_0.5.0/arianet", body: "nested"}})
	if b, err := ExtractBinary(nested, "arianet"); err != nil || string(b) != "nested" {
		t.Errorf("nested tar.gz: %q %v", b, err)
	}

	zp := filepath.Join(dir, "a.zip")
	makeZip(t, zp, append(files, entry{name: "arianet.exe", body: "windows-binary"}))
	if b, err := ExtractBinary(zp, "arianet.exe"); err != nil || string(b) != "windows-binary" {
		t.Errorf("zip: %q %v", b, err)
	}

	missing := filepath.Join(dir, "m.tar.gz")
	makeTarGz(t, missing, files)
	if _, err := ExtractBinary(missing, "arianet"); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("missing binary: %v", err)
	}
	if _, err := ExtractBinary(zp, "arianet"); err == nil {
		t.Error("zip without arianet accepted")
	}

	garbage := filepath.Join(dir, "g.tar.gz")
	os.WriteFile(garbage, []byte("not an archive"), 0o644)
	if _, err := ExtractBinary(garbage, "arianet"); err == nil {
		t.Error("garbage accepted")
	}
	gz := filepath.Join(dir, "g.zip")
	os.WriteFile(gz, []byte("not an archive"), 0o644)
	if _, err := ExtractBinary(gz, "arianet.exe"); err == nil {
		t.Error("garbage zip accepted")
	}
}

func writeExe(t *testing.T, body string) string {
	t.Helper()
	target := filepath.Join(t.TempDir(), "arianet")
	if err := os.WriteFile(target, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return target
}

func readFile(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func onlyEntries(t *testing.T, dir string) []string {
	t.Helper()
	des, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, d := range des {
		names = append(names, d.Name())
	}
	return names
}

func TestReplaceUnix(t *testing.T) {
	target := writeExe(t, "old")
	var checked string
	r := &Replacer{Target: target, GOOS: "linux", Check: func(p string) error {
		checked = readFile(t, p)
		return nil
	}}
	if err := r.Replace([]byte("new")); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, target); got != "new" {
		t.Errorf("target = %q", got)
	}
	if checked != "new" {
		t.Errorf("self-check saw %q", checked)
	}
	if runtime.GOOS != "windows" {
		fi, _ := os.Stat(target)
		if fi.Mode().Perm()&0o100 == 0 {
			t.Errorf("not executable: %v", fi.Mode())
		}
	}
	if names := onlyEntries(t, filepath.Dir(target)); len(names) != 1 {
		t.Errorf("leftover files: %v", names)
	}
}

func TestReplaceSelfCheckFailureKeepsOld(t *testing.T) {
	for _, goos := range []string{"linux", "windows"} {
		target := writeExe(t, "old")
		r := &Replacer{Target: target, GOOS: goos, Check: func(string) error { return errors.New("exec format error") }}
		if err := r.Replace([]byte("broken")); err == nil {
			t.Fatalf("%s: self-check failure not reported", goos)
		}
		if got := readFile(t, target); got != "old" {
			t.Errorf("%s: target = %q", goos, got)
		}
		if names := onlyEntries(t, filepath.Dir(target)); len(names) != 1 {
			t.Errorf("%s: leftover files: %v", goos, names)
		}
	}
}

func TestReplaceWindowsMovesAside(t *testing.T) {
	target := writeExe(t, "old")
	os.WriteFile(OldPath(target), []byte("older leftover"), 0o755)
	r := &Replacer{Target: target, GOOS: "windows"}
	if err := r.Replace([]byte("new")); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, target); got != "new" {
		t.Errorf("target = %q", got)
	}
	if got := readFile(t, OldPath(target)); got != "old" {
		t.Errorf(".old = %q", got)
	}

	CleanupOld(target, "linux")
	if _, err := os.Stat(OldPath(target)); err != nil {
		t.Error("CleanupOld must only act on windows")
	}
	CleanupOld(target, "windows")
	if _, err := os.Stat(OldPath(target)); !os.IsNotExist(err) {
		t.Errorf(".old not cleaned up: %v", err)
	}
	CleanupOld(target, "windows") // nothing to clean is fine
}

func TestReplaceWindowsRestoresOnFailure(t *testing.T) {
	target := writeExe(t, "old")
	r := &Replacer{Target: target, GOOS: "windows"}
	r.rename = func(oldpath, newpath string) error {
		if newpath == target && oldpath != OldPath(target) {
			return errors.New("simulated: file in use")
		}
		return os.Rename(oldpath, newpath)
	}
	err := r.Replace([]byte("new"))
	if err == nil || !strings.Contains(err.Error(), "file in use") {
		t.Fatalf("err = %v", err)
	}
	if got := readFile(t, target); got != "old" {
		t.Errorf("old executable not restored, target = %q", got)
	}
	if names := onlyEntries(t, filepath.Dir(target)); len(names) != 1 {
		t.Errorf("leftover files: %v", names)
	}
}

func TestReplaceWindowsRestoreFailureSaysWhere(t *testing.T) {
	target := writeExe(t, "old")
	r := &Replacer{Target: target, GOOS: "windows"}
	r.rename = func(oldpath, newpath string) error {
		if oldpath == target {
			return os.Rename(oldpath, newpath)
		}
		return errors.New("simulated")
	}
	err := r.Replace([]byte("new"))
	if err == nil || !strings.Contains(err.Error(), OldPath(target)) {
		t.Fatalf("error should name the .old path: %v", err)
	}
}

func TestReplaceNotWritable(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("needs a non-root Unix user")
	}
	target := writeExe(t, "old")
	dir := filepath.Dir(target)
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(dir, 0o755) })
	for _, goos := range []string{"linux", "windows"} {
		err := (&Replacer{Target: target, GOOS: goos}).Replace([]byte("new"))
		if !errors.Is(err, ErrNeedsElevation) {
			t.Errorf("%s: err = %v, want ErrNeedsElevation", goos, err)
		}
	}
	if got := readFile(t, target); got != "old" {
		t.Errorf("target = %q", got)
	}
}

func TestReleasesLookup(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			t.Error("release lookup must be unauthenticated")
		}
		switch r.URL.Path {
		case "/releases/latest":
			json.NewEncoder(w).Encode(map[string]any{
				"tag_name": "v0.5.0", "html_url": "https://example/v0.5.0",
				"assets": []map[string]string{{"name": "checksums.txt", "browser_download_url": srv.URL + "/dl/checksums.txt"}},
			})
		case "/releases/tags/v0.3.0":
			json.NewEncoder(w).Encode(map[string]any{"tag_name": "v0.3.0"})
		case "/releases/tags/v0.6.0-rc.1":
			json.NewEncoder(w).Encode(map[string]any{"tag_name": "v0.6.0-rc.1", "prerelease": true})
		case "/releases/tags/v0.7.0":
			json.NewEncoder(w).Encode(map[string]any{"tag_name": "v0.7.0", "draft": true})
		case "/dl/checksums.txt":
			fmt.Fprint(w, "abc")
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	rs := &Releases{API: srv.URL + "/releases/", Client: srv.Client()}
	ctx := context.Background()

	rel, err := rs.Latest(ctx)
	if err != nil || rel.Version != "0.5.0" || rel.Asset("checksums.txt") == nil || rel.Asset("nope") != nil {
		t.Fatalf("Latest = %+v, %v", rel, err)
	}
	if rel, err := rs.Tag(ctx, "0.3.0"); err != nil || rel.Version != "0.3.0" {
		t.Errorf("Tag(0.3.0) = %+v, %v", rel, err)
	}
	if rel, err := rs.Tag(ctx, "v0.6.0-rc.1"); err != nil || rel.Version != "0.6.0-rc.1" {
		t.Errorf("Tag(rc) = %+v, %v", rel, err)
	}
	if _, err := rs.Tag(ctx, "v0.7.0"); err == nil {
		t.Error("draft accepted")
	}
	if _, err := rs.Tag(ctx, "v9.9.9"); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("missing tag: %v", err)
	}
	if _, err := rs.Tag(ctx, "latest"); err == nil {
		t.Error("invalid version accepted")
	}

	dir := t.TempDir()
	p, sum, err := rs.Download(ctx, srv.URL+"/dl/checksums.txt", dir, "x-*")
	if err != nil || readFile(t, p) != "abc" || sum != sha([]byte("abc")) {
		t.Errorf("Download = %s %s %v", p, sum, err)
	}
	if _, _, err := rs.Download(ctx, srv.URL+"/dl/missing", dir, "x-*"); err == nil {
		t.Error("404 download accepted")
	}
	if names := onlyEntries(t, dir); len(names) != 1 {
		t.Errorf("failed download left files: %v", names)
	}
}

func TestRememberRefreshesCache(t *testing.T) {
	c := &Checker{CacheFile: filepath.Join(t.TempDir(), "update-check.json")}
	if err := c.Remember("v0.5.0"); err != nil {
		t.Fatal(err)
	}
	r, err := c.readCache()
	if err != nil || r.Version != "0.5.0" || !Fresh(r, c.now()) {
		t.Errorf("cache = %+v, %v", r, err)
	}
}
