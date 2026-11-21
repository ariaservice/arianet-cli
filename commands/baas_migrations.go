package commands

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/ariaservice/arianet-cli/internal/api"
	"github.com/ariaservice/arianet-cli/internal/printer"
	"github.com/spf13/cobra"
)

// migration new / list, db push / pull / diff and gen types through the
// service's schema, types and migrations endpoints (arianet-infra-tasks
// #1149). A region without those endpoints (404) gets the older behaviour:
// the SQL endpoint and the table introspection (baas_sql.go).

// baasMigrationsDir is where migration files live by default.
var baasMigrationsDir = filepath.Join(baasProjectDir, "migrations")

// legacyMigrationsDir was the default before arianet/migrations.
const legacyMigrationsDir = "migrations"

// exitCodeDifferences is what db diff exits with when it finds a difference.
const exitCodeDifferences = 2

// The service accepts at most this many files and bytes in one push.
const (
	maxPushFiles = 200
	maxPushBytes = 2 << 20
)

func isDir(path string) bool {
	st, err := os.Stat(path)
	return err == nil && st.IsDir()
}

// migrationsDirOf is --dir when given, else arianet/migrations, or
// migrations when only that one exists.
func migrationsDirOf(cmd *cobra.Command, dir string) string {
	if cmd.Flags().Changed("dir") {
		return dir
	}
	if !isDir(baasMigrationsDir) && isDir(legacyMigrationsDir) {
		fmt.Fprintf(os.Stderr, "Note: using ./%s (the default is now ./%s; move the files there, or pass --dir)\n", legacyMigrationsDir, baasMigrationsDir)
		return legacyMigrationsDir
	}
	return baasMigrationsDir
}

// readMigrationsIfAny is readMigrations with a missing directory as none.
func readMigrationsIfAny(dir string) ([]migrationFile, error) {
	files, err := readMigrations(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	return files, err
}

// endpointMissing reports whether err is a region that does not have the
// endpoint yet (an older baas-service answers 404 or 405 for it).
func endpointMissing(err error) bool {
	var apiErr *api.APIError
	if !errors.As(err, &apiErr) {
		return false
	}
	return apiErr.Status == http.StatusNotFound || apiErr.Status == http.StatusMethodNotAllowed
}

func fallbackNote(what string) {
	fmt.Fprintf(os.Stderr, "Note: this region does not have the %s endpoint yet; using the older client-side way.\n", what)
}

func schemasParam(schemas []string) []string {
	list := splitList(schemas)
	if len(list) == 0 {
		list = []string{"public"}
	}
	return list
}

// ─── remote history ──────────────────────────────────────────────────────────

type remoteMigration struct {
	Name      string  `json:"name"`
	Checksum  string  `json:"checksum"`
	AppliedAt *string `json:"applied_at"`
}

// remoteMigrations is the project's migration history in name order.
func remoteMigrations(cc *api.CloudClient, id string) ([]remoteMigration, error) {
	var out struct {
		Data []remoteMigration `json:"data"`
	}
	err := cc.Get("/projects/"+id+"/migrations", nil, &out)
	if err == nil {
		sort.Slice(out.Data, func(i, j int) bool { return out.Data[i].Name < out.Data[j].Name })
		return out.Data, nil
	}
	if !endpointMissing(err) {
		return nil, err
	}
	fallbackNote("migrations")
	applied, err := appliedMigrations(cc, id, true)
	if err != nil {
		return nil, err
	}
	list := make([]remoteMigration, 0, len(applied))
	for name, sum := range applied {
		list = append(list, remoteMigration{Name: name, Checksum: sum})
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Name < list[j].Name })
	return list, nil
}

// Migration states: in both with the same checksum, local only, in both
// with another checksum, recorded but no file.
const (
	migrationApplied    = "applied"
	migrationPending    = "pending"
	migrationChanged    = "changed"
	migrationRemoteOnly = "remote only"
)

type migrationStatus struct {
	Name           string  `json:"name"`
	Local          bool    `json:"local"`
	Checksum       string  `json:"checksum,omitempty"`
	RemoteChecksum string  `json:"remote_checksum,omitempty"`
	AppliedAt      *string `json:"applied_at"`
	Status         string  `json:"status"`
}

// migrationStatuses compares the files with the history, in name order.
func migrationStatuses(files []migrationFile, remote []remoteMigration) []migrationStatus {
	byName := map[string]*migrationStatus{}
	for _, f := range files {
		byName[f.Name] = &migrationStatus{Name: f.Name, Local: true, Checksum: f.Checksum, Status: migrationPending}
	}
	for _, r := range remote {
		st, ok := byName[r.Name]
		if !ok {
			st = &migrationStatus{Name: r.Name, Status: migrationRemoteOnly}
			byName[r.Name] = st
		}
		st.RemoteChecksum, st.AppliedAt = r.Checksum, r.AppliedAt
		if st.Local {
			st.Status = migrationApplied
			if r.Checksum != st.Checksum {
				st.Status = migrationChanged
			}
		}
	}
	out := make([]migrationStatus, 0, len(byName))
	for _, st := range byName {
		out = append(out, *st)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func migrationRows(list []migrationStatus) [][]string {
	rows := make([][]string, len(list))
	for i, m := range list {
		local, applied := "no", "-"
		if m.Local {
			local = "yes"
		}
		if m.AppliedAt != nil {
			applied = *m.AppliedAt
		} else if m.Status == migrationApplied || m.Status == migrationChanged || m.Status == migrationRemoteOnly {
			applied = "yes"
		}
		rows[i] = []string{m.Name, local, applied, m.Status}
	}
	return rows
}

var migrationHeaders = []string{"Name", "Local", "Applied at", "Status"}

// ─── migration new / list ────────────────────────────────────────────────────

var (
	camelBoundary = regexp.MustCompile(`([a-z0-9])([A-Z])`)
	nonWordRun    = regexp.MustCompile(`[^a-z0-9]+`)
)

// snakeName turns "Create todos" or "createTodos" into create_todos.
func snakeName(name string) string {
	name = camelBoundary.ReplaceAllString(name, "${1}_${2}")
	return strings.Trim(nonWordRun.ReplaceAllString(strings.ToLower(name), "_"), "_")
}

func (p *product) baasMigrationCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "migration", Aliases: []string{"migrations"},
		Short: "Create migration files and compare them with what the project applied"}

	var newDir string
	newCmd := &cobra.Command{
		Use:   "new <name>",
		Short: "Create an empty migration file named <UTC timestamp>_<name>.sql",
		Long: `Create arianet/migrations/<yyyymmddhhmmss>_<name>.sql (UTC time; the name in snake_case) with
a header comment. Write the SQL into it and apply it with arianet baas db push.`,
		Example: "  arianet baas migration new create_todos",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := snakeName(args[0])
			if name == "" {
				return fmt.Errorf("invalid migration name %q: use letters and digits", args[0])
			}
			dir := migrationsDirOf(cmd, newDir)
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return err
			}
			now := time.Now().UTC()
			file := now.Format("20060102150405") + "_" + name + ".sql"
			path := filepath.Join(dir, file)
			header := fmt.Sprintf("-- Migration %s, created by arianet baas migration new at %s.\n"+
				"-- Write the SQL below. No BEGIN or COMMIT: db push runs every pending file in one transaction.\n\n",
				name, now.Format(time.RFC3339))
			f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
			if err != nil {
				return err
			}
			if _, err := f.WriteString(header); err != nil {
				_ = f.Close()
				return err
			}
			if err := f.Close(); err != nil {
				return err
			}
			if jsonMode() {
				printer.New("json").PrintJSON(map[string]string{"name": file, "path": path})
				return nil
			}
			printer.Success("Created " + path)
			return nil
		},
	}
	newCmd.Flags().StringVar(&newDir, "dir", baasMigrationsDir, "directory of the migration files")

	var listDir string
	listCmd := &cobra.Command{
		Use:     "list [project-id]",
		Aliases: []string{"ls"},
		Short:   "List the local migration files and the project's history: applied, pending, changed or remote only",
		Args:    projectArgsRange(0, 0),
		RunE: func(cmd *cobra.Command, args []string) error {
			args, err := withProject(args, 0, 0)
			if err != nil {
				return err
			}
			dir := migrationsDirOf(cmd, listDir)
			files, err := readMigrationsIfAny(dir)
			if err != nil {
				return err
			}
			cc := p.client()
			if cc == nil {
				return nil
			}
			remote, err := remoteMigrations(cc, args[0])
			if err != nil {
				handleSQLError(err)
				return nil
			}
			list := migrationStatuses(files, remote)
			if jsonMode() {
				printer.New("json").PrintJSON(list)
				return nil
			}
			if len(list) == 0 {
				printer.Info(fmt.Sprintf("No migrations in %s and none applied. Start one with: arianet baas migration new <name>", dir))
				return nil
			}
			printer.New(cfg.Output).Table(migrationHeaders, migrationRows(list))
			return nil
		},
	}
	listCmd.Flags().StringVar(&listDir, "dir", baasMigrationsDir, "directory of the migration files")

	cmd.AddCommand(newCmd, listCmd)
	return cmd
}

// ─── db push / pull / diff ───────────────────────────────────────────────────

func (p *product) baasDBCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "db", Short: "Apply migration files, write the schema to a file, or compare both with the project"}
	cmd.AddCommand(p.baasDBPushCmd(), p.baasDBPullCmd(), p.baasDBDiffCmd())
	return cmd
}

// pushAnswer is the migrations endpoint's data.
type pushAnswer struct {
	DryRun  bool     `json:"dry_run"`
	Applied []string `json:"applied"`
	Skipped []string `json:"skipped"`
	Pending []string `json:"pending"`
}

// errorField reads one field of a refusal's body.
func errorField(err error, field string) string {
	var apiErr *api.APIError
	if !errors.As(err, &apiErr) || len(apiErr.Body) == 0 {
		return ""
	}
	var body map[string]any
	if json.Unmarshal(apiErr.Body, &body) != nil {
		return ""
	}
	switch v := body[field].(type) {
	case string:
		return v
	case []any:
		parts := make([]string, 0, len(v))
		for _, it := range v {
			parts = append(parts, fmt.Sprint(it))
		}
		return strings.Join(parts, ", ")
	}
	return ""
}

func apiCode(err error) string {
	var apiErr *api.APIError
	if errors.As(err, &apiErr) {
		return apiErr.Code
	}
	return ""
}

// trimForPush drops the files the history already has unchanged when the
// push would be over the service's limits (it skips them anyway).
func trimForPush(cc *api.CloudClient, id string, files []migrationFile) ([]migrationFile, error) {
	size := 0
	for _, f := range files {
		size += len(f.text)
	}
	if len(files) <= maxPushFiles && size <= maxPushBytes {
		return files, nil
	}
	remote, err := remoteMigrations(cc, id)
	if err != nil {
		return nil, err
	}
	done := map[string]string{}
	for _, r := range remote {
		done[r.Name] = r.Checksum
	}
	var out []migrationFile
	size = 0
	for _, f := range files {
		if done[f.Name] == f.Checksum {
			continue
		}
		out = append(out, f)
		size += len(f.text)
	}
	if len(out) > maxPushFiles || size > maxPushBytes {
		return nil, fmt.Errorf("%d pending file(s), %d bytes: one push takes at most %d files and %d bytes; apply them in parts with --dir",
			len(out), size, maxPushFiles, maxPushBytes)
	}
	return out, nil
}

func (p *product) baasDBPushCmd() *cobra.Command {
	var dir string
	var dryRun bool
	cmd := &cobra.Command{
		Use:   "push [project-id]",
		Short: "Apply the migration files (*.sql) not applied yet, in name order, in one transaction",
		Long: `Apply every *.sql file of --dir (default arianet/migrations) that the project has not
recorded yet, in lexical name order (create them with arianet baas migration new). The pending
files run in one transaction, each followed by its record in baas_cli.migrations (name,
checksum, applied_at): if one fails, nothing is applied and the failing file is named. A file
that was applied and has changed since is refused: put the change in a new migration file.
Files must not contain BEGIN or COMMIT. --dry-run lists what would be applied.

In a region without the migrations endpoint, each file runs in its own transaction instead.
Needs the baas:sql:execute scope.`,
		Example: `  arianet baas db push
  arianet baas db push 12 --dir db/migrations --dry-run`,
		Args: projectArgsRange(0, 0),
		RunE: func(cmd *cobra.Command, args []string) error {
			args, err := withProject(args, 0, 0)
			if err != nil {
				return err
			}
			id := args[0]
			dir := migrationsDirOf(cmd, dir)
			files, err := readMigrations(dir)
			if errors.Is(err, fs.ErrNotExist) {
				return fmt.Errorf("no migrations directory %s: create a migration with arianet baas migration new <name>, or pass --dir", dir)
			}
			if err != nil {
				return err
			}
			cc := p.client()
			if cc == nil {
				return nil
			}
			send, err := trimForPush(cc, id, files)
			if err != nil {
				if _, ok := err.(*api.APIError); ok {
					handleSQLError(err)
					return nil
				}
				return err
			}
			body := struct {
				Migrations []map[string]string `json:"migrations"`
				DryRun     bool                `json:"dry_run"`
			}{Migrations: []map[string]string{}, DryRun: dryRun}
			for _, f := range send {
				body.Migrations = append(body.Migrations, map[string]string{"name": f.Name, "sql": f.text})
			}
			var raw json.RawMessage
			err = cc.Post("/projects/"+id+"/migrations", body, &raw)
			switch {
			case err == nil:
			case endpointMissing(err):
				fallbackNote("migrations")
				return pushViaSQL(cc, id, dir, files, dryRun)
			case apiCode(err) == "migration_changed":
				names := errorField(err, "migration")
				if names == "" {
					names = errorField(err, "migrations")
				}
				msg := "a migration file was applied and has changed since"
				if names != "" {
					msg = "these files were applied and have changed since: " + names
				}
				return fmt.Errorf("%s\nnothing was applied; put the change in a new migration file (arianet baas migration list shows which)", msg)
			case sqlErrorOf(err) != nil:
				if name := errorField(err, "migration"); name != "" {
					fmt.Fprintf(os.Stderr, "Applying %s failed; the transaction was rolled back and no migration was applied.\n", printer.CleanLine(name))
				}
				handleSQLError(err)
				return nil
			default:
				handleAPIError(err)
				return nil
			}
			if jsonMode() {
				printRawJSON(raw)
				return nil
			}
			var out struct {
				Data pushAnswer `json:"data"`
			}
			if err := json.Unmarshal(raw, &out); err != nil {
				return fmt.Errorf("decode response: %w", err)
			}
			res := out.Data
			switch {
			case dryRun && len(res.Pending) > 0:
				printer.Info(fmt.Sprintf("Would apply %d migration(s):", len(res.Pending)))
				for _, name := range res.Pending {
					printer.Info("  " + name)
				}
			case len(res.Applied) == 0:
				printer.Success(fmt.Sprintf("The database is up to date (%d migration(s) applied).", len(res.Skipped)+len(files)-len(send)))
			default:
				for _, name := range res.Applied {
					printer.Info("Applied " + name)
				}
				printer.Success(fmt.Sprintf("Applied %d migration(s) in one transaction.", len(res.Applied)))
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&dir, "dir", baasMigrationsDir, "directory of the migration files")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "list what would be applied; change nothing")
	return cmd
}

// fetchSchemaSQL is the project's DDL: the service's (GET schema) or, in
// a region without it, the snapshot built here from the table
// introspection.
func fetchSchemaSQL(cc *api.CloudClient, id string, schemas []string, at time.Time) (string, error) {
	var out struct {
		Data struct {
			SQL string `json:"sql"`
		} `json:"data"`
	}
	err := cc.Get("/projects/"+id+"/schema", url.Values{"schemas": {strings.Join(schemas, ",")}}, &out)
	if err == nil {
		return out.Data.SQL, nil
	}
	if !endpointMissing(err) {
		return "", err
	}
	fallbackNote("schema")
	return schemaSQLViaTables(cc, id, schemas, at)
}

// pullHeader opens a file db pull writes.
func pullHeader(id string, schemas []string, at time.Time) string {
	return fmt.Sprintf("-- Schema %s of Arianet BaaS project %s, written by arianet baas db pull at %s.\n"+
		"-- Review it before applying it to another project.\n\n", strings.Join(schemas, ", "), id, at.Format(time.RFC3339))
}

func (p *product) baasDBPullCmd() *cobra.Command {
	var dir, file string
	var schemas []string
	var mark bool
	cmd := &cobra.Command{
		Use:   "pull [project-id]",
		Short: "Write the schema (tables, views, functions, enums, policies...) as a SQL file",
		Long: `Write the DDL of the project's schemas (default public): extensions, schemas, enums,
sequences, tables with their columns, defaults and constraints, indexes, views, materialized
views, functions, triggers, row level security, policies and comments (no data, no grants).
The file goes to --dir (default arianet/migrations) as <timestamp>_remote_schema.sql, or to
--file (- for standard output). db diff compares the project with the newest such snapshot.

The snapshot describes the database as it is, so db push would fail applying it to the same
project: --mark-applied records it as applied there (needs baas:sql:execute).

In a region without the schema endpoint the snapshot is built here from the table
introspection: tables only (views are listed but not written, functions are missing).`,
		Example: `  arianet baas db pull
  arianet baas db pull 12 --schema public,app
  arianet baas db pull 12 --file - > schema.sql`,
		Args: projectArgsRange(0, 0),
		RunE: func(cmd *cobra.Command, args []string) error {
			args, err := withProject(args, 0, 0)
			if err != nil {
				return err
			}
			id := args[0]
			list := schemasParam(schemas)
			cc := p.client()
			if cc == nil {
				return nil
			}
			now := time.Now().UTC()
			sql, err := fetchSchemaSQL(cc, id, list, now)
			if err != nil {
				handleAPIError(err)
				return nil
			}
			text := sql
			if !strings.HasPrefix(text, "-- Schema ") {
				text = pullHeader(id, list, now) + text
			}
			if !strings.HasSuffix(text, "\n") {
				text += "\n"
			}
			if file == "-" {
				fmt.Print(text)
				return nil
			}
			path := file
			if path == "" {
				d := migrationsDirOf(cmd, dir)
				if err := os.MkdirAll(d, 0o755); err != nil {
					return err
				}
				path = filepath.Join(d, now.Format("20060102150405")+"_remote_schema.sql")
			}
			if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "Wrote the schema (%s) to %s\n", strings.Join(list, ", "), path)
			if mark {
				m := migrationFile{Name: filepath.Base(path), Checksum: checksum([]byte(text))}
				if err := markApplied(cc, id, m); err != nil {
					handleSQLError(err)
					return nil
				}
				fmt.Fprintf(os.Stderr, "Recorded %s as applied.\n", m.Name)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&dir, "dir", baasMigrationsDir, "directory to write the snapshot into")
	cmd.Flags().StringVar(&file, "file", "", "write to this file instead (- for standard output)")
	cmd.Flags().StringSliceVar(&schemas, "schema", []string{"public"}, "schemas to write (repeatable or comma separated)")
	cmd.Flags().BoolVar(&mark, "mark-applied", false, "record the snapshot as applied in the project, so db push skips it")
	return cmd
}

// newestSnapshot is the last *_remote_schema.sql of dir by name, or "".
func newestSnapshot(dir string) string {
	matches, _ := filepath.Glob(filepath.Join(dir, "*_remote_schema.sql"))
	if len(matches) == 0 {
		return ""
	}
	sort.Strings(matches)
	return matches[len(matches)-1]
}

// stripLeadingComments drops the comment and blank lines a file opens
// with (db pull's header, the service's generated-at line).
func stripLeadingComments(text string) string {
	lines := strings.SplitAfter(text, "\n")
	i := 0
	for i < len(lines) {
		t := strings.TrimSpace(lines[i])
		if t != "" && !strings.HasPrefix(t, "--") {
			break
		}
		i++
	}
	return strings.Join(lines[i:], "")
}

// schemaLines are the lines db diff compares: without the leading
// comments, trailing spaces and trailing blank lines.
func schemaLines(text string) []string {
	text = strings.ReplaceAll(stripLeadingComments(text), "\r\n", "\n")
	lines := strings.Split(text, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRight(l, " \t")
	}
	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

type diffReport struct {
	Differences bool              `json:"differences"`
	Migrations  []migrationStatus `json:"migrations"`
	Schema      struct {
		Against   *string `json:"against"`
		Identical bool    `json:"identical"`
		Diff      string  `json:"diff"`
	} `json:"schema"`
}

func (p *product) baasDBDiffCmd() *cobra.Command {
	var dir, against string
	var schemas []string
	cmd := &cobra.Command{
		Use:   "diff [project-id]",
		Short: "Compare the migration files and the newest schema snapshot with the project",
		Long: `Compare the project with this directory:

  1. the migration files of --dir with the project's history: files not applied yet
     (pending), applied files changed since (changed), and recorded names without a file
     (remote only);
  2. the project's schema now (as db pull writes it) with the newest *_remote_schema.sql of
     --dir, or --against <file>, as a unified diff (- the file, + the project). Leading
     comment lines and trailing spaces are ignored.

Exit status: 0 when there is no difference, 2 when there is one, 1 on an error.`,
		Example: `  arianet baas db diff
  arianet baas db diff 12 --against schema.sql
  arianet baas db diff -o json`,
		Args: projectArgsRange(0, 0),
		RunE: func(cmd *cobra.Command, args []string) error {
			args, err := withProject(args, 0, 0)
			if err != nil {
				return err
			}
			id := args[0]
			dir := migrationsDirOf(cmd, dir)
			files, err := readMigrationsIfAny(dir)
			if err != nil {
				return err
			}
			snapshot := against
			if snapshot == "" {
				snapshot = newestSnapshot(dir)
			}
			var local []byte
			if snapshot != "" {
				if local, err = os.ReadFile(snapshot); err != nil {
					return err
				}
			}
			cc := p.client()
			if cc == nil {
				return nil
			}
			remote, err := remoteMigrations(cc, id)
			if err != nil {
				handleSQLError(err)
				return nil
			}
			var rep diffReport
			rep.Migrations = []migrationStatus{}
			for _, m := range migrationStatuses(files, remote) {
				if m.Status != migrationApplied {
					rep.Migrations = append(rep.Migrations, m)
				}
			}
			rep.Schema.Identical = true
			if snapshot != "" {
				list := schemasParam(schemas)
				sql, err := fetchSchemaSQL(cc, id, list, time.Now().UTC())
				if err != nil {
					handleAPIError(err)
					return nil
				}
				rep.Schema.Against = &snapshot
				rep.Schema.Diff = unifiedDiff(snapshot, "project "+id+" ("+strings.Join(list, ", ")+")", schemaLines(string(local)), schemaLines(sql))
				rep.Schema.Identical = rep.Schema.Diff == ""
			}
			rep.Differences = len(rep.Migrations) > 0 || !rep.Schema.Identical

			if jsonMode() {
				printer.New("json").PrintJSON(rep)
			} else {
				if len(rep.Migrations) == 0 {
					printer.Info(fmt.Sprintf("Migrations: every file of %s is applied, and the project has no other.", dir))
				} else {
					printer.Info("Migrations that differ:")
					printer.New(cfg.Output).Table(migrationHeaders, migrationRows(rep.Migrations))
				}
				switch {
				case snapshot == "":
					printer.Info(fmt.Sprintf("Schema: no *_remote_schema.sql in %s to compare with (write one with arianet baas db pull, or pass --against).", dir))
				case rep.Schema.Identical:
					printer.Info("Schema: the project matches " + snapshot + ".")
				default:
					printer.Info("Schema: the project differs from " + snapshot + ":")
					fmt.Print(rep.Schema.Diff)
				}
			}
			if rep.Differences {
				return &exitError{code: exitCodeDifferences}
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&dir, "dir", baasMigrationsDir, "directory of the migration files and snapshots")
	cmd.Flags().StringVar(&against, "against", "", "compare the schema with this file instead of the newest snapshot")
	cmd.Flags().StringSliceVar(&schemas, "schema", []string{"public"}, "schemas to compare (repeatable or comma separated)")
	return cmd
}

// ─── gen types ───────────────────────────────────────────────────────────────

func (p *product) baasGenCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "gen", Short: "Generate code from the project's database"}
	cmd.AddCommand(p.baasTypesCmd("types [project-id]", "typescript",
		"Generate TypeScript types of the database (Tables, Views, Functions, Enums)"))
	return cmd
}

// baasTypesCmd is gen types, and the older top-level types as its alias.
func (p *product) baasTypesCmd(use, defaultLang, short string) *cobra.Command {
	var lang, file string
	var schemas []string
	cmd := &cobra.Command{
		Use:   use,
		Short: short,
		Long: `Generate a TypeScript Database type of the project's schemas: per schema, Tables with Row
(what a select returns), Insert and Update, their Relationships, Views, Functions (Args,
Returns), Enums and CompositeTypes, plus the Tables<>, TablesInsert<>, TablesUpdate<> and
Enums<> helpers. Pass it to a typed client library as its database type parameter.

In a region without the types endpoint the types are generated here from the table
introspection (Tables and Views only).`,
		Example: `  arianet baas gen types --lang typescript --file src/database.types.ts
  arianet baas gen types 12 --schema public,app > types.ts`,
		Args: projectArgsRange(0, 0),
		RunE: func(cmd *cobra.Command, args []string) error {
			args, err := withProject(args, 0, 0)
			if err != nil {
				return err
			}
			id := args[0]
			if lang != "ts" && lang != "typescript" {
				return fmt.Errorf("--lang %q is not supported; use typescript", lang)
			}
			list := schemasParam(schemas)
			cc := p.client()
			if cc == nil {
				return nil
			}
			var out struct {
				Data struct {
					Types string `json:"types"`
				} `json:"data"`
			}
			var raw json.RawMessage
			err = cc.Get("/projects/"+id+"/types/typescript", url.Values{"schemas": {strings.Join(list, ",")}}, &raw)
			switch {
			case err == nil:
				if err := json.Unmarshal(raw, &out); err != nil {
					return fmt.Errorf("decode response: %w", err)
				}
			case endpointMissing(err):
				fallbackNote("types")
				if out.Data.Types, err = typesViaTables(cc, id, list); err != nil {
					handleAPIError(err)
					return nil
				}
				raw, _ = json.Marshal(map[string]any{"data": map[string]any{"lang": "typescript", "schemas": list, "types": out.Data.Types}})
			default:
				handleAPIError(err)
				return nil
			}
			if file == "" && jsonMode() {
				printRawJSON(raw)
				return nil
			}
			text := out.Data.Types
			if !strings.HasSuffix(text, "\n") {
				text += "\n"
			}
			if file == "" || file == "-" {
				fmt.Print(text)
				return nil
			}
			if d := filepath.Dir(file); d != "." {
				if err := os.MkdirAll(d, 0o755); err != nil {
					return err
				}
			}
			if err := os.WriteFile(file, []byte(text), 0o644); err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "Wrote the types of %s to %s\n", strings.Join(list, ", "), file)
			return nil
		},
	}
	cmd.Flags().StringVar(&lang, "lang", defaultLang, "output language: typescript (or ts)")
	cmd.Flags().StringVar(&file, "file", "", "write to this file instead of standard output")
	cmd.Flags().StringSliceVar(&schemas, "schema", []string{"public"}, "schemas to include (repeatable or comma separated)")
	return cmd
}
