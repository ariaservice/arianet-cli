package commands

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ariaservice/arianet-cli/internal/api"
)

func writeTree(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for name, text := range files {
		p := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func zipNames(t *testing.T, data []byte) map[string]string {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(rc)
		_ = rc.Close()
		out[f.Name] = string(b)
	}
	return out
}

func makeZip(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, text := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = io.WriteString(w, text)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestFunctionNameValidation(t *testing.T) {
	for _, ok := range []string{"hello", "hello-world", "a", "0x_y", strings.Repeat("a", 63)} {
		if err := validateFunctionName(ok); err != nil {
			t.Errorf("%q refused: %v", ok, err)
		}
	}
	for _, bad := range []string{"", "Hello", "-a", "_shared", "a/b", "a.b", strings.Repeat("a", 64), "secrets", "config", "../x"} {
		if err := validateFunctionName(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestSecretNameValidation(t *testing.T) {
	for _, ok := range []string{"API_KEY", "_x", "a1", strings.Repeat("A", 128)} {
		if err := validateSecretName(ok); err != nil {
			t.Errorf("%q refused: %v", ok, err)
		}
	}
	for _, bad := range []string{"", "1A", "A-B", "A B", "ARIANET_X", "arianet_baas_url", strings.Repeat("A", 129)} {
		if err := validateSecretName(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestBuildFunctionBundleLayout(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{
		"hello/index.ts":                   "import { cors } from '../_shared/cors.ts'",
		"hello/lib/util.ts":                "export const x = 1",
		"hello/.env":                       "SECRET=1",
		"hello/.cache/x":                   "skip",
		"hello/node_modules/pkg/index.js":  "skip",
		"_shared/cors.ts":                  "export const cors = {}",
		"_shared/.DS_Store":                "skip",
		"_utils/db.ts":                     "export {}",
		"other/index.ts":                   "not bundled",
		".env":                             "skip",
		"hello/README.md":                  "kept",
		"_shared/node_modules/a/index.js":  "skip",
		"hello/deep/er/still/fine/file.ts": "ok",
	})
	b, err := buildFunctionBundle(dir, "hello", 0)
	if err != nil {
		t.Fatal(err)
	}
	if b.Entrypoint != "hello/index.ts" {
		t.Errorf("entrypoint %q", b.Entrypoint)
	}
	want := "_shared/cors.ts,_utils/db.ts,hello/README.md,hello/deep/er/still/fine/file.ts,hello/index.ts,hello/lib/util.ts"
	if strings.Join(b.Files, ",") != want {
		t.Errorf("files %v", b.Files)
	}
	got := zipNames(t, b.Zip)
	if len(got) != 6 || got["_shared/cors.ts"] != "export const cors = {}" || got["hello/lib/util.ts"] != "export const x = 1" {
		t.Errorf("zip %v", got)
	}
	// Deterministic: the same files give the same bytes.
	again, err := buildFunctionBundle(dir, "hello", 0)
	if err != nil || !bytes.Equal(again.Zip, b.Zip) {
		t.Error("the bundle is not deterministic")
	}
}

func TestBuildFunctionBundleEntrypointFallbackAndErrors(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{"js/index.mjs": "x", "js/index.jsx": "y", "none/main.ts": "z"})
	b, err := buildFunctionBundle(dir, "js", 0)
	if err != nil || b.Entrypoint != "js/index.mjs" {
		t.Errorf("entrypoint %v %v", b, err)
	}
	if _, err := buildFunctionBundle(dir, "none", 0); err == nil || !strings.Contains(err.Error(), "no entrypoint") {
		t.Errorf("no entrypoint: %v", err)
	}
	if _, err := buildFunctionBundle(dir, "missing", 0); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("missing: %v", err)
	}
	if _, err := buildFunctionBundle(dir, "secrets", 0); err == nil || !strings.Contains(err.Error(), "reserved") {
		t.Errorf("reserved: %v", err)
	}
}

func TestBuildFunctionBundleLimits(t *testing.T) {
	dir := t.TempDir()
	big := strings.Repeat("random-ish text that still compresses ", 200)
	writeTree(t, dir, map[string]string{"f/index.ts": big})
	if _, err := buildFunctionBundle(dir, "f", 50); err == nil || !strings.Contains(err.Error(), "times the plan's bundle size") {
		t.Errorf("uncompressed limit: %v", err)
	}
	// Fits uncompressed (5x) but not compressed: random bytes do not shrink.
	noise := make([]byte, 2000)
	_, _ = rand.New(rand.NewSource(1)).Read(noise)
	writeTree(t, dir, map[string]string{"g/index.ts": string(noise)})
	if _, err := buildFunctionBundle(dir, "g", 500); err == nil || !strings.Contains(err.Error(), "the plan allows") {
		t.Errorf("compressed limit: %v", err)
	}
	if _, err := buildFunctionBundle(dir, "f", 1<<20); err != nil {
		t.Errorf("within the limit: %v", err)
	}

	many := map[string]string{"m/index.ts": "x"}
	for i := 0; i < bundleMaxFiles; i++ {
		many[fmt.Sprintf("m/files/f%04d.ts", i)] = "x"
	}
	writeTree(t, dir, many)
	if _, err := buildFunctionBundle(dir, "m", 0); err == nil || !strings.Contains(err.Error(), "at most 1000") {
		t.Errorf("file count: %v", err)
	}

	long := t.TempDir()
	writeTree(t, long, map[string]string{"l/index.ts": "x", "l/" + strings.Repeat("d", 120) + "/" + strings.Repeat("e", 140) + ".ts": "x"})
	if _, err := buildFunctionBundle(long, "l", 0); err == nil || !strings.Contains(err.Error(), "longer than 255") {
		t.Errorf("path length: %v", err)
	}
}

func TestBuildFunctionBundleRefusesSymlinks(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{"s/index.ts": "x", "secret.txt": "outside"})
	if err := os.Symlink(filepath.Join(dir, "secret.txt"), filepath.Join(dir, "s", "link.txt")); err != nil {
		t.Skip("symlinks unavailable:", err)
	}
	if _, err := buildFunctionBundle(dir, "s", 0); err == nil || !strings.Contains(err.Error(), "symbolic links") {
		t.Errorf("symlink: %v", err)
	}
}

func TestCheckBundlePath(t *testing.T) {
	for _, ok := range []string{"a/index.ts", "_shared/x.ts", "a/b/c.json"} {
		if err := checkBundlePath(ok); err != nil {
			t.Errorf("%q: %v", ok, err)
		}
	}
	for _, bad := range []string{"", "/etc/passwd", "../x", "a/../../x", `a\b`, "C:/x", "a//b", "./a", "a/./b", strings.Repeat("x", 256)} {
		if err := checkBundlePath(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestExtractFunctionBundle(t *testing.T) {
	dir := t.TempDir()
	data := makeZip(t, map[string]string{"hello/index.ts": "v2", "hello/lib/a.ts": "a", "_shared/cors.ts": "c"})
	written, err := extractFunctionBundle(data, dir, "hello", false)
	if err != nil || len(written) != 3 {
		t.Fatalf("written %v (%v)", written, err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "_shared", "cors.ts")); string(b) != "c" {
		t.Errorf("shared %q", b)
	}
	// Again: refused without --force, and nothing is touched.
	_ = os.WriteFile(filepath.Join(dir, "hello", "index.ts"), []byte("local edit"), 0o600)
	if _, err := extractFunctionBundle(data, dir, "hello", false); err == nil || !strings.Contains(err.Error(), "--force") {
		t.Fatalf("overwrite: %v", err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "hello", "index.ts")); string(b) != "local edit" {
		t.Errorf("a refused download changed a file: %q", b)
	}
	if _, err := extractFunctionBundle(data, dir, "hello", true); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "hello", "index.ts")); string(b) != "v2" {
		t.Errorf("forced %q", b)
	}
}

func TestExtractFunctionBundleFromTheEditorGoesUnderTheName(t *testing.T) {
	dir := t.TempDir()
	data := makeZip(t, map[string]string{"index.ts": "x", "util.ts": "y"})
	if _, err := extractFunctionBundle(data, dir, "web", false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "web", "index.ts")); err != nil {
		t.Error(err)
	}
}

func TestExtractFunctionBundleZipSlip(t *testing.T) {
	for _, evil := range []string{"../evil.ts", "hello/../../evil.ts", "/tmp/evil.ts", `hello\..\..\evil.ts`, "C:/evil.ts"} {
		parent := t.TempDir()
		dir := filepath.Join(parent, "functions")
		data := makeZip(t, map[string]string{"hello/index.ts": "ok", evil: "pwned"})
		if _, err := extractFunctionBundle(data, dir, "hello", true); err == nil || !strings.Contains(err.Error(), "unsafe") {
			t.Errorf("%q: %v", evil, err)
		}
		if _, err := os.Stat(filepath.Join(parent, "evil.ts")); err == nil {
			t.Errorf("%q escaped the folder", evil)
		}
		if _, err := os.Stat(filepath.Join(dir, "hello", "index.ts")); err == nil {
			t.Errorf("%q: nothing may be written when a path is refused", evil)
		}
	}

	// A symbolic link entry is refused.
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	h := &zip.FileHeader{Name: "hello/link"}
	h.SetMode(os.ModeSymlink | 0o777)
	w, _ := zw.CreateHeader(h)
	_, _ = io.WriteString(w, "/etc/passwd")
	_ = zw.Close()
	if _, err := extractFunctionBundle(buf.Bytes(), t.TempDir(), "hello", true); err == nil || !strings.Contains(err.Error(), "not a regular file") {
		t.Errorf("symlink entry: %v", err)
	}
}

func TestReadEnvFile(t *testing.T) {
	file := filepath.Join(t.TempDir(), ".env")
	_ = os.WriteFile(file, []byte("# comment\n\nA=1\nexport B = two words \nC=\"x\\ny\"\nD='raw\\n'\nE=a=b\n"), 0o600)
	got, err := readEnvFile(file)
	if err != nil {
		t.Fatal(err)
	}
	want := [][2]string{{"A", "1"}, {"B", "two words"}, {"C", "x\ny"}, {"D", `raw\n`}, {"E", "a=b"}}
	if len(got) != len(want) {
		t.Fatalf("got %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("%d: %q, want %q", i, got[i], want[i])
		}
	}
	_ = os.WriteFile(file, []byte("NOEQUALS\n"), 0o600)
	if _, err := readEnvFile(file); err == nil {
		t.Error("a line without = is refused")
	}
}

func TestBaaSFunctionsNew(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "arianet", "functions")
	out := runCaptured(t, newBaaSCmd(), "functions", "new", "hello-world", "--dir", dir)
	b, err := os.ReadFile(filepath.Join(dir, "hello-world", "index.ts"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Deno.serve(", `Deno.env.get("ARIANET_BAAS_URL")`, "Hello ${name}!", "application/json", "body.name"} {
		if !strings.Contains(string(b), want) {
			t.Errorf("template lacks %q:\n%s", want, b)
		}
	}
	if !strings.Contains(out, "functions serve hello-world") || !strings.Contains(out, "functions deploy <project-id> hello-world") {
		t.Errorf("next steps:\n%s", out)
	}
	if err := baasRunErr(t, "functions", "new", "hello-world", "--dir", dir); err == nil || !strings.Contains(err.Error(), "exists") {
		t.Errorf("existing folder: %v", err)
	}
	if err := baasRunErr(t, "functions", "new", "config", "--dir", dir); err == nil || !strings.Contains(err.Error(), "reserved") {
		t.Errorf("reserved: %v", err)
	}
	if err := baasRunErr(t, "functions", "new", "Bad", "--dir", dir); err == nil {
		t.Error("an invalid name is refused")
	}
}

const baasFunctionJSON = `{"name":"hello","verify_jwt":true,"entrypoint":"hello/index.ts","version":3,"sha256":"ab12","size_bytes":321,
	"url":"https://abcdefghij0123456789.baas.arianet.app/functions/v1/hello","created_at":"2026-11-21T09:00:00Z",
	"updated_at":"2026-11-21T09:00:00Z","deployed_at":"2026-11-21T10:00:00Z"}`

const baasFunctionsListJSON = `{"data":[` + baasFunctionJSON + `],"limits":{"max_functions":25,"bundle_bytes":10485760,"wall_clock_ms":60000,
	"memory_mb":128,"cpu_millis":250,"invocations_month":500000,"max_secrets":50,"hard":true},"usage":{"invocations_month":42},
	"config":{"expose_service_key":false},"endpoint":"https://abcdefghij0123456789.baas.arianet.app/functions/v1"}`

func TestBaaSFunctionsList(t *testing.T) {
	productTestServer(t, "table", api.BaaSPrefix, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/baas/v1/projects/12/functions" {
			t.Errorf("%s %s", r.Method, r.URL.Path)
		}
		_, _ = io.WriteString(w, baasFunctionsListJSON)
	})
	out := runCaptured(t, newBaaSCmd(), "functions", "list", "12")
	for _, want := range []string{"hello", "VERIFY JWT", "functions/v1/hello", "2026-11-21T10:00:00Z", "Functions: 1 of 25", "42 of 500000", "refused"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	productTestServer(t, "json", api.BaaSPrefix, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, baasFunctionsListJSON)
	})
	out = runCaptured(t, newBaaSCmd(), "fn", "ls", "12")
	if !strings.Contains(out, `"bundle_bytes": 10485760`) {
		t.Errorf("json:\n%s", out)
	}
}

func TestBaaSFunctionsDeployUploadsTheZip(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{"hello/index.ts": "Deno.serve(() => new Response('hi'))", "_shared/cors.ts": "export {}",
		"stripe/index.js": "x", "_shared/.secret": "no"})
	var deploys []string
	productTestServer(t, "table", api.BaaSPrefix, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/baas/v1/projects/12/functions":
			_, _ = io.WriteString(w, baasFunctionsListJSON)
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/deploy"):
			raw, _ := io.ReadAll(r.Body)
			if r.Header.Get("Content-Type") != "application/zip" || r.ContentLength != int64(len(raw)) {
				t.Errorf("content type %q length %d/%d", r.Header.Get("Content-Type"), r.ContentLength, len(raw))
			}
			files := zipNames(t, raw)
			if _, ok := files["_shared/cors.ts"]; !ok {
				t.Errorf("shared code missing: %v", files)
			}
			if _, ok := files["_shared/.secret"]; ok {
				t.Error("dotfiles are left out")
			}
			deploys = append(deploys, r.URL.Path+"?"+r.URL.RawQuery)
			w.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(w, `{"data":`+baasFunctionJSON+`}`)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
	})
	out := runCaptured(t, newBaaSCmd(), "functions", "deploy", "12", "hello", "--dir", dir, "--no-verify-jwt")
	if !strings.Contains(out, "Deployed hello version 3") || !strings.Contains(out, "functions/v1/hello") {
		t.Errorf("output:\n%s", out)
	}
	if len(deploys) != 1 || deploys[0] != "/baas/v1/projects/12/functions/hello/deploy?entrypoint=hello%2Findex.ts&verify_jwt=false" {
		t.Errorf("deploys %v", deploys)
	}

	// No name: every function folder, in name order, verify_jwt untouched.
	deploys = nil
	runCaptured(t, newBaaSCmd(), "functions", "deploy", "12", "--dir", dir)
	want := "/baas/v1/projects/12/functions/hello/deploy?entrypoint=hello%2Findex.ts|/baas/v1/projects/12/functions/stripe/deploy?entrypoint=stripe%2Findex.js"
	if strings.Join(deploys, "|") != want {
		t.Errorf("deploys %v", deploys)
	}

	deploys = nil
	runCaptured(t, newBaaSCmd(), "functions", "deploy", "12", "stripe", "--dir", dir, "--verify-jwt")
	if len(deploys) != 1 || !strings.HasSuffix(deploys[0], "verify_jwt=true") {
		t.Errorf("deploys %v", deploys)
	}
	if err := baasRunErr(t, "functions", "deploy", "12", "hello", "--dir", dir, "--verify-jwt", "--no-verify-jwt"); err == nil {
		t.Error("both flags are refused")
	}
}

func TestBaaSFunctionsDeployChecksThePlanSizeFirst(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{"hello/index.ts": strings.Repeat("abcdefghijklmnopqrstuvwxyz0123456789", 100)})
	productTestServer(t, "table", api.BaaSPrefix, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("nothing is uploaded: %s %s", r.Method, r.URL.Path)
		}
		_, _ = io.WriteString(w, `{"data":[],"limits":{"bundle_bytes":100}}`)
	})
	if err := baasRunErr(t, "functions", "deploy", "12", "hello", "--dir", dir); err == nil || !strings.Contains(err.Error(), "bundle size") {
		t.Errorf("err = %v", err)
	}
}

func TestBaaSFunctionsDeleteAndGet(t *testing.T) {
	var got []string
	productTestServer(t, "table", api.BaaSPrefix, func(w http.ResponseWriter, r *http.Request) {
		got = append(got, r.Method+" "+r.URL.Path)
		if r.Method == http.MethodDelete {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		_, _ = io.WriteString(w, `{"data":`+baasFunctionJSON+`}`)
	})
	out := runCaptured(t, newBaaSCmd(), "functions", "get", "12", "hello")
	if !strings.Contains(out, "hello/index.ts") || !strings.Contains(out, "ab12") {
		t.Errorf("get:\n%s", out)
	}
	out = runCaptured(t, newBaaSCmd(), "functions", "delete", "12", "hello", "--yes")
	if !strings.Contains(out, "Function hello deleted.") {
		t.Errorf("delete:\n%s", out)
	}
	if strings.Join(got, "|") != "GET /baas/v1/projects/12/functions/hello|DELETE /baas/v1/projects/12/functions/hello" {
		t.Errorf("requests %v", got)
	}
}

func TestBaaSFunctionsDownload(t *testing.T) {
	dir := t.TempDir()
	bundle := makeZip(t, map[string]string{"hello/index.ts": "deployed", "_shared/cors.ts": "c"})
	productTestServer(t, "table", api.BaaSPrefix, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/baas/v1/projects/12/functions/hello/body" || r.URL.Query().Get("version") != "2" {
			t.Errorf("%s ?%s", r.URL.Path, r.URL.RawQuery)
		}
		w.Header().Set("Content-Type", "application/zip")
		_, _ = w.Write(bundle)
	})
	out := runCaptured(t, newBaaSCmd(), "functions", "download", "12", "hello", "--version", "2", "--dir", dir)
	if !strings.Contains(out, "2 file(s)") {
		t.Errorf("output:\n%s", out)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "hello", "index.ts")); string(b) != "deployed" {
		t.Errorf("file %q", b)
	}
	if err := baasRunErr(t, "functions", "download", "12", "hello", "--version", "2", "--dir", dir); err == nil || !strings.Contains(err.Error(), "--force") {
		t.Errorf("overwrite: %v", err)
	}
	runCaptured(t, newBaaSCmd(), "functions", "download", "12", "hello", "--version", "2", "--dir", dir, "--force")
}

func TestBaaSFunctionsSecrets(t *testing.T) {
	envFile := filepath.Join(t.TempDir(), ".env")
	_ = os.WriteFile(envFile, []byte("FROM_FILE=1\nAPI_KEY=old\n"), 0o600)
	var got []string
	var putBody map[string]any
	productTestServer(t, "table", api.BaaSPrefix, func(w http.ResponseWriter, r *http.Request) {
		got = append(got, r.Method+" "+r.URL.Path)
		switch r.Method {
		case http.MethodPut:
			putBody = baasBody(t, r)
			fallthrough
		case http.MethodGet:
			_, _ = io.WriteString(w, `{"data":[{"name":"API_KEY","digest":"9f86d081","updated_at":"2026-11-21T09:00:00Z"}]}`)
		case http.MethodDelete:
			w.WriteHeader(http.StatusNoContent)
		}
	})
	out := runCaptured(t, newBaaSCmd(), "functions", "secrets", "list", "12")
	if !strings.Contains(out, "API_KEY") || !strings.Contains(out, "9f86d081") {
		t.Errorf("list:\n%s", out)
	}
	out = runCaptured(t, newBaaSCmd(), "functions", "secrets", "set", "12", "API_KEY=s3cr=et", "--env-file", envFile)
	secrets, _ := putBody["secrets"].(map[string]any)
	if len(putBody) != 1 || len(secrets) != 2 || secrets["API_KEY"] != "s3cr=et" || secrets["FROM_FILE"] != "1" {
		t.Errorf("put body %v", putBody)
	}
	if !strings.Contains(out, "Set 2 secret(s)") {
		t.Errorf("set:\n%s", out)
	}
	runCaptured(t, newBaaSCmd(), "functions", "secrets", "unset", "12", "API_KEY", "FROM_FILE", "--yes")
	want := "GET /baas/v1/projects/12/functions/secrets|PUT /baas/v1/projects/12/functions/secrets|" +
		"DELETE /baas/v1/projects/12/functions/secrets/API_KEY|DELETE /baas/v1/projects/12/functions/secrets/FROM_FILE"
	if strings.Join(got, "|") != want {
		t.Errorf("requests %v", got)
	}

	for _, bad := range [][]string{{"ARIANET_BAAS_URL=x"}, {"1BAD=x"}, {"NOVALUE"}, {}} {
		args := append([]string{"functions", "secrets", "set", "12"}, bad...)
		if err := baasRunErr(t, args...); err == nil {
			t.Errorf("%v accepted", bad)
		}
	}
	if err := baasRunErr(t, "functions", "secrets", "set", "12", "BIG="+strings.Repeat("x", maxSecretValueBytes+1)); err == nil {
		t.Error("a value over 64 KiB is refused")
	}
	if err := baasRunErr(t, "functions", "secrets", "unset", "12", "arianet_x", "--yes"); err == nil {
		t.Error("unset validates names")
	}
}

func TestBaaSFunctionsServe(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{"only/index.ts": "x"})
	old := lookDeno
	t.Cleanup(func() { lookDeno = old })
	lookDeno = func() (string, error) { return "", os.ErrNotExist }
	if err := baasRunErr(t, "functions", "serve", "--dir", dir); err == nil || !strings.Contains(err.Error(), "deno is not installed") {
		t.Errorf("without deno: %v", err)
	}
	writeTree(t, dir, map[string]string{"second/index.ts": "x"})
	if err := baasRunErr(t, "functions", "serve", "--dir", dir); err == nil || !strings.Contains(err.Error(), "name the one") {
		t.Errorf("two functions, no name: %v", err)
	}

	env := serveEnv([]string{"HOME=/h"}, [][2]string{{"API_KEY", "v"}}, 9000)
	if strings.Join(env, ",") != "HOME=/h,API_KEY=v,PORT=9000,DENO_SERVE_PORT=9000" {
		t.Errorf("env %v", env)
	}
}

func TestBaaSHelpNamesTheFunctionsScope(t *testing.T) {
	cmd := newBaaSCmd()
	if !strings.Contains(cmd.Long, "baas:functions:manage") {
		t.Error("the baas help lists the functions scope")
	}
	fn, _, err := cmd.Find([]string{"functions"})
	if err != nil || fn.Name() != "functions" {
		t.Fatalf("functions command: %v", err)
	}
	var names []string
	for _, c := range fn.Commands() {
		names = append(names, c.Name())
	}
	if strings.Join(names, ",") != "delete,deploy,download,get,list,new,secrets,serve" {
		t.Errorf("subcommands %v", names)
	}
}
