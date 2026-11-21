package commands

import (
	"archive/zip"
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ariaservice/arianet-cli/internal/printer"
	"github.com/spf13/cobra"
)

// functions: the project's edge functions (arianet-infra-tasks#1148).
// The code lives in arianet/functions/<name>/ next to shared folders whose
// names start with _ (e.g. _shared/); deploy zips a function's folder with
// the shared ones and uploads it to POST /projects/{id}/functions/{name}/deploy.

// defaultFunctionsDir is where new, deploy, download and serve look for the code.
const defaultFunctionsDir = "arianet/functions"

var (
	functionNameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,62}$`)
	secretNameRe   = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,127}$`)
)

// reservedFunctionNames are paths of the functions API itself.
var reservedFunctionNames = map[string]bool{"secrets": true, "config": true}

// functionEntrypoints are the files a function may start from, in the order tried.
var functionEntrypoints = []string{"index.ts", "index.js", "index.mjs", "index.tsx", "index.jsx"}

// Bundle rules the service enforces too; the size limit is the plan's.
const (
	bundleMaxFiles      = 1000
	bundleMaxPathBytes  = 255
	bundleExpandFactor  = 5
	maxSecretValueBytes = 64 << 10
	maxSecretsPerCall   = 100
	// maxDownloadBytes bounds what download reads and unpacks when the
	// plan's limit is not known.
	maxDownloadBytes = 512 << 20
)

// bundleTime is every zip entry's modification time, so the same files give
// the same bundle (and the same sha256).
var bundleTime = time.Date(1980, 1, 1, 0, 0, 0, 0, time.UTC)

var baasFunctionCols = []col{{header: "Name", key: "name"}, {header: "Version", key: "version"}, {header: "Verify JWT", key: "verify_jwt"},
	{header: "Deployed", key: "deployed_at"}, {header: "URL", key: "url"}}

var baasSecretCols = []col{{header: "Name", key: "name"}, {header: "Digest", key: "digest"}, {header: "Updated", key: "updated_at"}}

func validateFunctionName(name string) error {
	if reservedFunctionNames[name] {
		return fmt.Errorf("%q is reserved; choose another function name", name)
	}
	if !functionNameRe.MatchString(name) {
		return fmt.Errorf("invalid function name %q: 1-63 lowercase letters, digits, - or _, starting with a letter or digit", name)
	}
	return nil
}

func validateSecretName(name string) error {
	if !secretNameRe.MatchString(name) {
		return fmt.Errorf("invalid secret name %q: 1-128 letters, digits or _, not starting with a digit", name)
	}
	if strings.HasPrefix(strings.ToUpper(name), "ARIANET_") {
		return fmt.Errorf("secret name %q is reserved: names starting with ARIANET_ are set by the platform", name)
	}
	return nil
}

func functionPath(id, name string) string {
	return "/projects/" + id + "/functions/" + url.PathEscape(name)
}

// ─── bundle ──────────────────────────────────────────────────────────────────

// functionBundle is a deployable zip.
type functionBundle struct {
	Zip        []byte
	Entrypoint string   // path inside the zip, e.g. hello/index.ts
	Files      []string // paths inside the zip, in order
}

// checkBundlePath applies the service's path rules to a path inside a bundle.
func checkBundlePath(p string) error {
	switch {
	case p == "":
		return errors.New("empty path")
	case len(p) > bundleMaxPathBytes:
		return fmt.Errorf("%s: path longer than %d bytes", p, bundleMaxPathBytes)
	case strings.Contains(p, `\`):
		return fmt.Errorf("%s: paths use / only", p)
	case strings.HasPrefix(p, "/"):
		return fmt.Errorf("%s: absolute path", p)
	case len(p) >= 2 && p[1] == ':':
		return fmt.Errorf("%s: drive path", p)
	case strings.ContainsRune(p, 0):
		return fmt.Errorf("%q: NUL in path", p)
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return fmt.Errorf("%s: empty, . or .. path segment", p)
		}
	}
	return nil
}

// skipInBundle is true for dotfiles, dot folders and node_modules.
func skipInBundle(name string) bool {
	return strings.HasPrefix(name, ".") || name == "node_modules"
}

// sharedFolders are the folders of dir whose names start with _, in name order.
func sharedFolders(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() && strings.HasPrefix(e.Name(), "_") {
			out = append(out, e.Name())
		}
	}
	return out, nil
}

// functionFolders are the function folders of dir (not _shared ones, not dot folders), in name order.
func functionFolders(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() && !strings.HasPrefix(e.Name(), "_") && !skipInBundle(e.Name()) {
			out = append(out, e.Name())
		}
	}
	return out, nil
}

func findEntrypoint(fnDir string) (string, error) {
	for _, name := range functionEntrypoints {
		if st, err := os.Stat(filepath.Join(fnDir, name)); err == nil && st.Mode().IsRegular() {
			return name, nil
		}
	}
	return "", fmt.Errorf("%s has no entrypoint: add one of %s", fnDir, strings.Join(functionEntrypoints, ", "))
}

// buildFunctionBundle zips dir/<name>/ at <name>/... and every dir/_*/ at its
// own name. maxBytes is the plan's bundle size (0: not checked here).
func buildFunctionBundle(dir, name string, maxBytes int64) (*functionBundle, error) {
	if err := validateFunctionName(name); err != nil {
		return nil, err
	}
	fnDir := filepath.Join(dir, name)
	if st, err := os.Stat(fnDir); err != nil || !st.IsDir() {
		return nil, fmt.Errorf("function folder %s not found (create it with: arianet baas functions new %s)", fnDir, name)
	}
	entry, err := findEntrypoint(fnDir)
	if err != nil {
		return nil, err
	}
	roots := []string{name}
	shared, err := sharedFolders(dir)
	if err != nil {
		return nil, err
	}
	roots = append(roots, shared...)

	type file struct{ zipPath, local string }
	var files []file
	var total int64
	for _, root := range roots {
		base := filepath.Join(dir, root)
		err := filepath.WalkDir(base, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if p != base && skipInBundle(d.Name()) {
				if d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			if d.IsDir() {
				return nil
			}
			if !d.Type().IsRegular() {
				return fmt.Errorf("%s is not a regular file (symbolic links are not bundled)", p)
			}
			rel, err := filepath.Rel(dir, p)
			if err != nil {
				return err
			}
			zp := filepath.ToSlash(rel)
			if err := checkBundlePath(zp); err != nil {
				return err
			}
			info, err := d.Info()
			if err != nil {
				return err
			}
			total += info.Size()
			files = append(files, file{zipPath: zp, local: p})
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	sort.Slice(files, func(i, j int) bool { return files[i].zipPath < files[j].zipPath })
	if len(files) > bundleMaxFiles {
		return nil, fmt.Errorf("the bundle of %s has %d files; at most %d are allowed", name, len(files), bundleMaxFiles)
	}
	if maxBytes > 0 && total > bundleExpandFactor*maxBytes {
		return nil, fmt.Errorf("the files of %s are %d bytes; at most %d (%d times the plan's bundle size) are allowed",
			name, total, bundleExpandFactor*maxBytes, bundleExpandFactor)
	}

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	out := &functionBundle{Entrypoint: name + "/" + entry}
	for _, f := range files {
		h := &zip.FileHeader{Name: f.zipPath, Method: zip.Deflate, Modified: bundleTime}
		h.SetMode(0o644)
		w, err := zw.CreateHeader(h)
		if err != nil {
			return nil, err
		}
		src, err := os.Open(f.local)
		if err != nil {
			return nil, err
		}
		_, err = io.Copy(w, src)
		_ = src.Close()
		if err != nil {
			return nil, err
		}
		out.Files = append(out.Files, f.zipPath)
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	if maxBytes > 0 && int64(buf.Len()) > maxBytes {
		return nil, fmt.Errorf("the bundle of %s is %d bytes; the plan allows %d", name, buf.Len(), maxBytes)
	}
	out.Zip = buf.Bytes()
	return out, nil
}

// ─── download extraction ─────────────────────────────────────────────────────

// extractTarget is where a bundle entry goes under dir, refusing any path
// that would leave it.
func extractTarget(dir, name string) (string, error) {
	if err := checkBundlePath(name); err != nil {
		return "", fmt.Errorf("unsafe path in the bundle: %w", err)
	}
	target := filepath.Join(dir, filepath.FromSlash(name))
	rel, err := filepath.Rel(dir, target)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", fmt.Errorf("unsafe path in the bundle: %s", name)
	}
	return target, nil
}

// extractFunctionBundle unpacks a downloaded bundle into dir. A bundle whose
// paths are not under <name>/ or a _shared folder (one deployed from the
// console's editor) is unpacked into dir/<name>/. Existing files are refused
// unless force; nothing is written when a path is refused.
func extractFunctionBundle(data []byte, dir, name string, force bool) ([]string, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("the downloaded bundle is not a zip: %w", err)
	}
	prefix := ""
	for _, f := range zr.File {
		if err := checkBundlePath(strings.TrimSuffix(f.Name, "/")); err != nil {
			return nil, fmt.Errorf("unsafe path in the bundle: %w", err)
		}
	}
	for _, f := range zr.File {
		first, _, _ := strings.Cut(strings.TrimSuffix(f.Name, "/"), "/")
		if first != name && !strings.HasPrefix(first, "_") {
			prefix = name + "/"
			break
		}
	}
	type entry struct {
		f      *zip.File
		target string
	}
	var entries []entry
	var exist []string
	for _, f := range zr.File {
		if strings.HasSuffix(f.Name, "/") {
			if _, err := extractTarget(dir, strings.TrimSuffix(prefix+f.Name, "/")); err != nil {
				return nil, err
			}
			continue
		}
		if !f.Mode().IsRegular() {
			return nil, fmt.Errorf("unsafe entry in the bundle: %s is not a regular file", f.Name)
		}
		target, err := extractTarget(dir, prefix+f.Name)
		if err != nil {
			return nil, err
		}
		if _, err := os.Lstat(target); err == nil {
			exist = append(exist, target)
		}
		entries = append(entries, entry{f, target})
	}
	if len(exist) > 0 && !force {
		return nil, fmt.Errorf("these files exist: %s\nnothing was written; add --force to overwrite them", strings.Join(exist, ", "))
	}
	var written []string
	var total int64
	for _, e := range entries {
		if err := os.MkdirAll(filepath.Dir(e.target), 0o755); err != nil {
			return written, err
		}
		if st, err := os.Lstat(e.target); err == nil && !st.Mode().IsRegular() {
			return written, fmt.Errorf("%s exists and is not a regular file", e.target)
		}
		src, err := e.f.Open()
		if err != nil {
			return written, err
		}
		dst, err := os.OpenFile(e.target, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
		if err != nil {
			_ = src.Close()
			return written, err
		}
		n, err := io.Copy(dst, io.LimitReader(src, maxDownloadBytes-total+1))
		_ = src.Close()
		if cerr := dst.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			return written, err
		}
		if total += n; total > maxDownloadBytes {
			return written, fmt.Errorf("the bundle unpacks to more than %d bytes", maxDownloadBytes)
		}
		written = append(written, e.target)
	}
	return written, nil
}

// ─── env files ───────────────────────────────────────────────────────────────

// readEnvFile reads NAME=VALUE lines: blank lines and # comments are
// skipped, "export " is allowed, and a value in single or double quotes is
// unquoted. The order of the file is kept.
func readEnvFile(file string) ([][2]string, error) {
	f, err := os.Open(file)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	var out [][2]string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), maxSecretValueBytes+4096)
	line := 0
	for sc.Scan() {
		line++
		text := strings.TrimSpace(sc.Text())
		if text == "" || strings.HasPrefix(text, "#") {
			continue
		}
		text = strings.TrimPrefix(text, "export ")
		name, value, ok := strings.Cut(text, "=")
		name = strings.TrimSpace(name)
		if !ok || name == "" {
			return nil, fmt.Errorf("%s:%d: not NAME=VALUE", file, line)
		}
		value = strings.TrimSpace(value)
		if len(value) >= 2 && (value[0] == '"' || value[0] == '\'') && value[len(value)-1] == value[0] {
			quote := value[0]
			value = value[1 : len(value)-1]
			if quote == '"' {
				value = strings.NewReplacer(`\n`, "\n", `\"`, `"`, `\\`, `\`).Replace(value)
			}
		}
		out = append(out, [2]string{name, value})
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("%s: %w", file, err)
	}
	return out, nil
}

// ─── commands ────────────────────────────────────────────────────────────────

func (p *product) baasFunctionsCmd() *cobra.Command {
	var dir string
	cmd := &cobra.Command{
		Use:     "functions",
		Aliases: []string{"function", "fn"},
		Short:   "Write, run, deploy and manage the project's edge functions",
		Long: `Edge functions are TypeScript or JavaScript handlers (Deno.serve) that run next to the
project and answer at https://<ref>.baas.arianet.app/functions/v1/<name>. The code lives in
` + defaultFunctionsDir + `/<name>/index.ts; folders whose names start with _ (e.g. _shared/)
hold code every function may import and are deployed with each of them.

By default a call needs a project JWT in Authorization: Bearer (the anon key, the service key or
a signed-in user's token); deploy --no-verify-jwt lets anyone call it (webhooks). Every function
gets ARIANET_BAAS_URL, ARIANET_BAAS_ANON_KEY, ARIANET_BAAS_DB_URL and the project's secrets as
environment variables. Limits (functions, bundle size, invocations a month) come from the plan.

Deploy, delete, download and secrets changes need the baas:functions:manage scope.`,
		Example: `  arianet baas functions new hello-world
  arianet baas functions serve hello-world
  arianet baas functions deploy 12 hello-world
  arianet baas functions secrets set 12 STRIPE_KEY=sk_live_123`,
	}
	cmd.PersistentFlags().StringVar(&dir, "dir", defaultFunctionsDir, "the folder holding the function folders")
	dirOf := func() string { return dir }

	cmd.AddCommand(
		p.baasFunctionsNewCmd(dirOf),
		p.baasFunctionsListCmd(),
		p.command(endpoint{use: "get <project-id> <name>", aliases: []string{"show"}, short: "Show one function and its latest versions",
			method: http.MethodGet, path: "/projects/{0}/functions/{1}", nargs: 2,
			cols: []col{{header: "Name", key: "name"}, {header: "Version", key: "version"}, {header: "Verify JWT", key: "verify_jwt"},
				{header: "Entrypoint", key: "entrypoint"}, {header: "Size", key: "size_bytes"}, {header: "SHA-256", key: "sha256"},
				{header: "URL", key: "url"}, {header: "Created", key: "created_at"}, {header: "Deployed", key: "deployed_at"}}}),
		p.baasFunctionsDeployCmd(dirOf),
		p.command(endpoint{use: "delete <project-id> <name>", aliases: []string{"rm"}, short: "Delete a function and all its versions",
			method: http.MethodDelete, path: "/projects/{0}/functions/{1}", nargs: 2,
			confirm: "Delete function {1} of project #{0}? Its URL stops answering.", done: "Function {1} deleted."}),
		p.baasFunctionsDownloadCmd(dirOf),
		p.baasFunctionsSecretsCmd(),
		p.baasFunctionsServeCmd(dirOf),
	)
	return cmd
}

const functionTemplate = `// Edge function "%[1]s" of an Arianet BaaS project.
//
// Run it on your machine:  arianet baas functions serve %[1]s
// Deploy it:               arianet baas functions deploy <project-id> %[1]s
//
// The platform sets ARIANET_BAAS_URL, ARIANET_BAAS_ANON_KEY and ARIANET_BAAS_DB_URL, and adds
// your secrets (arianet baas functions secrets set) as environment variables.

const projectUrl = Deno.env.get("ARIANET_BAAS_URL") ?? "(not set)";

Deno.serve(async (req: Request) => {
  let name = "world";
  try {
    const body = await req.json();
    if (typeof body?.name === "string" && body.name !== "") {
      name = body.name;
    }
  } catch {
    // No JSON body: greet the world.
  }
  console.log(` + "`hello from %[1]s, project ${projectUrl}`" + `);
  return new Response(JSON.stringify({ message: ` + "`Hello ${name}!`" + ` }), {
    headers: { "Content-Type": "application/json" },
  });
});
`

func (p *product) baasFunctionsNewCmd(dirOf func() string) *cobra.Command {
	return &cobra.Command{
		Use:     "new <name>",
		Aliases: []string{"create"},
		Short:   "Create " + defaultFunctionsDir + "/<name>/index.ts from a template",
		Example: "  arianet baas functions new hello-world",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := strings.TrimSpace(args[0])
			if err := validateFunctionName(name); err != nil {
				return err
			}
			fnDir := filepath.Join(dirOf(), name)
			if _, err := os.Lstat(fnDir); err == nil {
				return fmt.Errorf("%s exists; choose another name or edit the code there", fnDir)
			}
			if err := os.MkdirAll(fnDir, 0o755); err != nil {
				return err
			}
			file := filepath.Join(fnDir, "index.ts")
			if err := os.WriteFile(file, []byte(fmt.Sprintf(functionTemplate, name)), 0o644); err != nil {
				return err
			}
			printer.Success(fmt.Sprintf("Created %s.", file))
			printer.Info("Next steps:")
			printer.Info(fmt.Sprintf("  arianet baas functions serve %s                 # run it on your machine (needs Deno)", name))
			printer.Info(fmt.Sprintf("  arianet baas functions deploy <project-id> %s   # deploy it", name))
			return nil
		},
	}
}

func renderFunctionsList(raw []byte) {
	var resp struct {
		Limits map[string]any `json:"limits"`
		Usage  map[string]any `json:"usage"`
		Data   []any          `json:"data"`
	}
	printAnswer(raw, baasFunctionCols)
	if json.Unmarshal(raw, &resp) != nil || resp.Limits == nil {
		return
	}
	line := fmt.Sprintf("Functions: %d of %s. Invocations this month: %s of %s", len(resp.Data),
		cell(resp.Limits["max_functions"]), cell(resp.Usage["invocations_month"]), cell(resp.Limits["invocations_month"]))
	if hard, _ := resp.Limits["hard"].(bool); hard {
		line += " (calls beyond it are refused until the month ends)"
	}
	printer.Info(line + ".")
}

func (p *product) baasFunctionsListCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "list <project-id>",
		Aliases: []string{"ls"},
		Short:   "List the project's functions with the plan's limits and this month's use",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := projectID(args[0])
			if err != nil {
				return err
			}
			cc := p.client()
			if cc == nil {
				return nil
			}
			var raw json.RawMessage
			if err := cc.Get("/projects/"+id+"/functions", nil, &raw); err != nil {
				handleAPIError(err)
				return nil
			}
			renderFunctionsList(raw)
			return nil
		},
	}
}

func (p *product) baasFunctionsDeployCmd(dirOf func() string) *cobra.Command {
	var noVerifyJWT, verifyJWT bool
	cmd := &cobra.Command{
		Use:   "deploy <project-id> [name...]",
		Short: "Bundle and deploy functions (every function folder when no name is given)",
		Long: `Zip ` + defaultFunctionsDir + `/<name>/ (at <name>/...) together with every folder whose name starts
with _ (shared code, e.g. _shared/), and deploy it as the function's new active version. The
entrypoint is <name>/index.ts, or index.js, index.mjs, index.tsx or index.jsx. Dotfiles, dot
folders and node_modules are left out. A new function needs a JWT by default; --no-verify-jwt
lets anyone call it (webhooks), --verify-jwt turns the check back on. Without either flag an
existing function keeps its setting. Needs the baas:functions:manage scope.`,
		Example: `  arianet baas functions deploy 12 hello-world
  arianet baas functions deploy 12 stripe-webhook --no-verify-jwt
  arianet baas functions deploy 12`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := projectID(args[0])
			if err != nil {
				return err
			}
			if noVerifyJWT && verifyJWT {
				return fmt.Errorf("--verify-jwt and --no-verify-jwt cannot be combined")
			}
			dir := dirOf()
			names := args[1:]
			if len(names) == 0 {
				if names, err = functionFolders(dir); err != nil {
					return fmt.Errorf("no functions folder: %w", err)
				}
				if len(names) == 0 {
					return fmt.Errorf("%s has no function folders (create one with: arianet baas functions new <name>)", dir)
				}
			}
			for _, n := range names {
				if err := validateFunctionName(n); err != nil {
					return err
				}
			}
			cc := p.client()
			if cc == nil {
				return nil
			}
			// The plan's bundle size, checked here before uploading.
			var list struct {
				Limits struct {
					BundleBytes int64 `json:"bundle_bytes"`
				} `json:"limits"`
			}
			if err := cc.Get("/projects/"+id+"/functions", nil, &list); err != nil {
				handleAPIError(err)
				return nil
			}
			bundles := make([]*functionBundle, len(names))
			for i, n := range names {
				if bundles[i], err = buildFunctionBundle(dir, n, list.Limits.BundleBytes); err != nil {
					return err
				}
			}
			var deployed []any
			for i, n := range names {
				b := bundles[i]
				q := url.Values{"entrypoint": {b.Entrypoint}}
				if noVerifyJWT {
					q.Set("verify_jwt", "false")
				}
				if verifyJWT {
					q.Set("verify_jwt", "true")
				}
				var raw json.RawMessage
				if err := cc.Upload(http.MethodPost, functionPath(id, n)+"/deploy", q, bytes.NewReader(b.Zip), int64(len(b.Zip)), "application/zip", &raw); err != nil {
					if i > 0 {
						fmt.Fprintf(os.Stderr, "Deploying %s failed (%d function(s) before it were deployed).\n", n, i)
					}
					handleAPIError(err)
					return nil
				}
				var out struct {
					Data map[string]any `json:"data"`
				}
				_ = json.Unmarshal(raw, &out)
				deployed = append(deployed, out.Data)
				if !jsonMode() {
					printer.Success(fmt.Sprintf("Deployed %s version %s (%d files, %d bytes): %s", n, cell(out.Data["version"]),
						len(b.Files), len(b.Zip), cell(out.Data["url"])))
				}
			}
			if jsonMode() {
				printer.New(cfg.Output).PrintJSON(map[string]any{"data": deployed})
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&noVerifyJWT, "no-verify-jwt", false, "let anyone call the function, without a JWT")
	cmd.Flags().BoolVar(&verifyJWT, "verify-jwt", false, "require a project JWT again")
	return cmd
}

func (p *product) baasFunctionsDownloadCmd(dirOf func() string) *cobra.Command {
	var version int
	var force bool
	cmd := &cobra.Command{
		Use:   "download <project-id> <name>",
		Short: "Download a deployed function's code into " + defaultFunctionsDir + "/",
		Long: `Download the bundle of the active version (or --version) and unpack it into the functions
folder: <name>/... and the shared _ folders it was deployed with. Existing files are not
overwritten unless --force. Needs the baas:functions:manage scope.`,
		Example: `  arianet baas functions download 12 hello-world
  arianet baas functions download 12 hello-world --version 2 --force`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := projectID(args[0])
			if err != nil {
				return err
			}
			name := strings.TrimSpace(args[1])
			if err := validateFunctionName(name); err != nil {
				return err
			}
			q := url.Values{}
			if version > 0 {
				q.Set("version", strconv.Itoa(version))
			}
			cc := p.client()
			if cc == nil {
				return nil
			}
			var buf limitedBuffer
			buf.max = maxDownloadBytes
			if _, err := cc.Download(functionPath(id, name)+"/body", q, &buf); err != nil {
				handleAPIError(err)
				return nil
			}
			written, err := extractFunctionBundle(buf.Bytes(), dirOf(), name, force)
			if err != nil {
				return err
			}
			if jsonMode() {
				printer.New(cfg.Output).PrintJSON(map[string]any{"files": written})
				return nil
			}
			printer.Success(fmt.Sprintf("Downloaded %s into %s (%d file(s)).", name, dirOf(), len(written)))
			for _, w := range written {
				printer.Info("  " + w)
			}
			return nil
		},
	}
	cmd.Flags().IntVar(&version, "version", 0, "this version instead of the active one")
	cmd.Flags().BoolVar(&force, "force", false, "overwrite files that exist")
	return cmd
}

// limitedBuffer refuses to grow past max bytes.
type limitedBuffer struct {
	bytes.Buffer
	max int
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if b.Len()+len(p) > b.max {
		return 0, fmt.Errorf("the bundle is larger than %d bytes", b.max)
	}
	return b.Buffer.Write(p)
}

func (p *product) baasFunctionsSecretsCmd() *cobra.Command {
	secretsPath := func(id string) string { return "/projects/" + id + "/functions/secrets" }

	list := &cobra.Command{
		Use:     "list <project-id>",
		Aliases: []string{"ls"},
		Short:   "List the secrets' names and digests (values are never shown)",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := projectID(args[0])
			if err != nil {
				return err
			}
			cc := p.client()
			if cc == nil {
				return nil
			}
			var raw json.RawMessage
			if err := cc.Get(secretsPath(id), nil, &raw); err != nil {
				handleAPIError(err)
				return nil
			}
			printAnswer(raw, baasSecretCols)
			return nil
		},
	}

	var envFile string
	set := &cobra.Command{
		Use:   "set <project-id> [NAME=VALUE...]",
		Short: "Create or replace secrets (functions read them as environment variables)",
		Long: `Create or replace secrets: give NAME=VALUE pairs, an --env-file of NAME=VALUE lines, or both
(the arguments win). Names are letters, digits and _, not starting with a digit or with ARIANET_;
a value is at most 64 KiB. Running functions see the change within about 30 seconds. Needs the
baas:functions:manage scope.`,
		Example: `  arianet baas functions secrets set 12 STRIPE_KEY=sk_live_123 MAIL_FROM=noreply@example.com
  arianet baas functions secrets set 12 --env-file arianet/functions/.env`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := projectID(args[0])
			if err != nil {
				return err
			}
			var pairs [][2]string
			if envFile != "" {
				if pairs, err = readEnvFile(envFile); err != nil {
					return err
				}
			}
			for _, a := range args[1:] {
				name, value, ok := strings.Cut(a, "=")
				if !ok {
					return fmt.Errorf("%q is not NAME=VALUE", a)
				}
				pairs = append(pairs, [2]string{strings.TrimSpace(name), value})
			}
			if len(pairs) == 0 {
				return fmt.Errorf("no secrets given: pass NAME=VALUE or --env-file <file>")
			}
			secrets := map[string]string{}
			var order []string
			for _, pv := range pairs {
				if err := validateSecretName(pv[0]); err != nil {
					return err
				}
				if len(pv[1]) > maxSecretValueBytes {
					return fmt.Errorf("the value of %s is %d bytes; at most %d are allowed", pv[0], len(pv[1]), maxSecretValueBytes)
				}
				if _, seen := secrets[pv[0]]; !seen {
					order = append(order, pv[0])
				}
				secrets[pv[0]] = pv[1]
			}
			cc := p.client()
			if cc == nil {
				return nil
			}
			var raw json.RawMessage
			for start := 0; start < len(order); start += maxSecretsPerCall {
				end := min(start+maxSecretsPerCall, len(order))
				batch := map[string]string{}
				for _, n := range order[start:end] {
					batch[n] = secrets[n]
				}
				if err := cc.Put(secretsPath(id), map[string]any{"secrets": batch}, &raw); err != nil {
					handleAPIError(err)
					return nil
				}
			}
			if jsonMode() {
				printRawJSON(raw)
				return nil
			}
			printer.Success(fmt.Sprintf("Set %d secret(s): %s.", len(order), strings.Join(order, ", ")))
			printAnswer(raw, baasSecretCols)
			return nil
		},
	}
	set.Flags().StringVar(&envFile, "env-file", "", "read NAME=VALUE lines from this file")

	unset := &cobra.Command{
		Use:     "unset <project-id> <NAME>...",
		Aliases: []string{"delete", "rm"},
		Short:   "Delete secrets",
		Example: "  arianet baas functions secrets unset 12 STRIPE_KEY --yes",
		Args:    cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := projectID(args[0])
			if err != nil {
				return err
			}
			names := args[1:]
			for _, n := range names {
				if err := validateSecretName(n); err != nil {
					return err
				}
			}
			yes, _ := cmd.Flags().GetBool("yes")
			if !yes && !confirm(fmt.Sprintf("Delete secret(s) %s of project #%s?", strings.Join(names, ", "), id)) {
				printer.Info("Cancelled.")
				return nil
			}
			cc := p.client()
			if cc == nil {
				return nil
			}
			for _, n := range names {
				if err := cc.Delete(secretsPath(id)+"/"+url.PathEscape(n), nil, nil, nil); err != nil {
					handleAPIError(err)
					return nil
				}
			}
			if jsonMode() {
				printer.New(cfg.Output).PrintJSON(map[string]any{"deleted": names})
				return nil
			}
			printer.Success(fmt.Sprintf("Deleted %d secret(s): %s.", len(names), strings.Join(names, ", ")))
			return nil
		},
	}
	unset.Flags().BoolP("yes", "y", false, "skip the confirmation prompt")

	return subgroup("secrets", "Manage the secrets every function of the project reads as environment variables", []string{"secret"}, list, set, unset)
}

// lookDeno finds the deno binary; tests replace it.
var lookDeno = func() (string, error) { return exec.LookPath("deno") }

const denoInstallHelp = `serve runs the function with Deno, which is not installed (deno is not on your PATH).
Install it, then run the command again:

  macOS / Linux:  curl -fsSL https://deno.land/install.sh | sh
  Homebrew:       brew install deno
  Windows:        irm https://deno.land/install.ps1 | iex

More: https://docs.deno.com/runtime/getting_started/installation/`

// serveEnv is the environment of a local run: ours, then the env file, then the port.
func serveEnv(base []string, file [][2]string, port int) []string {
	env := append([]string(nil), base...)
	for _, kv := range file {
		env = append(env, kv[0]+"="+kv[1])
	}
	return append(env, "PORT="+strconv.Itoa(port), "DENO_SERVE_PORT="+strconv.Itoa(port))
}

func (p *product) baasFunctionsServeCmd(dirOf func() string) *cobra.Command {
	var envFile string
	var port int
	cmd := &cobra.Command{
		Use:   "serve [name]",
		Short: "Run a function on your machine with Deno, restarting on every change",
		Long: `Run ` + defaultFunctionsDir + `/<name>/index.ts with deno run --watch (network, environment and file
read access). The name may be left out when there is only one function. The environment is yours
plus --env-file (default ` + defaultFunctionsDir + `/.env when it exists; put ARIANET_BAAS_URL,
ARIANET_BAAS_ANON_KEY and your secrets there) plus PORT and DENO_SERVE_PORT. Deno.serve listens on
port 8000 unless the code passes another port. No platform features (JWT check, logs) run locally.`,
		Example: `  arianet baas functions serve hello-world
  curl -X POST http://localhost:8000 -H 'Content-Type: application/json' -d '{"name":"Ana"}'`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir := dirOf()
			var name string
			if len(args) == 1 {
				name = strings.TrimSpace(args[0])
			} else {
				names, err := functionFolders(dir)
				if err != nil {
					return fmt.Errorf("no functions folder: %w", err)
				}
				if len(names) != 1 {
					return fmt.Errorf("%s has %d function folders; name the one to serve", dir, len(names))
				}
				name = names[0]
			}
			if err := validateFunctionName(name); err != nil {
				return err
			}
			fnDir := filepath.Join(dir, name)
			entry, err := findEntrypoint(fnDir)
			if err != nil {
				return err
			}
			deno, err := lookDeno()
			if err != nil {
				fmt.Fprintln(os.Stderr, denoInstallHelp)
				return fmt.Errorf("deno is not installed")
			}
			file := envFile
			if file == "" {
				if st, err := os.Stat(filepath.Join(dir, ".env")); err == nil && st.Mode().IsRegular() {
					file = filepath.Join(dir, ".env")
				}
			}
			var vars [][2]string
			if file != "" {
				if vars, err = readEnvFile(file); err != nil {
					return err
				}
			}
			run := exec.Command(deno, "run", "--allow-net", "--allow-env", "--allow-read", "--watch", path.Join(filepath.ToSlash(fnDir), entry))
			run.Env = serveEnv(os.Environ(), vars, port)
			run.Stdin, run.Stdout, run.Stderr = os.Stdin, os.Stdout, os.Stderr
			fmt.Fprintf(os.Stderr, "Serving %s at http://localhost:%d/ (Ctrl+C stops it)\n", name, port)
			if file != "" {
				fmt.Fprintf(os.Stderr, "Environment from %s (%d variable(s))\n", file, len(vars))
			}
			if port != 8000 {
				fmt.Fprintf(os.Stderr, "Note: Deno.serve uses port %d only when the code passes it, e.g. Deno.serve({ port: Number(Deno.env.get(\"PORT\")) }, handler)\n", port)
			}
			return run.Run()
		},
	}
	cmd.Flags().StringVar(&envFile, "env-file", "", "environment variables for the run (default: "+defaultFunctionsDir+"/.env when it exists)")
	cmd.Flags().IntVar(&port, "port", 8000, "the port to announce and pass as PORT")
	return cmd
}
