package commands

import (
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/ariaservice/arianet-cli/internal/api"
	"github.com/ariaservice/arianet-cli/internal/printer"
	"github.com/spf13/cobra"
)

// The baas command group: Arianet BaaS (a project = a PostgreSQL database
// with its endpoint, auth, storage and keys) through the gateway's /baas/v1
// (arianet-infra-tasks#1102, #1109). The commands that are one request are
// endpoint descriptions like every product; sql, db, types, env and the
// storage transfers are written out below.

// EnvBaaSAPIBase points the baas commands at a BaaS API's /v1 directly
// (e.g. a local development stack, http://localhost:8098/v1) instead of
// <api_url>/baas/v1. The token is then that API's session.
const EnvBaaSAPIBase = "ARIANET_BAAS_API_BASE"

var baasProjectCols = []col{{header: "ID", key: "id"}, {header: "Ref", key: "ref"}, {header: "Name", key: "name"},
	{header: "Plan", key: "plan"}, {header: "Region", key: "region"}, {header: "State", key: "state"},
	{header: "Endpoint", key: "endpoint"}, {header: "Created", key: "created_at"}}

var baasKeyCols = []col{{header: "Kind", key: "kind"}, {header: "Key ID", key: "kid"}, {header: "Preview", key: "preview"},
	{header: "State", key: "state"}, {header: "Created", key: "created_at"}, {header: "Rotated", key: "rotated_at"}}

var baasUserCols = []col{{header: "ID", key: "id"}, {header: "Email", key: "email"}, {header: "Phone", key: "phone"},
	{header: "Confirmed", key: "confirmed_at"}, {header: "Last sign in", key: "last_sign_in_at"}, {header: "Created", key: "created_at"}}

var baasLogCols = []col{{header: "Time", key: "at"}, {header: "Source", key: "source"}, {header: "Method", key: "method"},
	{header: "Path", key: "path"}, {header: "Status", key: "status"}, {header: "ms", key: "duration_ms"}, {header: "IP", key: "ip"}}

var baasKeyKinds = []string{"anon", "service", "publishable", "secret", "database"}

func newBaaSCmd() *cobra.Command {
	var apiBase string
	p := &product{name: "arianet baas", prefix: api.BaaSPrefix, actions: true, linkProject: true,
		scopes: "baas:projects:read (reads, incl. migration list, db pull and diff, gen types, advisors), " +
			"baas:projects:manage (create, rename, delete, pause, resume, plan, domains, transfer, api-settings set), " +
			"baas:keys:manage (keys reveal and rotate, env), baas:sql:execute (sql, db push, db pull --mark-applied), " +
			"baas:storage:manage (storage writes), baas:auth-users:manage (auth users create and delete), " +
			"baas:functions:manage (functions deploy, delete, download and secrets changes)",
		base: func() string {
			if apiBase != "" {
				return apiBase
			}
			return os.Getenv(EnvBaaSAPIBase)
		}}
	cmd := p.group("baas", "Manage BaaS projects: database, SQL, migrations, keys, storage, auth users and edge functions",
		`Arianet BaaS: each project is a PostgreSQL database with a public endpoint (REST, auth,
storage) and its keys. Create and manage projects, run SQL, apply migrations from files,
generate TypeScript types, deploy edge functions, and print the environment your app needs.

Run arianet baas link in your app's directory once: the project ID, the first argument of
the project commands, can then be left out there. Migration files live in arianet/migrations.

Projects run in region thr-1.`)
	cmd.PersistentFlags().StringVar(&apiBase, "api-base", "", "call this BaaS /v1 address directly instead of the gateway (development)")
	_ = cmd.PersistentFlags().MarkHidden("api-base")

	projects := subgroup("projects", "Create and manage projects", []string{"project"},
		p.command(endpoint{use: "list", aliases: []string{"ls"}, short: "List your projects", method: http.MethodGet,
			path: "/projects", cols: baasProjectCols}),
		p.command(endpoint{use: "get <project-id>", aliases: []string{"show"}, short: "Show one project", method: http.MethodGet,
			path: "/projects/{0}", nargs: 1, cols: baasProjectCols}),
		p.command(endpoint{use: "create", short: "Create a project (billed from the moment its database exists)", method: http.MethodPost,
			path: "/projects", done: "Project created; it is being provisioned.",
			example: "  arianet baas projects create --name shop --plan free --wait",
			body: []bodyFlag{
				{"name", "name", "string", "project name: 1-64 letters, digits, spaces, dots, dashes or underscores", true},
				{"plan", "plan", "string", "plan code (arianet baas plans)", true},
				{"workspace", "workspace_id", "int", "workspace ID (optional)", false},
			}}),
		p.command(endpoint{use: "rename <project-id>", short: "Rename a project", method: http.MethodPatch, path: "/projects/{0}", nargs: 1,
			done: "Project #{0} renamed.", body: []bodyFlag{{"name", "name", "string", "new name", true}}}),
		p.command(endpoint{use: "delete <project-id>", aliases: []string{"rm"}, short: "Delete a project and all its data", method: http.MethodPost,
			path: "/projects/{0}/delete", nargs: 1, done: "Project #{0} is being deleted.",
			example: "  arianet baas projects delete 12 --confirm abcdefghij0123456789",
			body:    []bodyFlag{{"confirm", "confirm", "string", "the project's ref, typed to confirm (arianet baas projects get)", true}}}),
		p.command(endpoint{use: "pause <project-id>", short: "Pause a project: its database refuses connections, its data is kept", method: http.MethodPost,
			path: "/projects/{0}/pause", nargs: 1, done: "Project #{0} is being paused."}),
		p.command(endpoint{use: "resume <project-id>", short: "Resume a paused project", method: http.MethodPost,
			path: "/projects/{0}/resume", nargs: 1, done: "Project #{0} is being resumed."}),
		p.baasPlanCmd(),
	)

	keys := subgroup("keys", "Show, reveal and rotate a project's keys", []string{"key"},
		p.command(endpoint{use: "list <project-id>", aliases: []string{"ls"}, short: "List the project's keys (previews only)", method: http.MethodGet,
			path: "/projects/{0}/keys", nargs: 1, cols: baasKeyCols}),
		p.command(endpoint{use: "reveal <project-id> <kind>", short: "Show one key (" + strings.Join(baasKeyKinds, ", ") + "); recorded in your activity log",
			method: http.MethodPost, path: "/projects/{0}/keys/{1}/reveal", nargs: 2, done: "Key {1} of project #{0}:",
			example: "  arianet baas keys reveal 12 anon\n  arianet baas keys reveal 12 database -o json"}),
		p.command(endpoint{use: "rotate <project-id>", short: "Issue new anon and service keys; the previous ones keep working for the overlap",
			method: http.MethodPost, path: "/projects/{0}/keys/rotate", nargs: 1,
			confirm: "Rotate the signing key of project #{0}? New anon and service keys are issued; the current ones are revoked after the overlap.",
			done:    "Keys of project #{0} rotated."}),
	)

	userList := p.command(endpoint{use: "list <project-id>", aliases: []string{"ls"}, short: "List the project's users", method: http.MethodGet,
		path: "/projects/{0}/auth/users", nargs: 1, cols: baasUserCols,
		query: []queryFlag{{"search", "q", "search the email, phone or ID"}, {"page", "page", "page number"}, {"per-page", "per_page", "users per page (at most 200)"}}})
	users := subgroup("users", "Manage the project's end users", []string{"user"},
		userList,
		p.command(endpoint{use: "get <project-id> <user-id>", aliases: []string{"show"}, short: "Show one user", method: http.MethodGet,
			path: "/projects/{0}/auth/users/{1}", nargs: 2}),
		p.command(endpoint{use: "create <project-id>", short: "Create a user", method: http.MethodPost, path: "/projects/{0}/auth/users", nargs: 1,
			done: "User created.", example: "  arianet baas auth users create 12 --email ana@example.com --password 'S3cret-pass' --email-confirm",
			body: []bodyFlag{
				{"email", "email", "string", "email address", false},
				{"phone", "phone", "string", "phone number", false},
				{"password", "password", "string", "password", false},
				{"email-confirm", "email_confirm", "bool", "mark the email as confirmed", false},
				{"phone-confirm", "phone_confirm", "bool", "mark the phone as confirmed", false},
			}}),
		p.command(endpoint{use: "delete <project-id> <user-id>", aliases: []string{"rm"}, short: "Delete a user", method: http.MethodDelete,
			path: "/projects/{0}/auth/users/{1}", nargs: 2, confirm: "Delete user {1} of project #{0}?", done: "User {1} deleted."}),
	)
	auth := subgroup("auth", "The project's end-user authentication", nil, users)

	cmd.AddCommand(
		p.command(endpoint{use: "plans", short: "List the plans and their limits", method: http.MethodGet, path: "/plans",
			cols: []col{{header: "Code", key: "code"}, {header: "Name", key: "name"}, {header: "Free", key: "free"},
				{header: "Database bytes", key: "limits.db_bytes"}, {header: "Storage bytes", key: "limits.storage_bytes"},
				{header: "Monthly users", key: "limits.mau"}, {header: "Projects", key: "limits.max_projects_per_account"}}}),
		projects, keys,
		p.baasLinkCmd(),
		p.baasUnlinkCmd(),
		p.baasSQLCmd(),
		p.baasMigrationCmd(),
		p.baasDBCmd(),
		p.baasGenCmd(),
		p.baasTypesCmd("types [project-id]", "ts", "Alias of gen types: generate TypeScript types of the database"),
		p.baasEnvCmd(),
		p.baasStorageCmd(),
		auth,
		p.baasFunctionsCmd(),
		p.baasLogsCmd(),
		p.baasAdvisorsCmd(),
		p.baasDomainsCmd(),
		p.baasTransferCmd(),
		p.baasAPISettingsCmd(),
		p.command(endpoint{use: "usage <project-id>", short: "Show this month's use against the plan", method: http.MethodGet,
			path: "/projects/{0}/usage", nargs: 1, render: renderBaaSUsage}),
		p.actionsCmd(),
		p.apiCmd(),
	)
	return cmd
}

var digitsOnly = regexp.MustCompile(`^[0-9]+$`)

// projectID checks a project ID argument (a decimal number).
func projectID(arg string) (string, error) {
	arg = strings.TrimSpace(arg)
	if !digitsOnly.MatchString(arg) {
		return "", fmt.Errorf("invalid project ID %q: expected a number", arg)
	}
	return arg, nil
}

func (p *product) baasPlanCmd() *cobra.Command {
	var w waitFlags
	cmd := &cobra.Command{
		Use:     "plan [project-id] <plan-code>",
		Short:   "Move a project to another plan (billed from now)",
		Example: "  arianet baas projects plan 12 pro",
		Args:    projectArgsRange(1, 1),
		RunE: func(cmd *cobra.Command, args []string) error {
			args, err := withProject(args, 1, 1)
			if err != nil {
				return err
			}
			id := args[0]
			cc := p.client()
			if cc == nil {
				return nil
			}
			var raw json.RawMessage
			if err := cc.Put("/projects/"+id+"/plan", map[string]string{"plan": strings.TrimSpace(args[1])}, &raw); err != nil {
				handleAPIError(err)
				return nil
			}
			p.reportChange(cc, raw, fmt.Sprintf("Project #%s moved to plan %s.", id, args[1]), w)
			return nil
		},
	}
	w.add(cmd)
	return cmd
}

func renderBaaSUsage(p *printer.Printer, obj map[string]any) {
	data, _ := obj["data"].(map[string]any)
	if data == nil {
		data = obj
	}
	printFields(p, data, []col{{header: "Plan", key: "plan"}, {header: "Period start", key: "period_start"}, {header: "Period end", key: "period_end"}})
	metrics, _ := data["metrics"].(map[string]any)
	order := []string{"db_bytes", "storage_bytes", "egress_bytes", "mau", "requests", "sms_otp"}
	rows := [][]string{}
	for _, name := range order {
		m, ok := metrics[name].(map[string]any)
		if !ok {
			continue
		}
		limit := "unlimited"
		if m["limit"] != nil {
			limit = cell(m["limit"])
		}
		rows = append(rows, []string{strings.ReplaceAll(name, "_", " "), cell(m["used"]), limit})
	}
	if len(rows) > 0 {
		p.TableAdvanced("Use this period", []string{"Metric", "Used", "Limit"}, rows)
	}
}

// ─── logs ────────────────────────────────────────────────────────────────────

func (p *product) baasLogsCmd() *cobra.Command {
	var source, search string
	var since time.Duration
	var limit, status int
	var before int64
	cmd := &cobra.Command{
		Use:   "logs [project-id]",
		Short: "Show the project's request logs (last 7 days) and SQL editor runs",
		Example: `  arianet baas logs 12 --since 1h
  arianet baas logs 12 --source auth --status 4 --limit 50`,
		Args: projectArgsRange(0, 0),
		RunE: func(cmd *cobra.Command, args []string) error {
			args, err := withProject(args, 0, 0)
			if err != nil {
				return err
			}
			id := args[0]
			q := url.Values{}
			if source != "" {
				q.Set("source", source)
			}
			if since > 0 {
				q.Set("from", time.Now().UTC().Add(-since).Format(time.RFC3339))
			}
			if search != "" {
				q.Set("q", search)
			}
			if status > 0 {
				q.Set("status", fmt.Sprint(status))
			}
			if limit > 0 {
				q.Set("limit", fmt.Sprint(limit))
			}
			if before > 0 {
				q.Set("before", fmt.Sprint(before))
			}
			cc := p.client()
			if cc == nil {
				return nil
			}
			var raw json.RawMessage
			if err := cc.Get("/projects/"+id+"/logs", q, &raw); err != nil {
				handleAPIError(err)
				return nil
			}
			printAnswer(raw, baasLogCols)
			if !jsonMode() {
				if obj, err := decodeObject(raw); err == nil && obj["next"] != nil {
					printer.Info(fmt.Sprintf("Older entries: add --before %s", cell(obj["next"])))
				}
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&source, "source", "", "only this component: gateway, auth, storage, rest or sql")
	cmd.Flags().DurationVar(&since, "since", 0, "only entries newer than this, e.g. 30m or 24h")
	cmd.Flags().StringVar(&search, "search", "", "only paths containing this")
	cmd.Flags().IntVar(&status, "status", 0, "only this status class: 2, 3, 4 or 5")
	cmd.Flags().IntVar(&limit, "limit", 0, "at most this many entries (1-500)")
	cmd.Flags().Int64Var(&before, "before", 0, "the next page: entries before this one (printed under the table)")
	return cmd
}

// ─── storage ─────────────────────────────────────────────────────────────────

func (p *product) baasStorageCmd() *cobra.Command {
	bucketPath := func(id, bucket string) string {
		return "/projects/" + id + "/storage/buckets/" + url.PathEscape(bucket)
	}

	buckets := p.command(endpoint{use: "buckets <project-id>", aliases: []string{"bucket"}, short: "List buckets (create and delete are subcommands)",
		method: http.MethodGet, path: "/projects/{0}/storage/buckets", nargs: 1,
		cols: []col{{header: "Name", key: "name"}, {header: "Public", key: "public"}, {header: "Size limit", key: "file_size_limit"},
			{header: "Types", key: "allowed_mime_types"}, {header: "Created", key: "created_at"}}})
	buckets.Args = projectArgsRange(0, 0)
	buckets.AddCommand(
		p.command(endpoint{use: "create <project-id>", short: "Create a bucket", method: http.MethodPost, path: "/projects/{0}/storage/buckets", nargs: 1,
			done: "Bucket created.", example: "  arianet baas storage buckets create 12 --name avatars --public",
			body: []bodyFlag{
				{"name", "name", "string", "bucket name (letters, digits, _ . -)", true},
				{"public", "public", "bool", "objects can be read without a key", false},
				{"size-limit", "file_size_limit", "int", "largest object in bytes", false},
				{"types", "allowed_mime_types", "strings", "allowed content types, e.g. image/png,image/jpeg", false},
			}}),
		p.command(endpoint{use: "delete <project-id> <bucket>", aliases: []string{"rm"}, short: "Delete an empty bucket", method: http.MethodDelete,
			path: "/projects/{0}/storage/buckets/{1}", nargs: 2, confirm: "Delete bucket {1}?", done: "Bucket {1} deleted."}),
		p.command(endpoint{use: "empty <project-id> <bucket>", short: "Delete every object of a bucket", method: http.MethodPost,
			path: "/projects/{0}/storage/buckets/{1}/empty", nargs: 2, confirm: "Delete every object in bucket {1}?", done: "Bucket {1} emptied."}),
	)

	var lsLimit, lsOffset int
	ls := &cobra.Command{
		Use:     "ls [project-id] <bucket> [prefix]",
		Aliases: []string{"list"},
		Short:   "List a folder of a bucket",
		Args:    projectArgsRange(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			args, err := withProject(args, 1, 2)
			if err != nil {
				return err
			}
			id := args[0]
			q := url.Values{}
			if len(args) == 3 {
				q.Set("prefix", args[2])
			}
			if lsLimit > 0 {
				q.Set("limit", fmt.Sprint(lsLimit))
			}
			if lsOffset > 0 {
				q.Set("offset", fmt.Sprint(lsOffset))
			}
			cc := p.client()
			if cc == nil {
				return nil
			}
			var raw json.RawMessage
			if err := cc.Get(bucketPath(id, args[1])+"/objects", q, &raw); err != nil {
				handleAPIError(err)
				return nil
			}
			printAnswer(raw, []col{{header: "Name", key: "name"}, {header: "Folder", key: "folder"}, {header: "Size", key: "size"},
				{header: "Type", key: "mimetype"}, {header: "Updated", key: "updated_at"}})
			return nil
		},
	}
	ls.Flags().IntVar(&lsLimit, "limit", 0, "at most this many entries (1-1000)")
	ls.Flags().IntVar(&lsOffset, "offset", 0, "skip this many entries")

	var upsert bool
	var contentType string
	upload := &cobra.Command{
		Use:     "upload [project-id] <bucket> <local-file> [object-path]",
		Aliases: []string{"put", "cp"},
		Short:   "Upload a file (the object path defaults to the file's name)",
		Example: "  arianet baas storage upload 12 avatars ./me.png users/42/me.png --upsert",
		Args:    projectArgsRange(2, 3),
		RunE: func(cmd *cobra.Command, args []string) error {
			args, err := withProject(args, 2, 3)
			if err != nil {
				return err
			}
			id := args[0]
			local := args[2]
			object := filepath.Base(local)
			if len(args) == 4 {
				object = strings.TrimPrefix(args[3], "/")
			}
			f, err := os.Open(local)
			if err != nil {
				return err
			}
			defer func() { _ = f.Close() }()
			st, err := f.Stat()
			if err != nil {
				return err
			}
			if st.IsDir() {
				return fmt.Errorf("%s is a directory", local)
			}
			ct := contentType
			if ct == "" {
				ct = mime.TypeByExtension(filepath.Ext(local))
			}
			q := url.Values{"path": {object}}
			if upsert {
				q.Set("upsert", "true")
			}
			cc := p.client()
			if cc == nil {
				return nil
			}
			var raw json.RawMessage
			if err := cc.Upload(http.MethodPut, bucketPath(id, args[1])+"/object", q, f, st.Size(), ct, &raw); err != nil {
				handleAPIError(err)
				return nil
			}
			if jsonMode() {
				printRawJSON(raw)
				return nil
			}
			printer.Success(fmt.Sprintf("Uploaded %s to %s/%s (%d bytes).", local, args[1], object, st.Size()))
			return nil
		},
	}
	upload.Flags().BoolVar(&upsert, "upsert", false, "replace an object that exists")
	upload.Flags().StringVar(&contentType, "content-type", "", "content type to store (default: from the file extension)")

	download := &cobra.Command{
		Use:     "download [project-id] <bucket> <object-path> [local-file]",
		Aliases: []string{"get"},
		Short:   "Download an object (to the object's file name, or - for standard output)",
		Args:    projectArgsRange(2, 3),
		RunE: func(cmd *cobra.Command, args []string) error {
			args, err := withProject(args, 2, 3)
			if err != nil {
				return err
			}
			id := args[0]
			object := strings.TrimPrefix(args[2], "/")
			local := filepath.Base(object)
			if len(args) == 4 {
				local = args[3]
			}
			cc := p.client()
			if cc == nil {
				return nil
			}
			var w io.Writer = os.Stdout
			var tmp *os.File
			if local != "-" {
				// Written next to the target and renamed, so a failed
				// download never leaves a half file under the real name.
				tmp, err = os.CreateTemp(filepath.Dir(local), ".arianet-download-*")
				if err != nil {
					return err
				}
				defer os.Remove(tmp.Name())
				w = tmp
			}
			n, err := cc.Download(bucketPath(id, args[1])+"/object", url.Values{"path": {object}}, w)
			if tmp != nil {
				if cerr := tmp.Close(); err == nil && cerr != nil {
					err = cerr
				}
			}
			if err != nil {
				handleAPIError(err)
				return nil
			}
			if tmp == nil {
				return nil
			}
			if err := os.Rename(tmp.Name(), local); err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "Downloaded %s/%s to %s (%d bytes).\n", args[1], object, local, n)
			return nil
		},
	}

	rm := &cobra.Command{
		Use:     "rm [project-id] <bucket> <object-path>...",
		Aliases: []string{"delete"},
		Short:   "Delete objects",
		Args:    projectArgsRange(2, -1),
		RunE: func(cmd *cobra.Command, args []string) error {
			args, err := withProject(args, 2, -1)
			if err != nil {
				return err
			}
			id := args[0]
			paths := make([]string, 0, len(args)-2)
			for _, a := range args[2:] {
				paths = append(paths, strings.TrimPrefix(a, "/"))
			}
			yes, _ := cmd.Flags().GetBool("yes")
			if !yes && !confirm(fmt.Sprintf("Delete %d object(s) from bucket %s?", len(paths), args[1])) {
				printer.Info("Cancelled.")
				return nil
			}
			cc := p.client()
			if cc == nil {
				return nil
			}
			var raw json.RawMessage
			if err := cc.Delete(bucketPath(id, args[1])+"/objects", nil, map[string]any{"paths": paths}, &raw); err != nil {
				handleAPIError(err)
				return nil
			}
			if jsonMode() {
				printRawJSON(raw)
				return nil
			}
			var out struct {
				Data struct {
					Deleted []string `json:"deleted"`
				} `json:"data"`
			}
			_ = json.Unmarshal(raw, &out)
			printer.Success(fmt.Sprintf("Deleted %d object(s).", len(out.Data.Deleted)))
			for _, d := range out.Data.Deleted {
				printer.Info(d)
			}
			return nil
		},
	}
	rm.Flags().BoolP("yes", "y", false, "skip the confirmation prompt")

	return subgroup("storage", "Manage the project's buckets and files", nil, buckets, ls, upload, download, rm)
}

// ─── env ─────────────────────────────────────────────────────────────────────

// baasEnvAliases are the framework variable names printed next to the
// ARIANET_BAAS_* ones: browser code only sees variables with its
// framework's public prefix.
var baasEnvAliases = []string{"NEXT_PUBLIC_BAAS", "VITE_BAAS", "NUXT_PUBLIC_BAAS", "EXPO_PUBLIC_BAAS"}

func (p *product) baasEnvCmd() *cobra.Command {
	var format string
	cmd := &cobra.Command{
		Use:   "env [project-id]",
		Short: "Print the URL and anon key your app needs, as .env lines or JSON",
		Long: `Print the project's endpoint and its anon key (revealed, which needs baas:keys:manage and
is recorded in your activity log) as ARIANET_BAAS_URL and ARIANET_BAAS_ANON_KEY, plus the same
values under the public prefixes of common frameworks:

  NEXT_PUBLIC_BAAS_URL / NEXT_PUBLIC_BAAS_ANON_KEY   Next.js
  VITE_BAAS_URL / VITE_BAAS_ANON_KEY                 Vite (React, Vue, Svelte)
  NUXT_PUBLIC_BAAS_URL / NUXT_PUBLIC_BAAS_ANON_KEY   Nuxt
  EXPO_PUBLIC_BAAS_URL / EXPO_PUBLIC_BAAS_ANON_KEY   Expo

Keep the lines your app reads. If your code expects other names, map them in your .env file,
e.g. MY_APP_API_URL=$NEXT_PUBLIC_BAAS_URL, or pass the values to your client library's
constructor from these variables. The anon key is safe in browser code (row level security
decides what it can read); never put the service or secret key there.`,
		Example: `  arianet baas env 12 > .env.local
  arianet baas env 12 --format json`,
		Args: projectArgsRange(0, 0),
		RunE: func(cmd *cobra.Command, args []string) error {
			args, err := withProject(args, 0, 0)
			if err != nil {
				return err
			}
			id := args[0]
			if format == "" && jsonMode() {
				format = "json"
			}
			if format == "" {
				format = "dotenv"
			}
			if format != "dotenv" && format != "json" {
				return fmt.Errorf("--format must be dotenv or json")
			}
			cc := p.client()
			if cc == nil {
				return nil
			}
			var project struct {
				Data struct {
					Endpoint string `json:"endpoint"`
				} `json:"data"`
			}
			if err := cc.Get("/projects/"+id, nil, &project); err != nil {
				handleAPIError(err)
				return nil
			}
			var key struct {
				Data struct {
					Key string `json:"key"`
				} `json:"data"`
			}
			if err := cc.Post("/projects/"+id+"/keys/anon/reveal", nil, &key); err != nil {
				handleAPIError(err)
				return nil
			}
			vars := baasEnvVars(project.Data.Endpoint, key.Data.Key)
			if format == "json" {
				obj := map[string]string{}
				for _, v := range vars {
					obj[v[0]] = v[1]
				}
				printer.New("json").PrintJSON(obj)
				return nil
			}
			fmt.Printf("# Arianet BaaS project %s\n", id)
			for _, v := range vars {
				fmt.Printf("%s=%s\n", v[0], v[1])
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&format, "format", "", "dotenv or json (default dotenv; json with -o json)")
	return cmd
}

// baasEnvVars are the name/value pairs env prints, in order.
func baasEnvVars(endpoint, anonKey string) [][2]string {
	vars := [][2]string{{"ARIANET_BAAS_URL", endpoint}, {"ARIANET_BAAS_ANON_KEY", anonKey}}
	for _, prefix := range baasEnvAliases {
		vars = append(vars, [2]string{prefix + "_URL", endpoint}, [2]string{prefix + "_ANON_KEY", anonKey})
	}
	return vars
}
