package update

// This file is the in-place upgrade used by `arianet upgrade`: release lookup
// with assets, archive naming that matches .goreleaser.yaml, SHA-256
// verification against checksums.txt, archive extraction and an atomic swap of
// the running executable.

import (
	"archive/tar"
	"archive/zip"
	"bufio"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

const (
	// ReleasesAPI is the GitHub API base for this repository's releases.
	ReleasesAPI = "https://api.github.com/repos/ariaservice/arianet-cli/releases"

	// ChecksumsAsset is goreleaser's checksum file name (checksum.name_template).
	ChecksumsAsset = "checksums.txt"

	// SignatureAsset is the Sigstore bundle over ChecksumsAsset.
	SignatureAsset = ChecksumsAsset + ".sigstore.json"

	// DownloadTimeout bounds one asset download.
	DownloadTimeout = 5 * time.Minute

	// maxArchiveSize and maxBinarySize stop a hostile or broken release from
	// filling the disk or memory.
	maxArchiveSize = 200 << 20
	maxBinarySize  = 200 << 20
)

// ErrNeedsElevation means the executable's directory is not writable by this
// user, so the upgrade has to be re-run with sudo or as Administrator.
var ErrNeedsElevation = errors.New("the install directory is not writable")

// ErrNoBuild means the release has no archive for this OS/architecture.
var ErrNoBuild = errors.New("no release archive for this platform")

// Asset is one downloadable file of a release.
type Asset struct {
	Name string `json:"name"`
	URL  string `json:"browser_download_url"`
}

// FullRelease is a release with its version and downloadable assets.
type FullRelease struct {
	Version string
	URL     string
	Assets  []Asset
}

// Asset returns the named asset, or nil.
func (r *FullRelease) Asset(name string) *Asset {
	for i := range r.Assets {
		if r.Assets[i].Name == name {
			return &r.Assets[i]
		}
	}
	return nil
}

// Releases looks releases up on the GitHub API and downloads their assets.
type Releases struct {
	// API is the releases endpoint base; ReleasesAPI when empty.
	API    string
	Client *http.Client
}

// Latest returns the newest stable release.
func (r *Releases) Latest(ctx context.Context) (*FullRelease, error) {
	rel, err := r.get(ctx, r.api()+"/latest")
	if err != nil {
		return nil, err
	}
	v, _ := ParseVersion(rel.Version)
	if v.Pre != "" {
		return nil, errors.New("latest release is not a stable release")
	}
	return rel, nil
}

// Tag returns the release for one version ("0.4.0" or "v0.4.0").
func (r *Releases) Tag(ctx context.Context, version string) (*FullRelease, error) {
	v, ok := ParseVersion(version)
	if !ok {
		return nil, fmt.Errorf("invalid version %q (want vX.Y.Z)", version)
	}
	return r.get(ctx, r.api()+"/tags/v"+v.String())
}

func (r *Releases) api() string {
	if r.API != "" {
		return strings.TrimRight(r.API, "/")
	}
	return ReleasesAPI
}

func (r *Releases) client() *http.Client {
	if r.Client != nil {
		return r.Client
	}
	return &http.Client{Timeout: DownloadTimeout}
}

type githubFullRelease struct {
	TagName    string  `json:"tag_name"`
	HTMLURL    string  `json:"html_url"`
	Draft      bool    `json:"draft"`
	Prerelease bool    `json:"prerelease"`
	Assets     []Asset `json:"assets"`
}

func (r *Releases) get(ctx context.Context, url string) (*FullRelease, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "arianet-cli-upgrade")
	resp, err := r.client().Do(req)
	if err != nil {
		return nil, fmt.Errorf("release lookup: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, errors.New("release not found")
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("release lookup: HTTP %d", resp.StatusCode)
	}
	var gr githubFullRelease
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&gr); err != nil {
		return nil, fmt.Errorf("release lookup: %w", err)
	}
	if gr.Draft {
		return nil, errors.New("release is a draft")
	}
	v, ok := ParseVersion(gr.TagName)
	if !ok {
		return nil, fmt.Errorf("unexpected release tag %q", gr.TagName)
	}
	if gr.Prerelease && v.Pre == "" {
		v.Pre = "prerelease"
	}
	return &FullRelease{Version: v.String(), URL: gr.HTMLURL, Assets: gr.Assets}, nil
}

// Download fetches url into a new file in dir, stopping at maxArchiveSize,
// and returns its path and SHA-256.
func (r *Releases) Download(ctx context.Context, url, dir, pattern string) (string, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", "", err
	}
	req.Header.Set("User-Agent", "arianet-cli-upgrade")
	resp, err := r.client().Do(req)
	if err != nil {
		return "", "", fmt.Errorf("download: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("download %s: HTTP %d", path.Base(url), resp.StatusCode)
	}
	f, err := os.CreateTemp(dir, pattern)
	if err != nil {
		return "", "", err
	}
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(f, h), io.LimitReader(resp.Body, maxArchiveSize+1))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil && n > maxArchiveSize {
		err = fmt.Errorf("download %s: larger than %d bytes", path.Base(url), maxArchiveSize)
	}
	if err != nil {
		os.Remove(f.Name())
		return "", "", err
	}
	return f.Name(), hex.EncodeToString(h.Sum(nil)), nil
}

// ArchiveName is the release archive for a version and platform, exactly as
// .goreleaser.yaml names it: arianet_<version>_<os>_<arch>.tar.gz, and .zip on
// Windows.
func ArchiveName(version, goos, goarch string) string {
	ext := ".tar.gz"
	if goos == "windows" {
		ext = ".zip"
	}
	return fmt.Sprintf("arianet_%s_%s_%s%s", strings.TrimPrefix(version, "v"), goos, goarch, ext)
}

// PackageName is the release .deb or .rpm for a version and Linux arch.
func PackageName(version, goarch, format string) string {
	return fmt.Sprintf("arianet_%s_linux_%s.%s", strings.TrimPrefix(version, "v"), goarch, format)
}

// BinaryName is the executable's name inside an archive.
func BinaryName(goos string) string {
	if goos == "windows" {
		return "arianet.exe"
	}
	return "arianet"
}

// ChecksumFor finds name's SHA-256 in a checksums.txt body
// ("<hex>  <name>" per line).
func ChecksumFor(checksums []byte, name string) (string, error) {
	sc := bufio.NewScanner(strings.NewReader(string(checksums)))
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) == 2 && strings.TrimPrefix(fields[1], "*") == name {
			sum := strings.ToLower(fields[0])
			if b, err := hex.DecodeString(sum); err != nil || len(b) != sha256.Size {
				return "", fmt.Errorf("malformed checksum for %s", name)
			}
			return sum, nil
		}
	}
	return "", fmt.Errorf("%s is not listed in %s", name, ChecksumsAsset)
}

// VerifyChecksum compares an actual SHA-256 with the one checksums.txt lists.
func VerifyChecksum(checksums []byte, name, actual string) error {
	want, err := ChecksumFor(checksums, name)
	if err != nil {
		return err
	}
	if !strings.EqualFold(want, actual) {
		return fmt.Errorf("checksum mismatch for %s; refusing to install", name)
	}
	return nil
}

// ExtractBinary returns the named executable from a .tar.gz or .zip archive.
// Only a regular file whose base name is binary is accepted; directories in
// the archive path are ignored.
func ExtractBinary(archivePath, binary string) ([]byte, error) {
	if strings.HasSuffix(archivePath, ".zip") {
		return extractZip(archivePath, binary)
	}
	return extractTarGz(archivePath, binary)
}

func extractTarGz(archivePath, binary string) ([]byte, error) {
	f, err := os.Open(archivePath)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return nil, fmt.Errorf("read archive: %w", err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read archive: %w", err)
		}
		if h.Typeflag != tar.TypeReg || path.Base(h.Name) != binary {
			continue
		}
		return readLimited(tr, binary)
	}
	return nil, fmt.Errorf("%s not found in the archive", binary)
}

func extractZip(archivePath, binary string) ([]byte, error) {
	zr, err := zip.OpenReader(archivePath)
	if err != nil {
		return nil, fmt.Errorf("read archive: %w", err)
	}
	defer zr.Close()
	for _, zf := range zr.File {
		if !zf.Mode().IsRegular() || path.Base(zf.Name) != binary {
			continue
		}
		rc, err := zf.Open()
		if err != nil {
			return nil, fmt.Errorf("read archive: %w", err)
		}
		defer rc.Close()
		return readLimited(rc, binary)
	}
	return nil, fmt.Errorf("%s not found in the archive", binary)
}

func readLimited(r io.Reader, name string) ([]byte, error) {
	b, err := io.ReadAll(io.LimitReader(r, maxBinarySize+1))
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", name, err)
	}
	if len(b) > maxBinarySize {
		return nil, fmt.Errorf("%s is larger than %d bytes", name, maxBinarySize)
	}
	if len(b) == 0 {
		return nil, fmt.Errorf("%s is empty", name)
	}
	return b, nil
}

// Replacer swaps the executable at Target for a new binary.
type Replacer struct {
	// Target is the executable to replace (os.Executable, symlinks resolved).
	Target string
	// GOOS selects the swap strategy; "windows" moves the running file aside
	// because Windows cannot overwrite or rename over a running executable.
	GOOS string
	// Check, when set, runs against the new binary (already in place next to
	// Target under a temporary name) before the swap; an error aborts it.
	Check func(path string) error

	rename func(oldpath, newpath string) error // os.Rename; tests fail it
}

// OldPath is where Windows keeps the previous executable until the next run.
func OldPath(target string) string { return target + ".old" }

// Replace installs binary at r.Target. The new file is written in the same
// directory first so the final step is a rename on the same filesystem; on
// any failure the original executable is left (or put back) in place.
func (r *Replacer) Replace(binary []byte) error {
	rename := r.rename
	if rename == nil {
		rename = os.Rename
	}
	dir := filepath.Dir(r.Target)
	mode := fs.FileMode(0o755)
	if fi, err := os.Stat(r.Target); err == nil {
		mode = fi.Mode().Perm() | 0o100
	}

	pattern := ".arianet-upgrade-*"
	if r.GOOS == "windows" {
		pattern += ".exe"
	}
	tmp, err := os.CreateTemp(dir, pattern)
	if err != nil {
		if errors.Is(err, fs.ErrPermission) {
			return ErrNeedsElevation
		}
		return err
	}
	tmpPath := tmp.Name()
	keepTmp := false
	defer func() {
		if !keepTmp {
			os.Remove(tmpPath)
		}
	}()
	if _, err := tmp.Write(binary); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpPath, mode); err != nil {
		return err
	}
	if r.Check != nil {
		if err := r.Check(tmpPath); err != nil {
			return fmt.Errorf("the downloaded binary failed its self-check: %w", err)
		}
	}

	if r.GOOS != "windows" {
		if err := rename(tmpPath, r.Target); err != nil {
			if errors.Is(err, fs.ErrPermission) {
				return ErrNeedsElevation
			}
			return err
		}
		keepTmp = true
		return nil
	}

	old := OldPath(r.Target)
	_ = os.Remove(old) // a leftover from an earlier upgrade
	if err := rename(r.Target, old); err != nil {
		if errors.Is(err, fs.ErrPermission) {
			return ErrNeedsElevation
		}
		return err
	}
	if err := rename(tmpPath, r.Target); err != nil {
		if rerr := rename(old, r.Target); rerr != nil {
			return fmt.Errorf("%v; restoring the previous executable also failed: %v (it is at %s)", err, rerr, old)
		}
		if errors.Is(err, fs.ErrPermission) {
			return ErrNeedsElevation
		}
		return err
	}
	keepTmp = true
	return nil
}

// CleanupOld removes the executable a Windows upgrade moved aside. It is
// called on start-up; while the old process still runs the delete fails and is
// tried again next time.
func CleanupOld(target, goos string) {
	if goos != "windows" || target == "" {
		return
	}
	_ = os.Remove(OldPath(target))
}

// Remember records version as the latest known release in the update cache, so the
// notifier does not announce a version that was just installed.
func (c *Checker) Remember(version string) error {
	return c.writeCache(&Release{Version: strings.TrimPrefix(version, "v"), URL: ReleaseURL(version), CheckedAt: c.now()})
}
