package commands

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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

// sql, db push / pull and types: everything that reads or changes the
// project's database through the SQL editor endpoint (POST
// /projects/{id}/sql, one transaction per request) and the table
// introspection (GET /projects/{id}/tables).

// baasSQLResult is the SQL endpoint's data.
type baasSQLResult struct {
	Statements []struct {
		Index        int    `json:"index"`
		Command      string `json:"command"`
		RowsAffected int64  `json:"rows_affected"`
		ReturnedRows bool   `json:"returned_rows"`
	} `json:"statements"`
	Result *struct {
		Columns []struct {
			Name string `json:"name"`
			Type string `json:"type"`
		} `json:"columns"`
		Rows      [][]json.RawMessage `json:"rows"`
		RowCount  int64               `json:"row_count"`
		Truncated bool                `json:"truncated"`
	} `json:"result"`
	DurationMS int64 `json:"duration_ms"`
	ReadOnly   bool  `json:"read_only"`
}

// baasSQLError is the database's refusal (422 sql_error).
type baasSQLError struct {
	SQLState  string `json:"sqlstate"`
	Message   string `json:"message"`
	Detail    string `json:"detail"`
	Hint      string `json:"hint"`
	Position  int    `json:"position"`
	Statement int    `json:"statement"`
}

func runBaaSSQL(cc *api.CloudClient, id, query string, readOnly bool) (*baasSQLResult, json.RawMessage, error) {
	var raw json.RawMessage
	body := map[string]any{"query": query}
	if readOnly {
		body["read_only"] = true
	}
	if err := cc.Post("/projects/"+id+"/sql", body, &raw); err != nil {
		return nil, nil, err
	}
	var out struct {
		Data baasSQLResult `json:"data"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, raw, fmt.Errorf("decode response: %w", err)
	}
	return &out.Data, raw, nil
}

// sqlErrorOf is the database's refusal inside err, if that is what it is.
func sqlErrorOf(err error) *baasSQLError {
	var apiErr *api.APIError
	if !errors.As(err, &apiErr) || apiErr.Code != "sql_error" || len(apiErr.Body) == 0 {
		return nil
	}
	var body struct {
		SQLError *baasSQLError `json:"sql_error"`
	}
	if json.Unmarshal(apiErr.Body, &body) != nil {
		return nil
	}
	return body.SQLError
}

// handleSQLError prints the database's refusal with its position and hint,
// or any other error as usual, and exits.
func handleSQLError(err error) {
	e := sqlErrorOf(err)
	if e == nil {
		handleAPIError(err)
		return
	}
	if jsonMode() {
		var apiErr *api.APIError
		errors.As(err, &apiErr)
		fmt.Fprintln(os.Stderr, string(apiErr.Body))
		os.Exit(1)
	}
	printer.Error(fmt.Sprintf("%s (SQLSTATE %s)", e.Message, e.SQLState))
	if e.Statement >= 0 {
		fmt.Fprintf(os.Stderr, "  Statement: %d\n", e.Statement+1)
	}
	if e.Position > 0 {
		fmt.Fprintf(os.Stderr, "  Position:  %d\n", e.Position)
	}
	if e.Detail != "" {
		fmt.Fprintf(os.Stderr, "  Detail:    %s\n", printer.CleanLine(e.Detail))
	}
	if e.Hint != "" {
		fmt.Fprintf(os.Stderr, "  Hint:      %s\n", printer.CleanLine(e.Hint))
	}
	os.Exit(1)
}

// sqlValue renders one result value: text as is, NULL, or the JSON text.
func sqlValue(v json.RawMessage) string {
	s := strings.TrimSpace(string(v))
	if s == "null" || s == "" {
		return "NULL"
	}
	if strings.HasPrefix(s, `"`) {
		var str string
		if json.Unmarshal(v, &str) == nil {
			return str
		}
	}
	return s
}

func printSQLResult(r *baasSQLResult) {
	if r.Result != nil {
		headers := make([]string, len(r.Result.Columns))
		for i, c := range r.Result.Columns {
			headers[i] = c.Name
		}
		rows := make([][]string, len(r.Result.Rows))
		for i, row := range r.Result.Rows {
			rows[i] = make([]string, len(row))
			for j, v := range row {
				rows[i][j] = sqlValue(v)
			}
		}
		if len(rows) == 0 {
			printer.Info("(no rows)")
		} else {
			printer.New(cfg.Output).Table(headers, rows)
		}
		if r.Result.Truncated {
			printer.Warn(fmt.Sprintf("Only the first %d rows are shown (at most 1000 rows / 5 MB).", len(rows)))
		}
	}
	for _, st := range r.Statements {
		if st.ReturnedRows {
			continue
		}
		printer.Info(fmt.Sprintf("%s (%d rows)", st.Command, st.RowsAffected))
	}
	mode := ""
	if r.ReadOnly {
		mode = ", read only"
	}
	printer.Info(fmt.Sprintf("%d statement(s) in %d ms%s.", len(r.Statements), r.DurationMS, mode))
}

func (p *product) baasSQLCmd() *cobra.Command {
	var file string
	var readOnly, asJSON bool
	cmd := &cobra.Command{
		Use:   "sql <project-id> [query]",
		Short: "Run SQL as the project's owner (one transaction) and print the result",
		Long: `Run SQL as the project's owner: every statement in one transaction (no BEGIN or COMMIT
inside), a 15 second statement timeout (the plan's when longer). Prints the rows of the last
statement that returned rows (at most 1000) and every statement's command tag. Every run is
recorded in your activity log. Needs the baas:sql:execute scope.`,
		Example: `  arianet baas sql 12 "select id, title from todos limit 5"
  arianet baas sql 12 -f seed.sql
  cat report.sql | arianet baas sql 12 -f - --read-only --json`,
		Args: cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := projectID(args[0])
			if err != nil {
				return err
			}
			var query string
			switch {
			case file != "" && len(args) == 2:
				return fmt.Errorf("give the query as an argument or with -f, not both")
			case file == "-":
				b, err := io.ReadAll(os.Stdin)
				if err != nil {
					return err
				}
				query = string(b)
			case file != "":
				b, err := os.ReadFile(file)
				if err != nil {
					return err
				}
				query = string(b)
			case len(args) == 2:
				query = args[1]
			}
			if strings.TrimSpace(query) == "" {
				return fmt.Errorf("no SQL given: pass a query or -f <file> (- for standard input)")
			}
			cc := p.client()
			if cc == nil {
				return nil
			}
			res, raw, err := runBaaSSQL(cc, id, query, readOnly)
			if err != nil {
				handleSQLError(err)
				return nil
			}
			if asJSON || jsonMode() {
				printRawJSON(raw)
				return nil
			}
			printSQLResult(res)
			return nil
		},
	}
	cmd.Flags().StringVarP(&file, "file", "f", "", "read the SQL from this file (- for standard input)")
	cmd.Flags().BoolVar(&readOnly, "read-only", false, "run in a READ ONLY transaction")
	cmd.Flags().BoolVar(&asJSON, "json", false, "print the service's answer as JSON (same as -o json)")
	return cmd
}

// ─── db push / pull ──────────────────────────────────────────────────────────

const baasMigrationsTable = "baas_cli.migrations"

const baasMigrationsDDL = `CREATE SCHEMA IF NOT EXISTS baas_cli;
CREATE TABLE IF NOT EXISTS baas_cli.migrations (
  name text PRIMARY KEY,
  checksum text NOT NULL,
  applied_at timestamptz NOT NULL DEFAULT now()
)`

const baasMigrationsSelect = `SELECT name, checksum FROM baas_cli.migrations ORDER BY name`

type migrationFile struct {
	Name     string
	Checksum string
	text     string
}

func checksum(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func sqlLiteral(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }

// readMigrations lists dir's *.sql files in lexical order.
func readMigrations(dir string) ([]migrationFile, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []migrationFile
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, err
		}
		out = append(out, migrationFile{Name: e.Name(), Checksum: checksum(b), text: string(b)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// appliedMigrations reads the tracking table, creating it unless dryRun
// (a missing table is then simply nothing applied yet).
func appliedMigrations(cc *api.CloudClient, id string, dryRun bool) (map[string]string, error) {
	query := baasMigrationsDDL + ";\n" + baasMigrationsSelect
	if dryRun {
		query = baasMigrationsSelect
	}
	res, _, err := runBaaSSQL(cc, id, query, dryRun)
	if err != nil {
		if e := sqlErrorOf(err); dryRun && e != nil && (e.SQLState == "42P01" || e.SQLState == "3F000") {
			return map[string]string{}, nil
		}
		return nil, err
	}
	applied := map[string]string{}
	if res.Result != nil {
		for _, row := range res.Result.Rows {
			if len(row) >= 2 {
				applied[sqlValue(row[0])] = sqlValue(row[1])
			}
		}
	}
	return applied, nil
}

// recordMigration is the statement that marks a file applied.
func recordMigration(m migrationFile) string {
	return "INSERT INTO " + baasMigrationsTable + " (name, checksum) VALUES (" + sqlLiteral(m.Name) + ", " + sqlLiteral(m.Checksum) + ")"
}

// pushPlan compares the files with what the database recorded: the files to
// apply, those already applied, the applied files edited since, and the
// recorded names no file has.
func pushPlan(files []migrationFile, applied map[string]string) (pending, done, edited []migrationFile, remoteOnly []string) {
	local := map[string]bool{}
	for _, f := range files {
		local[f.Name] = true
		sum, ok := applied[f.Name]
		switch {
		case !ok:
			pending = append(pending, f)
		case sum != f.Checksum:
			edited = append(edited, f)
		default:
			done = append(done, f)
		}
	}
	for name := range applied {
		if !local[name] {
			remoteOnly = append(remoteOnly, name)
		}
	}
	sort.Strings(remoteOnly)
	return pending, done, edited, remoteOnly
}

func migrationNames(files []migrationFile) []string {
	out := make([]string, len(files))
	for i, f := range files {
		out[i] = f.Name
	}
	return out
}

func (p *product) baasDBCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "db", Short: "Apply migration files to the database, or write its schema to a file"}
	cmd.AddCommand(p.baasDBPushCmd(), p.baasDBPullCmd())
	return cmd
}

func (p *product) baasDBPushCmd() *cobra.Command {
	var dir string
	var dryRun bool
	cmd := &cobra.Command{
		Use:   "push <project-id>",
		Short: "Apply the migration files (*.sql) not applied yet, in name order",
		Long: `Apply every *.sql file of --dir that the project has not recorded yet, in lexical name
order (name them e.g. 20261121093000_create_todos.sql). Each file runs in its own transaction
together with its record in baas_cli.migrations (name, checksum, applied_at), so a file is
applied completely or not at all. A file that was applied and has changed since is refused:
put the change in a new migration file. Files must not contain BEGIN or COMMIT. Needs
the baas:sql:execute scope.`,
		Example: `  arianet baas db push 12
  arianet baas db push 12 --dir db/migrations --dry-run`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := projectID(args[0])
			if err != nil {
				return err
			}
			files, err := readMigrations(dir)
			if err != nil {
				return err
			}
			cc := p.client()
			if cc == nil {
				return nil
			}
			applied, err := appliedMigrations(cc, id, dryRun)
			if err != nil {
				handleSQLError(err)
				return nil
			}
			pending, done, edited, remoteOnly := pushPlan(files, applied)
			if len(edited) > 0 {
				return fmt.Errorf("these files were applied and have changed since: %s\nnothing was applied; put the change in a new migration file",
					strings.Join(migrationNames(edited), ", "))
			}
			for _, name := range remoteOnly {
				fmt.Fprintf(os.Stderr, "Warning: %s is recorded as applied but is not in %s\n", name, dir)
			}

			result := map[string]any{"dry_run": dryRun, "already_applied": migrationNames(done), "pending": migrationNames(pending), "applied": []string{}}
			if dryRun || len(pending) == 0 {
				switch {
				case jsonMode():
					printer.New("json").PrintJSON(result)
				case len(pending) == 0:
					printer.Success(fmt.Sprintf("The database is up to date (%d migration(s) applied).", len(done)))
				default:
					printer.Info(fmt.Sprintf("Would apply %d migration(s):", len(pending)))
					for _, f := range pending {
						printer.Info("  " + f.Name)
					}
				}
				return nil
			}

			appliedNow := []string{}
			for _, f := range pending {
				res, _, err := runBaaSSQL(cc, id, f.text+"\n;\n"+recordMigration(f), false)
				if err != nil {
					fmt.Fprintf(os.Stderr, "Applying %s failed and was rolled back (%d migration(s) before it were applied).\n", f.Name, len(appliedNow))
					handleSQLError(err)
					return nil
				}
				appliedNow = append(appliedNow, f.Name)
				if !jsonMode() {
					printer.Info(fmt.Sprintf("Applied %s (%d statement(s), %d ms)", f.Name, len(res.Statements)-1, res.DurationMS))
				}
			}
			if jsonMode() {
				result["applied"] = appliedNow
				printer.New("json").PrintJSON(result)
				return nil
			}
			printer.Success(fmt.Sprintf("Applied %d migration(s).", len(appliedNow)))
			return nil
		},
	}
	cmd.Flags().StringVar(&dir, "dir", "migrations", "directory of the migration files")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "list what would be applied; change nothing")
	return cmd
}

func (p *product) baasDBPullCmd() *cobra.Command {
	var dir, file, schema string
	var markApplied bool
	cmd := &cobra.Command{
		Use:   "pull <project-id>",
		Short: "Write the schema (tables, keys, indexes, row level security, policies) as a SQL file",
		Long: `Write a SQL snapshot of one schema of the project's database (default public): its tables
with their columns, primary, unique, check and foreign keys, indexes, row level security,
policies, triggers and comments. The file goes to --dir as <timestamp>_remote_schema.sql (or
to --file, - for standard output).

The snapshot describes the database as it is, so db push would fail applying it to the same
project: --mark-applied records it as applied there (needs baas:sql:execute). Views are listed
but not written (their definition is not part of the table introspection), and the functions
that triggers call are not included.`,
		Example: `  arianet baas db pull 12
  arianet baas db pull 12 --file - > schema.sql`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := projectID(args[0])
			if err != nil {
				return err
			}
			cc := p.client()
			if cc == nil {
				return nil
			}
			tables, err := fetchBaaSTables(cc, id, schema)
			if err != nil {
				handleAPIError(err)
				return nil
			}
			now := time.Now().UTC()
			text := schemaSQL(id, schema, tables, now)
			if file == "-" {
				fmt.Print(text)
				return nil
			}
			path := file
			if path == "" {
				if err := os.MkdirAll(dir, 0o755); err != nil {
					return err
				}
				path = filepath.Join(dir, now.Format("20060102150405")+"_remote_schema.sql")
			}
			if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "Wrote the schema of %d table(s) to %s\n", len(tables), path)
			if markApplied {
				m := migrationFile{Name: filepath.Base(path), Checksum: checksum([]byte(text))}
				if _, _, err := runBaaSSQL(cc, id, baasMigrationsDDL+";\n"+recordMigration(m), false); err != nil {
					handleSQLError(err)
					return nil
				}
				fmt.Fprintf(os.Stderr, "Recorded %s as applied.\n", m.Name)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&dir, "dir", "migrations", "directory to write the snapshot into")
	cmd.Flags().StringVar(&file, "file", "", "write to this file instead (- for standard output)")
	cmd.Flags().StringVar(&schema, "schema", "public", "the schema to write")
	cmd.Flags().BoolVar(&markApplied, "mark-applied", false, "record the snapshot as applied in the project, so db push skips it")
	return cmd
}

// ─── table introspection ─────────────────────────────────────────────────────

type baasColumn struct {
	Name      string  `json:"name"`
	Type      string  `json:"type"`
	Nullable  bool    `json:"nullable"`
	Default   *string `json:"default"`
	Identity  *string `json:"identity"`
	Generated bool    `json:"generated"`
	Comment   *string `json:"comment"`
}

type baasConstraint struct {
	Name       string   `json:"name"`
	Type       string   `json:"type"`
	Columns    []string `json:"columns"`
	Definition string   `json:"definition"`
}

type baasIndex struct {
	Name       string `json:"name"`
	Primary    bool   `json:"primary"`
	Definition string `json:"definition"`
}

type baasPolicy struct {
	Name       string   `json:"name"`
	Command    string   `json:"command"`
	Permissive bool     `json:"permissive"`
	Roles      []string `json:"roles"`
	Using      *string  `json:"using"`
	WithCheck  *string  `json:"with_check"`
}

type baasTrigger struct {
	Name       string `json:"name"`
	Enabled    bool   `json:"enabled"`
	Definition string `json:"definition"`
}

type baasTable struct {
	Schema      string           `json:"schema"`
	Name        string           `json:"name"`
	Kind        string           `json:"kind"`
	RLSEnabled  bool             `json:"rls_enabled"`
	RLSForced   bool             `json:"rls_forced"`
	Comment     *string          `json:"comment"`
	Columns     []baasColumn     `json:"columns"`
	PrimaryKey  []string         `json:"primary_key"`
	Constraints []baasConstraint `json:"constraints"`
	Indexes     []baasIndex      `json:"indexes"`
	Policies    []baasPolicy     `json:"policies"`
	Triggers    []baasTrigger    `json:"triggers"`
}

func (t baasTable) isTable() bool {
	return t.Kind == "table" || t.Kind == "partitioned_table"
}

// fetchBaaSTables reads every table and view of schema with its details,
// in name order.
func fetchBaaSTables(cc *api.CloudClient, id, schema string) ([]baasTable, error) {
	var list struct {
		Data []struct {
			Schema string `json:"schema"`
			Name   string `json:"name"`
		} `json:"data"`
	}
	if err := cc.Get("/projects/"+id+"/tables", url.Values{"schema": {schema}}, &list); err != nil {
		return nil, err
	}
	out := make([]baasTable, 0, len(list.Data))
	for _, t := range list.Data {
		s := t.Schema
		if s == "" {
			s = schema
		}
		var detail struct {
			Data baasTable `json:"data"`
		}
		if err := cc.Get("/projects/"+id+"/tables/"+url.PathEscape(s+"."+t.Name), nil, &detail); err != nil {
			return nil, err
		}
		out = append(out, detail.Data)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

var plainIdent = regexp.MustCompile(`^[a-z_][a-z0-9_]*$`)

// reservedWords are the keywords that cannot be a bare table or column name.
var reservedWords = map[string]bool{}

func init() {
	for _, w := range strings.Fields(`all analyse analyze and any array as asc asymmetric both case cast check collate column
		constraint create current_catalog current_date current_role current_time current_timestamp current_user default
		deferrable desc distinct do else end except false fetch for foreign from grant group having in initially intersect
		into lateral leading limit localtime localtimestamp not null offset on only or order placing primary references
		returning select session_user some symmetric table then to trailing true union unique user using variadic when
		where window with`) {
		reservedWords[w] = true
	}
}

// quoteIdent quotes a name when SQL needs it.
func quoteIdent(name string) string {
	if plainIdent.MatchString(name) && !reservedWords[name] {
		return name
	}
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

var serialDefault = regexp.MustCompile(`^nextval\('[^']+'::regclass\)$`)

// columnSQL is one column of CREATE TABLE, and a note when the snapshot
// cannot carry all of it.
func columnSQL(c baasColumn) (line, note string) {
	typ := c.Type
	def := ""
	if c.Default != nil {
		def = *c.Default
	}
	// A serial column's sequence belongs to the column: written as serial,
	// it is created with the table.
	if serialDefault.MatchString(def) {
		switch typ {
		case "integer":
			typ, def = "serial", ""
		case "bigint":
			typ, def = "bigserial", ""
		case "smallint":
			typ, def = "smallserial", ""
		}
	}
	line = quoteIdent(c.Name) + " " + typ
	switch {
	case c.Identity != nil && *c.Identity == "always":
		line += " GENERATED ALWAYS AS IDENTITY"
	case c.Identity != nil:
		line += " GENERATED BY DEFAULT AS IDENTITY"
	case def != "":
		line += " DEFAULT " + def
	}
	if !c.Nullable {
		line += " NOT NULL"
	}
	if c.Generated {
		note = "generated column: its expression is not part of the snapshot"
	}
	return line, note
}

func policySQL(table string, pol baasPolicy) string {
	kind := "PERMISSIVE"
	if !pol.Permissive {
		kind = "RESTRICTIVE"
	}
	roles := make([]string, 0, len(pol.Roles))
	for _, r := range pol.Roles {
		if r == "public" {
			roles = append(roles, "PUBLIC")
		} else {
			roles = append(roles, quoteIdent(r))
		}
	}
	if len(roles) == 0 {
		roles = []string{"PUBLIC"}
	}
	s := fmt.Sprintf("CREATE POLICY %s ON %s AS %s FOR %s TO %s", quoteIdent(pol.Name), table, kind, pol.Command, strings.Join(roles, ", "))
	if pol.Using != nil {
		s += " USING (" + *pol.Using + ")"
	}
	if pol.WithCheck != nil {
		s += " WITH CHECK (" + *pol.WithCheck + ")"
	}
	return s + ";\n"
}

// schemaSQL writes the snapshot db pull saves: every table, then the
// foreign keys once all tables exist.
func schemaSQL(id, schema string, tables []baasTable, at time.Time) string {
	var b strings.Builder
	q := quoteIdent(schema)
	fmt.Fprintf(&b, "-- Schema %s of Arianet BaaS project %s, written by arianet baas db pull at %s.\n", schema, id, at.Format(time.RFC3339))
	b.WriteString("-- Review it before applying it to another project.\n\n")
	if schema != "public" {
		fmt.Fprintf(&b, "CREATE SCHEMA IF NOT EXISTS %s;\n\n", q)
	}
	var foreign []string
	for _, t := range tables {
		name := q + "." + quoteIdent(t.Name)
		if !t.isTable() {
			fmt.Fprintf(&b, "-- %s %s: not written (its definition is not part of the table introspection).\n\n", strings.ReplaceAll(t.Kind, "_", " "), name)
			continue
		}
		fmt.Fprintf(&b, "CREATE TABLE %s (\n", name)
		for i, c := range t.Columns {
			line, note := columnSQL(c)
			if i < len(t.Columns)-1 {
				line += ","
			}
			if note != "" {
				line += " -- " + note
			}
			b.WriteString("  " + line + "\n")
		}
		b.WriteString(");\n")

		constraintNames := map[string]bool{}
		for _, c := range t.Constraints {
			constraintNames[c.Name] = true
			stmt := fmt.Sprintf("ALTER TABLE %s ADD CONSTRAINT %s %s;\n", name, quoteIdent(c.Name), c.Definition)
			switch c.Type {
			case "not_null": // part of the column
			case "foreign_key":
				foreign = append(foreign, stmt)
			default:
				b.WriteString(stmt)
			}
		}
		for _, ix := range t.Indexes {
			if !ix.Primary && !constraintNames[ix.Name] {
				b.WriteString(ix.Definition + ";\n")
			}
		}
		if t.Comment != nil && *t.Comment != "" {
			fmt.Fprintf(&b, "COMMENT ON TABLE %s IS %s;\n", name, sqlLiteral(*t.Comment))
		}
		for _, c := range t.Columns {
			if c.Comment != nil && *c.Comment != "" {
				fmt.Fprintf(&b, "COMMENT ON COLUMN %s.%s IS %s;\n", name, quoteIdent(c.Name), sqlLiteral(*c.Comment))
			}
		}
		if t.RLSEnabled {
			fmt.Fprintf(&b, "ALTER TABLE %s ENABLE ROW LEVEL SECURITY;\n", name)
		}
		if t.RLSForced {
			fmt.Fprintf(&b, "ALTER TABLE %s FORCE ROW LEVEL SECURITY;\n", name)
		}
		for _, pol := range t.Policies {
			b.WriteString(policySQL(name, pol))
		}
		for _, tg := range t.Triggers {
			b.WriteString(tg.Definition + ";\n")
			if !tg.Enabled {
				fmt.Fprintf(&b, "ALTER TABLE %s DISABLE TRIGGER %s;\n", name, quoteIdent(tg.Name))
			}
		}
		b.WriteString("\n")
	}
	if len(foreign) > 0 {
		b.WriteString("-- Foreign keys, once every table exists.\n")
		for _, f := range foreign {
			b.WriteString(f)
		}
	}
	return b.String()
}

// ─── types ───────────────────────────────────────────────────────────────────

func (p *product) baasTypesCmd() *cobra.Command {
	var lang string
	var schemas []string
	cmd := &cobra.Command{
		Use:   "types <project-id>",
		Short: "Generate TypeScript types of the database (Tables: Row, Insert, Update)",
		Long: `Generate a TypeScript Database type from the project's tables: per schema, Tables with
Row (what a select returns), Insert (columns with a default, an identity or NULL allowed are
optional) and Update (every column optional), Relationships from the foreign keys, and Views.
Pass it to a typed client library as its database type parameter.`,
		Example: `  arianet baas types 12 --lang ts > src/database.types.ts
  arianet baas types 12 --schema public,app > types.ts`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := projectID(args[0])
			if err != nil {
				return err
			}
			if lang != "ts" && lang != "typescript" {
				return fmt.Errorf("--lang %q is not supported; use ts", lang)
			}
			list := splitList(schemas)
			if len(list) == 0 {
				list = []string{"public"}
			}
			cc := p.client()
			if cc == nil {
				return nil
			}
			bySchema := map[string][]baasTable{}
			for _, s := range list {
				tables, err := fetchBaaSTables(cc, id, s)
				if err != nil {
					handleAPIError(err)
					return nil
				}
				bySchema[s] = tables
			}
			fmt.Print(typescriptTypes(list, bySchema))
			return nil
		},
	}
	cmd.Flags().StringVar(&lang, "lang", "ts", "output language: ts")
	cmd.Flags().StringSliceVar(&schemas, "schema", []string{"public"}, "schemas to include (repeatable or comma separated)")
	return cmd
}

var (
	typeModifier = regexp.MustCompile(`\([^)]*\)`)
	tsIdent      = regexp.MustCompile(`^[A-Za-z_$][A-Za-z0-9_$]*$`)
	foreignKeyRe = regexp.MustCompile(`^FOREIGN KEY \((.+?)\) REFERENCES (.+?)\((.+?)\)`)
)

// tsType maps a PostgreSQL type name (format_type) to TypeScript.
func tsType(pg string) string {
	base := strings.TrimSpace(pg)
	dims := 0
	for strings.HasSuffix(base, "[]") {
		base = strings.TrimSuffix(base, "[]")
		dims++
	}
	base = strings.TrimSpace(typeModifier.ReplaceAllString(base, ""))
	var ts string
	switch base {
	case "smallint", "integer", "bigint", "real", "double precision", "numeric", "decimal",
		"int2", "int4", "int8", "float4", "float8", "oid":
		ts = "number"
	case "boolean", "bool":
		ts = "boolean"
	case "json", "jsonb":
		ts = "Json"
	default:
		ts = "string"
	}
	return ts + strings.Repeat("[]", dims)
}

// tsString is a TypeScript string literal.
func tsString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// tsKey is a property name, quoted when it is not an identifier.
func tsKey(name string) string {
	if tsIdent.MatchString(name) {
		return name
	}
	return tsString(name)
}

func tsStrings(items []string) string {
	quoted := make([]string, len(items))
	for i, s := range items {
		quoted[i] = tsString(s)
	}
	return "[" + strings.Join(quoted, ", ") + "]"
}

// unquoteIdent turns "Name" back into Name.
func unquoteIdent(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		return strings.ReplaceAll(s[1:len(s)-1], `""`, `"`)
	}
	return s
}

func identList(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, len(parts))
	for i, p := range parts {
		out[i] = unquoteIdent(p)
	}
	return out
}

// relationName is the table of "schema.table", "table" or "s"."t".
func relationName(ref string) string {
	ref = strings.TrimSpace(ref)
	if strings.HasSuffix(ref, `"`) {
		if i := strings.LastIndex(ref[:len(ref)-1], `"`); i >= 0 {
			return unquoteIdent(ref[i:])
		}
	}
	if i := strings.LastIndex(ref, "."); i >= 0 {
		ref = ref[i+1:]
	}
	return unquoteIdent(ref)
}

func sameColumns(a, b []string) bool {
	if len(a) != len(b) || len(a) == 0 {
		return false
	}
	as, bs := append([]string(nil), a...), append([]string(nil), b...)
	sort.Strings(as)
	sort.Strings(bs)
	for i := range as {
		if as[i] != bs[i] {
			return false
		}
	}
	return true
}

func writeColumns(b *strings.Builder, indent string, cols []baasColumn, mode string) {
	for _, c := range cols {
		typ := tsType(c.Type)
		if c.Nullable {
			typ += " | null"
		}
		readOnly := c.Generated || (c.Identity != nil && *c.Identity == "always")
		switch {
		case mode == "Row":
			fmt.Fprintf(b, "%s%s: %s\n", indent, tsKey(c.Name), typ)
		case readOnly:
			fmt.Fprintf(b, "%s%s?: never\n", indent, tsKey(c.Name))
		case mode == "Update" || c.Nullable || c.Default != nil || c.Identity != nil:
			fmt.Fprintf(b, "%s%s?: %s\n", indent, tsKey(c.Name), typ)
		default:
			fmt.Fprintf(b, "%s%s: %s\n", indent, tsKey(c.Name), typ)
		}
	}
}

func writeRelationships(b *strings.Builder, indent string, t baasTable) {
	var entries []string
	for _, c := range t.Constraints {
		if c.Type != "foreign_key" {
			continue
		}
		m := foreignKeyRe.FindStringSubmatch(c.Definition)
		if m == nil {
			continue
		}
		cols := identList(m[1])
		oneToOne := sameColumns(cols, t.PrimaryKey)
		for _, u := range t.Constraints {
			if u.Type == "unique" && sameColumns(cols, u.Columns) {
				oneToOne = true
			}
		}
		in := indent + "    "
		entries = append(entries, fmt.Sprintf("%s  {\n%sforeignKeyName: %s\n%scolumns: %s\n%sisOneToOne: %t\n%sreferencedRelation: %s\n%sreferencedColumns: %s\n%s  },\n",
			indent, in, tsString(c.Name), in, tsStrings(cols), in, oneToOne, in, tsString(relationName(m[2])), in, tsStrings(identList(m[3])), indent))
	}
	if len(entries) == 0 {
		fmt.Fprintf(b, "%sRelationships: []\n", indent)
		return
	}
	fmt.Fprintf(b, "%sRelationships: [\n%s%s]\n", indent, strings.Join(entries, ""), indent)
}

const tsEmptyMap = "{\n      [_ in never]: never\n    }\n"

// typescriptTypes writes the Database type of the schemas.
func typescriptTypes(schemas []string, bySchema map[string][]baasTable) string {
	var b strings.Builder
	b.WriteString("// Generated by arianet baas types. Do not edit: run the command again after a schema change.\n\n")
	b.WriteString("export type Json =\n  | string\n  | number\n  | boolean\n  | null\n  | { [key: string]: Json | undefined }\n  | Json[]\n\n")
	b.WriteString("export type Database = {\n")
	for _, s := range schemas {
		fmt.Fprintf(&b, "  %s: {\n", tsKey(s))
		var tables, views []baasTable
		for _, t := range bySchema[s] {
			if t.isTable() || t.Kind == "foreign_table" {
				tables = append(tables, t)
			} else {
				views = append(views, t)
			}
		}
		b.WriteString("    Tables: ")
		if len(tables) == 0 {
			b.WriteString(tsEmptyMap)
		} else {
			b.WriteString("{\n")
			for _, t := range tables {
				fmt.Fprintf(&b, "      %s: {\n", tsKey(t.Name))
				for _, mode := range []string{"Row", "Insert", "Update"} {
					fmt.Fprintf(&b, "        %s: {\n", mode)
					writeColumns(&b, "          ", t.Columns, mode)
					b.WriteString("        }\n")
				}
				writeRelationships(&b, "        ", t)
				b.WriteString("      }\n")
			}
			b.WriteString("    }\n")
		}
		b.WriteString("    Views: ")
		if len(views) == 0 {
			b.WriteString(tsEmptyMap)
		} else {
			b.WriteString("{\n")
			for _, v := range views {
				fmt.Fprintf(&b, "      %s: {\n        Row: {\n", tsKey(v.Name))
				writeColumns(&b, "          ", v.Columns, "Row")
				b.WriteString("        }\n        Relationships: []\n      }\n")
			}
			b.WriteString("    }\n")
		}
		for _, k := range []string{"Functions", "Enums", "CompositeTypes"} {
			fmt.Fprintf(&b, "    %s: %s", k, tsEmptyMap)
		}
		b.WriteString("  }\n")
	}
	b.WriteString("}\n")
	fmt.Fprintf(&b, `
type DefaultSchema = Database[%s]

export type Tables<T extends keyof DefaultSchema["Tables"]> = DefaultSchema["Tables"][T]["Row"]
export type TablesInsert<T extends keyof DefaultSchema["Tables"]> = DefaultSchema["Tables"][T]["Insert"]
export type TablesUpdate<T extends keyof DefaultSchema["Tables"]> = DefaultSchema["Tables"][T]["Update"]
`, tsString(schemas[0]))
	return b.String()
}
