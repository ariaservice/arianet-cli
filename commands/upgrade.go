package commands

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/ariaservice/arianet-cli/internal/config"
	"github.com/ariaservice/arianet-cli/internal/update"
	"github.com/ariaservice/arianet-cli/pkg/version"
	"github.com/spf13/cobra"
)

// exitCodeAvailable is what `arianet upgrade --check` exits with when a newer
// release is available.
const exitCodeAvailable = 2

// exitError ends the run with a specific exit code. An empty msg prints
// nothing more (the command already explained itself).
type exitError struct {
	code int
	msg  string
}

func (e *exitError) Error() string { return e.msg }

// cosignIdentity is the release workflow that signs checksums.txt.
const cosignIdentity = `^https://github.com/ariaservice/arianet-cli/\.github/workflows/release\.yml@refs/tags/v`

func newUpgradeCmd() *cobra.Command {
	var opts upgradeOptions
	cmd := &cobra.Command{
		Use:     "upgrade",
		Aliases: []string{"update"},
		Short:   "Upgrade arianet to the latest release",
		Long: `Upgrade arianet in place, the way it was installed:

  Homebrew            runs: brew upgrade ariaservice/arianet/arianet
  apt (Debian/Ubuntu) runs: ` + update.AptUpgradeCommand + `
                      (in a terminal; otherwise it prints the command and exits 1)
  rpm                 downloads the release .rpm, verifies it, runs: sudo rpm -U
  standalone binary   (install script, manual download, Windows .exe) downloads
                      the release archive for this OS/architecture, verifies its
                      SHA-256 against checksums.txt (and the Sigstore signature
                      when cosign is installed) and replaces this executable.

If the executable's directory is not writable, re-run with sudo (or, on
Windows, from a terminal opened as Administrator).

Exit codes with --check: 0 up to date, 2 a newer release is available,
1 the check failed.`,
		Example: "  arianet upgrade\n  arianet upgrade --check\n  arianet upgrade --yes\n  arianet upgrade --version v0.4.0 --allow-downgrade",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			u := newUpgrader(cmd.OutOrStdout(), cmd.ErrOrStderr())
			return u.run(cmd.Context(), opts)
		},
	}
	cmd.Flags().BoolVar(&opts.check, "check", false, "only report whether a newer release exists (exit 2 if one does)")
	cmd.Flags().BoolVarP(&opts.yes, "yes", "y", false, "upgrade without asking")
	cmd.Flags().StringVar(&opts.version, "version", "", "install this release (vX.Y.Z) instead of the latest")
	cmd.Flags().BoolVar(&opts.allowDowngrade, "allow-downgrade", false, "allow --version to install an older release")
	return cmd
}

type upgradeOptions struct {
	check          bool
	yes            bool
	version        string
	allowDowngrade bool
}

// upgrader holds everything `arianet upgrade` touches, so tests can replace
// the OS, the release server, the executable and the commands it runs.
type upgrader struct {
	out, errOut io.Writer
	current     string
	goos        string
	goarch      string
	exePath     string
	method      update.InstallMethod
	interactive bool // stdin and stderr are terminals
	releases    *update.Releases
	cache       *update.Checker // nil: no cache refresh
	confirm     func(prompt string) bool
	runCmd      func(name string, args ...string) error
	// verifySignature checks checksums.txt against its Sigstore bundle; nil
	// when cosign is not installed.
	verifySignature func(checksums, bundle string) error
	// selfCheck runs a downloaded binary before it replaces this one.
	selfCheck func(path, version string) error
	tempDir   string
}

func newUpgrader(out, errOut io.Writer) *upgrader {
	exe := update.ExecutablePath()
	u := &upgrader{
		out:         out,
		errOut:      errOut,
		current:     version.Version,
		goos:        runtime.GOOS,
		goarch:      runtime.GOARCH,
		exePath:     exe,
		interactive: update.IsTerminal(os.Stdin) && update.IsTerminal(os.Stderr),
		releases:    &update.Releases{},
		confirm:     confirm,
		runCmd:      runPassthrough,
		selfCheck:   selfCheckBinary,
	}
	u.method = update.Detect(update.InstallEnv{
		GOOS:        runtime.GOOS,
		ExePath:     exe,
		DpkgInfoDir: "/var/lib/dpkg/info",
		RpmOwns:     rpmOwns,
	})
	if dir, err := config.Dir(); err == nil {
		u.cache = &update.Checker{CacheFile: update.CacheFile(dir)}
	}
	if _, err := exec.LookPath("cosign"); err == nil {
		u.verifySignature = cosignVerify
	}
	return u
}

func (u *upgrader) run(ctx context.Context, opts upgradeOptions) error {
	if ctx == nil {
		ctx = context.Background()
	}
	cur, ok := update.ParseVersion(u.current)
	if !ok {
		return fmt.Errorf("this is a development build (%s) and cannot be upgraded; install a release instead", u.current)
	}

	lookupCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	var rel *update.FullRelease
	var err error
	if opts.version != "" {
		rel, err = u.releases.Tag(lookupCtx, opts.version)
	} else {
		rel, err = u.releases.Latest(lookupCtx)
	}
	if err != nil {
		if opts.version != "" {
			return fmt.Errorf("release %s: %w", opts.version, err)
		}
		return fmt.Errorf("could not find the latest release: %w", err)
	}
	target, _ := update.ParseVersion(rel.Version)

	if opts.version == "" {
		u.remember(rel.Version)
	}
	switch cmp := update.Compare(target, cur); {
	case cmp == 0:
		fmt.Fprintf(u.out, "arianet is up to date (v%s)\n", cur)
		return nil
	case cmp < 0 && opts.version == "":
		fmt.Fprintf(u.out, "arianet is up to date (v%s; the latest release is v%s)\n", cur, target)
		return nil
	case cmp < 0 && !opts.allowDowngrade:
		return fmt.Errorf("v%s is older than the installed v%s; pass --allow-downgrade to install it anyway", target, cur)
	}

	if opts.check {
		if update.Compare(target, cur) > 0 {
			fmt.Fprintf(u.out, "A new release of arianet is available: %s → %s\n", cur, target)
		} else {
			fmt.Fprintf(u.out, "v%s is available (installed: v%s)\n", target, cur)
		}
		fmt.Fprintf(u.out, "Installed with: %s\n", u.method)
		fmt.Fprintf(u.out, "To upgrade, run: %s\n", u.suggestedCommand(opts))
		fmt.Fprintln(u.out, update.ReleaseURL(target.String()))
		return &exitError{code: exitCodeAvailable}
	}

	switch u.method {
	case update.InstallHomebrew:
		return u.viaHomebrew(cur, target, opts)
	case update.InstallApt:
		return u.viaApt(cur, target, opts)
	case update.InstallRpm:
		return u.viaRpm(ctx, cur, target, rel, opts)
	default:
		return u.viaStandalone(ctx, cur, target, rel, opts)
	}
}

func (u *upgrader) suggestedCommand(opts upgradeOptions) string {
	if opts.version != "" {
		s := "arianet upgrade --version v" + strings.TrimPrefix(opts.version, "v")
		if opts.allowDowngrade {
			s += " --allow-downgrade"
		}
		return s
	}
	return "arianet upgrade"
}

// approve asks before upgrading unless --yes was given; without a terminal it
// refuses rather than upgrade silently.
func (u *upgrader) approve(cur, target update.Version, opts upgradeOptions) error {
	if opts.yes {
		return nil
	}
	if !u.interactive {
		return errors.New("not running in a terminal; re-run with --yes to upgrade without a prompt")
	}
	if !u.confirm(fmt.Sprintf("Upgrade arianet %s → %s (%s)?", cur, target, u.method)) {
		return &exitError{code: 1, msg: "upgrade cancelled"}
	}
	return nil
}

func (u *upgrader) viaHomebrew(cur, target update.Version, opts upgradeOptions) error {
	if opts.version != "" {
		return errors.New("--version is not supported for Homebrew installs; Homebrew installs the latest release")
	}
	if err := u.approve(cur, target, opts); err != nil {
		return err
	}
	if err := u.runCmd("brew", "upgrade", "ariaservice/arianet/arianet"); err != nil {
		return fmt.Errorf("brew upgrade failed: %w", err)
	}
	u.done(cur, target)
	return nil
}

func (u *upgrader) viaApt(cur, target update.Version, opts upgradeOptions) error {
	if opts.version != "" {
		return errors.New("--version is not supported for apt installs; apt installs the latest release in the repository")
	}
	if !u.interactive {
		fmt.Fprintf(u.errOut, "arianet was installed with apt. To upgrade %s → %s, run:\n  %s\n", cur, target, update.AptUpgradeCommand)
		return &exitError{code: 1}
	}
	if err := u.approve(cur, target, opts); err != nil {
		return err
	}
	if err := u.runCmd("sudo", "apt-get", "update"); err != nil {
		return fmt.Errorf("apt-get update failed: %w", err)
	}
	if err := u.runCmd("sudo", "apt-get", "install", "--only-upgrade", "-y", "arianet"); err != nil {
		return fmt.Errorf("apt-get install failed: %w", err)
	}
	u.done(cur, target)
	return nil
}

func (u *upgrader) viaRpm(ctx context.Context, cur, target update.Version, rel *update.FullRelease, opts upgradeOptions) error {
	name := update.PackageName(target.String(), u.goarch, "rpm")
	asset := rel.Asset(name)
	if asset == nil {
		return fmt.Errorf("release v%s has no %s: %w", target, name, update.ErrNoBuild)
	}
	rpmArgs := []string{"rpm", "-U"}
	if update.Compare(target, cur) < 0 {
		rpmArgs = append(rpmArgs, "--oldpackage")
	}
	if !u.interactive {
		fmt.Fprintf(u.errOut, "arianet was installed from an .rpm. To upgrade %s → %s, run:\n  sudo %s %s\n", cur, target, strings.Join(rpmArgs, " "), asset.URL)
		return &exitError{code: 1}
	}
	if err := u.approve(cur, target, opts); err != nil {
		return err
	}
	dir, err := u.workDir()
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	pkg, err := u.fetchVerified(ctx, rel, name, dir)
	if err != nil {
		return err
	}
	if err := u.runCmd("sudo", append(rpmArgs, pkg)...); err != nil {
		return fmt.Errorf("rpm upgrade failed: %w", err)
	}
	u.done(cur, target)
	return nil
}

func (u *upgrader) viaStandalone(ctx context.Context, cur, target update.Version, rel *update.FullRelease, opts upgradeOptions) error {
	if u.exePath == "" {
		return errors.New("cannot find this executable's path")
	}
	name := update.ArchiveName(target.String(), u.goos, u.goarch)
	if rel.Asset(name) == nil {
		return fmt.Errorf("release v%s has no build for %s/%s (%s); see %s: %w",
			target, u.goos, u.goarch, name, update.ReleaseURL(target.String()), update.ErrNoBuild)
	}
	if err := u.approve(cur, target, opts); err != nil {
		return err
	}
	fmt.Fprintf(u.errOut, "Downloading arianet %s (%s/%s)\n", target, u.goos, u.goarch)

	dir, err := u.workDir()
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	archive, err := u.fetchVerified(ctx, rel, name, dir)
	if err != nil {
		return err
	}
	binary, err := update.ExtractBinary(archive, update.BinaryName(u.goos))
	if err != nil {
		return err
	}

	r := &update.Replacer{Target: u.exePath, GOOS: u.goos}
	if u.selfCheck != nil {
		want := target.String()
		r.Check = func(path string) error { return u.selfCheck(path, want) }
	}
	if err := r.Replace(binary); err != nil {
		if errors.Is(err, update.ErrNeedsElevation) {
			return u.elevationError()
		}
		return fmt.Errorf("could not replace %s (it is unchanged): %w", u.exePath, err)
	}
	u.done(cur, target)
	return nil
}

func (u *upgrader) elevationError() error {
	dir := filepath.Dir(u.exePath)
	if u.goos == "windows" {
		return fmt.Errorf("cannot write to %s; open a terminal as Administrator and run arianet upgrade again", dir)
	}
	return fmt.Errorf("cannot write to %s; re-run with: sudo arianet upgrade", dir)
}

// fetchVerified downloads one release asset and checks it against
// checksums.txt (and its signature when cosign is available).
func (u *upgrader) fetchVerified(ctx context.Context, rel *update.FullRelease, name, dir string) (string, error) {
	sums := rel.Asset(update.ChecksumsAsset)
	if sums == nil {
		return "", fmt.Errorf("release v%s has no %s; refusing to install an unverified download", rel.Version, update.ChecksumsAsset)
	}
	dlCtx, cancel := context.WithTimeout(ctx, update.DownloadTimeout)
	defer cancel()

	sumsPath, _, err := u.releases.Download(dlCtx, sums.URL, dir, "checksums-*.txt")
	if err != nil {
		return "", err
	}
	if u.verifySignature != nil {
		bundle := rel.Asset(update.SignatureAsset)
		if bundle == nil {
			return "", fmt.Errorf("release v%s has no %s; refusing to install", rel.Version, update.SignatureAsset)
		}
		bundlePath, _, err := u.releases.Download(dlCtx, bundle.URL, dir, "checksums-*.sigstore.json")
		if err != nil {
			return "", err
		}
		if err := u.verifySignature(sumsPath, bundlePath); err != nil {
			return "", fmt.Errorf("signature verification of %s failed; refusing to install: %w", update.ChecksumsAsset, err)
		}
	}
	checksums, err := os.ReadFile(sumsPath)
	if err != nil {
		return "", err
	}

	asset := rel.Asset(name)
	pattern := "download-*-" + name
	path, sum, err := u.releases.Download(dlCtx, asset.URL, dir, pattern)
	if err != nil {
		return "", err
	}
	if err := update.VerifyChecksum(checksums, name, sum); err != nil {
		return "", err
	}
	fmt.Fprintln(u.errOut, "  checksum OK")
	if u.verifySignature != nil {
		fmt.Fprintln(u.errOut, "  signature OK")
	}
	return path, nil
}

func (u *upgrader) workDir() (string, error) {
	return os.MkdirTemp(u.tempDir, "arianet-upgrade-")
}

func (u *upgrader) done(cur, target update.Version) {
	u.remember(target.String())
	fmt.Fprintf(u.out, "Upgraded arianet %s → %s\n", cur, target)
}

// remember refreshes the notifier's cache with a release just seen or
// installed.
func (u *upgrader) remember(v string) {
	if u.cache != nil {
		_ = u.cache.Remember(v)
	}
}

// runPassthrough runs a command with this process's stdin, stdout and stderr.
func runPassthrough(name string, args ...string) error {
	if _, err := exec.LookPath(name); err != nil {
		return fmt.Errorf("%s not found in PATH", name)
	}
	c := exec.Command(name, args...)
	c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
	return c.Run()
}

// rpmOwns asks the rpm database whether the arianet package owns path.
func rpmOwns(path string) bool {
	if _, err := exec.LookPath("rpm"); err != nil {
		return false
	}
	out, err := exec.Command("rpm", "-qf", "--queryformat", "%{NAME}", path).Output()
	return err == nil && strings.TrimSpace(string(out)) == "arianet"
}

// cosignVerify checks checksums.txt was signed by this repository's release
// workflow, exactly as scripts/install.sh does.
func cosignVerify(checksums, bundle string) error {
	c := exec.Command("cosign", "verify-blob",
		"--bundle", bundle,
		"--certificate-identity-regexp", cosignIdentity,
		"--certificate-oidc-issuer", "https://token.actions.githubusercontent.com",
		checksums)
	if out, err := c.CombinedOutput(); err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// selfCheckBinary runs `<path> version` and expects it to report version, so
// a wrong-architecture or corrupt binary never replaces a working one.
func selfCheckBinary(path, want string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	c := exec.CommandContext(ctx, path, "version")
	c.Env = append(os.Environ(), update.EnvDisable+"=1")
	out, err := c.Output()
	if err != nil {
		return err
	}
	if !strings.Contains(string(out), want) {
		return fmt.Errorf("it reports %q, expected version %s", strings.TrimSpace(firstLine(string(out))), want)
	}
	return nil
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
