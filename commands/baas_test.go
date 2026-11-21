package commands

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ariaservice/arianet-cli/internal/api"
	"github.com/ariaservice/arianet-cli/internal/config"
)

func baasBody(t *testing.T, r *http.Request) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil && err != io.EOF {
		t.Errorf("body: %v", err)
	}
	return body
}

func baasRunErr(t *testing.T, args ...string) error {
	t.Helper()
	cmd := newBaaSCmd()
	cmd.SetArgs(args)
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	return cmd.Execute()
}

const baasProjectJSON = `{"id":"12","ref":"abcdefghij0123456789","name":"shop","region":"thr-1","plan":"free","state":"active",
	"endpoint":"https://abcdefghij0123456789.baas.example","workspace_id":null,"suspension_reason":null,"paused_at":null,
	"last_request_at":null,"created_at":"2026-11-21T09:00:00Z","running_action":null}`

func TestBaaSProjectsListSendsTheRegion(t *testing.T) {
	productTestServer(t, "table", api.BaaSPrefix, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/baas/v1/projects" {
			t.Errorf("%s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get(api.RegionHeader); got != "thr-1" {
			t.Errorf("region header %q", got)
		}
		_, _ = io.WriteString(w, `{"data":[`+baasProjectJSON+`]}`)
	})
	out := runCaptured(t, newBaaSCmd(), "projects", "list", "--region", "thr-1")
	for _, want := range []string{"abcdefghij0123456789", "shop", "free", "active", "thr-1"} {
		if !strings.Contains(out, want) {
			t.Errorf("table lacks %q:\n%s", want, out)
		}
	}
}

func TestBaaSProjectsGet(t *testing.T) {
	productTestServer(t, "table", api.BaaSPrefix, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/baas/v1/projects/12" {
			t.Errorf("path %s", r.URL.Path)
		}
		_, _ = io.WriteString(w, `{"data":`+baasProjectJSON+`}`)
	})
	out := runCaptured(t, newBaaSCmd(), "projects", "get", "12")
	if !strings.Contains(out, "abcdefghij0123456789") || !strings.Contains(out, "Endpoint") {
		t.Errorf("output:\n%s", out)
	}
}

func TestBaaSProjectsCreateFollowsItsAction(t *testing.T) {
	productTestServer(t, "table", api.BaaSPrefix, func(w http.ResponseWriter, r *http.Request) {
		body := baasBody(t, r)
		if r.Method != http.MethodPost || r.URL.Path != "/baas/v1/projects" || body["name"] != "shop" || body["plan"] != "free" || len(body) != 2 {
			t.Errorf("%s %s %v", r.Method, r.URL.Path, body)
		}
		w.WriteHeader(http.StatusAccepted)
		_, _ = io.WriteString(w, `{"data":`+baasProjectJSON+`,"action":{"id":55,"command":"create","state":"running","progress":0,"project_id":"12"}}`)
	})
	out := runCaptured(t, newBaaSCmd(), "projects", "create", "--name", "shop", "--plan", "free")
	for _, want := range []string{"Project created", "abcdefghij0123456789", "Action #55 is running", "arianet baas actions wait 55"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
}

func TestBaaSProjectsCreateNeedsNameAndPlan(t *testing.T) {
	productTestServer(t, "table", api.BaaSPrefix, func(w http.ResponseWriter, r *http.Request) {
		t.Error("nothing may be sent")
	})
	if err := baasRunErr(t, "projects", "create", "--name", "shop"); err == nil || !strings.Contains(err.Error(), "--plan is required") {
		t.Fatalf("err = %v", err)
	}
}

func TestBaaSProjectsDeleteSendsTheTypedRef(t *testing.T) {
	productTestServer(t, "json", api.BaaSPrefix, func(w http.ResponseWriter, r *http.Request) {
		body := baasBody(t, r)
		if r.Method != http.MethodPost || r.URL.Path != "/baas/v1/projects/12/delete" || body["confirm"] != "abcdefghij0123456789" {
			t.Errorf("%s %s %v", r.Method, r.URL.Path, body)
		}
		w.WriteHeader(http.StatusAccepted)
		_, _ = io.WriteString(w, `{"action":{"id":56,"command":"delete","state":"running"}}`)
	})
	out := runCaptured(t, newBaaSCmd(), "projects", "delete", "12", "--confirm", "abcdefghij0123456789")
	if !strings.Contains(out, `"id": 56`) {
		t.Errorf("output:\n%s", out)
	}
	if err := baasRunErr(t, "projects", "delete", "12"); err == nil || !strings.Contains(err.Error(), "--confirm is required") {
		t.Fatalf("delete without --confirm: %v", err)
	}
}

func TestBaaSPauseResumeAndPlan(t *testing.T) {
	var got []string
	productTestServer(t, "json", api.BaaSPrefix, func(w http.ResponseWriter, r *http.Request) {
		body := baasBody(t, r)
		got = append(got, r.Method+" "+r.URL.Path+fmt.Sprint(body["plan"]))
		if strings.HasSuffix(r.URL.Path, "/plan") {
			_, _ = io.WriteString(w, `{"data":`+baasProjectJSON+`}`)
			return
		}
		w.WriteHeader(http.StatusAccepted)
		_, _ = io.WriteString(w, `{"action":{"id":57,"state":"running"}}`)
	})
	runCaptured(t, newBaaSCmd(), "projects", "pause", "12")
	runCaptured(t, newBaaSCmd(), "projects", "resume", "12")
	runCaptured(t, newBaaSCmd(), "projects", "plan", "12", "pro")
	want := []string{"POST /baas/v1/projects/12/pause<nil>", "POST /baas/v1/projects/12/resume<nil>", "PUT /baas/v1/projects/12/planpro"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("requests %v", got)
	}
	if err := baasRunErr(t, "projects", "plan", "1x", "pro"); err == nil || !strings.Contains(err.Error(), "invalid project ID") {
		t.Fatalf("bad id: %v", err)
	}
}

func TestBaaSKeys(t *testing.T) {
	var got []string
	productTestServer(t, "table", api.BaaSPrefix, func(w http.ResponseWriter, r *http.Request) {
		got = append(got, r.Method+" "+r.URL.Path)
		switch r.URL.Path {
		case "/baas/v1/projects/12/keys", "/baas/v1/projects/12/keys/rotate":
			_, _ = io.WriteString(w, `{"data":[{"kind":"anon","kid":"k2","preview":"eyJh…x1","state":"active","created_at":"2026-11-21T09:00:00Z","rotated_at":null}]}`)
		case "/baas/v1/projects/12/keys/anon/reveal":
			_, _ = io.WriteString(w, `{"data":{"kind":"anon","kid":"k2","key":"eyJhbGciOiJFUzI1NiJ9.full"}}`)
		default:
			t.Errorf("unexpected %s", r.URL.Path)
		}
	})
	out := runCaptured(t, newBaaSCmd(), "keys", "list", "12")
	if !strings.Contains(out, "eyJh…x1") || !strings.Contains(out, "active") {
		t.Errorf("list:\n%s", out)
	}
	out = runCaptured(t, newBaaSCmd(), "keys", "reveal", "12", "anon")
	if !strings.Contains(out, "eyJhbGciOiJFUzI1NiJ9.full") {
		t.Errorf("reveal:\n%s", out)
	}
	runCaptured(t, newBaaSCmd(), "keys", "rotate", "12", "--yes")
	want := "GET /baas/v1/projects/12/keys|POST /baas/v1/projects/12/keys/anon/reveal|POST /baas/v1/projects/12/keys/rotate"
	if strings.Join(got, "|") != want {
		t.Errorf("requests %v", got)
	}
}

const baasSQLAnswer = `{"data":{"statements":[{"index":0,"command":"INSERT","rows_affected":2,"returned_rows":false},
	{"index":1,"command":"SELECT","rows_affected":2,"returned_rows":true}],
	"result":{"columns":[{"name":"id","type":"int8"},{"name":"title","type":"text"},{"name":"meta","type":"jsonb"}],
	"rows":[[1,"milk",{"a":1}],[2,null,null]],"row_count":2,"truncated":false},"duration_ms":4,"read_only":false}}`

func TestBaaSSQLQueryArgument(t *testing.T) {
	productTestServer(t, "table", api.BaaSPrefix, func(w http.ResponseWriter, r *http.Request) {
		body := baasBody(t, r)
		if r.Method != http.MethodPost || r.URL.Path != "/baas/v1/projects/12/sql" || body["query"] != "select 1" || body["read_only"] != nil {
			t.Errorf("%s %s %v", r.Method, r.URL.Path, body)
		}
		_, _ = io.WriteString(w, baasSQLAnswer)
	})
	out := runCaptured(t, newBaaSCmd(), "sql", "12", "select 1")
	for _, want := range []string{"TITLE", "milk", `{"a":1}`, "NULL", "INSERT (2 rows)", "2 statement(s) in 4 ms"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
}

func TestBaaSSQLFileReadOnlyJSON(t *testing.T) {
	file := filepath.Join(t.TempDir(), "q.sql")
	if err := os.WriteFile(file, []byte("select * from todos;\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	productTestServer(t, "table", api.BaaSPrefix, func(w http.ResponseWriter, r *http.Request) {
		body := baasBody(t, r)
		if body["query"] != "select * from todos;\n" || body["read_only"] != true {
			t.Errorf("body %v", body)
		}
		_, _ = io.WriteString(w, baasSQLAnswer)
	})
	out := runCaptured(t, newBaaSCmd(), "sql", "12", "-f", file, "--read-only", "--json")
	if !strings.Contains(out, `"row_count": 2`) {
		t.Errorf("--json prints the answer:\n%s", out)
	}
}

func TestBaaSSQLNeedsAQuery(t *testing.T) {
	productTestServer(t, "table", api.BaaSPrefix, func(w http.ResponseWriter, r *http.Request) { t.Error("nothing may be sent") })
	if err := baasRunErr(t, "sql", "12"); err == nil || !strings.Contains(err.Error(), "no SQL given") {
		t.Fatalf("err = %v", err)
	}
	if err := baasRunErr(t, "sql", "12", "select 1", "-f", "x.sql"); err == nil || !strings.Contains(err.Error(), "not both") {
		t.Fatalf("err = %v", err)
	}
}

// fakeMigrationsDB answers the SQL endpoint like a database with the
// tracking table: it remembers what INSERT INTO baas_cli.migrations records.
type fakeMigrationsDB struct {
	mu       sync.Mutex
	exists   bool
	applied  [][2]string
	queries  []string
	readOnly []bool
}

var recordRe = regexp.MustCompile(`INSERT INTO baas_cli\.migrations \(name, checksum\) VALUES \('([^']*)', '([0-9a-f]+)'\)`)

func (db *fakeMigrationsDB) handler(t *testing.T) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		db.mu.Lock()
		defer db.mu.Unlock()
		if baasOlderRegion(w, r) {
			return
		}
		if r.URL.Path != "/baas/v1/projects/12/sql" {
			t.Errorf("path %s", r.URL.Path)
		}
		body := baasBody(t, r)
		q, _ := body["query"].(string)
		db.queries = append(db.queries, q)
		db.readOnly = append(db.readOnly, body["read_only"] == true)
		if strings.Contains(q, "CREATE TABLE IF NOT EXISTS baas_cli.migrations") {
			db.exists = true
		}
		if !db.exists {
			w.WriteHeader(http.StatusUnprocessableEntity)
			_, _ = io.WriteString(w, `{"error":"relation \"baas_cli.migrations\" does not exist","code":"sql_error",
				"sql_error":{"sqlstate":"42P01","message":"relation \"baas_cli.migrations\" does not exist","statement":0}}`)
			return
		}
		for _, m := range recordRe.FindAllStringSubmatch(q, -1) {
			db.applied = append(db.applied, [2]string{m[1], m[2]})
		}
		var rows []string
		if strings.Contains(q, "SELECT name, checksum") {
			for _, a := range db.applied {
				rows = append(rows, fmt.Sprintf("[%q,%q]", a[0], a[1]))
			}
			fmt.Fprintf(w, `{"data":{"statements":[{"index":0,"command":"SELECT","rows_affected":%d,"returned_rows":true}],
				"result":{"columns":[{"name":"name","type":"text"},{"name":"checksum","type":"text"}],"rows":[%s],"row_count":%d,"truncated":false},
				"duration_ms":1,"read_only":false}}`, len(rows), strings.Join(rows, ","), len(rows))
			return
		}
		_, _ = io.WriteString(w, `{"data":{"statements":[{"index":0,"command":"CREATE TABLE","rows_affected":0,"returned_rows":false},
			{"index":1,"command":"INSERT","rows_affected":1,"returned_rows":false}],"result":null,"duration_ms":3,"read_only":false}}`)
	}
}

func writeMigrations(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, text := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestBaaSDBPushAppliesPendingFilesInOrder(t *testing.T) {
	dir := writeMigrations(t, map[string]string{
		"002_add_done.sql":     "alter table todos add column done boolean not null default false;",
		"001_create_todos.sql": "create table todos (id bigint primary key, title text);",
		"notes.txt":            "not a migration",
	})
	db := &fakeMigrationsDB{exists: true, applied: [][2]string{{"001_create_todos.sql", checksum([]byte("create table todos (id bigint primary key, title text);"))}}}
	productTestServer(t, "table", api.BaaSPrefix, db.handler(t))

	out := runCaptured(t, newBaaSCmd(), "db", "push", "12", "--dir", dir)
	if !strings.Contains(out, "Applied 002_add_done.sql") || !strings.Contains(out, "Applied 1 migration(s).") || strings.Contains(out, "Applied 001") {
		t.Errorf("output:\n%s", out)
	}
	if len(db.queries) != 2 || !strings.Contains(db.queries[0], "CREATE SCHEMA IF NOT EXISTS baas_cli") {
		t.Fatalf("queries %q", db.queries)
	}
	apply := db.queries[1]
	if !strings.HasPrefix(apply, "alter table todos add column done") || !strings.Contains(apply, "INSERT INTO baas_cli.migrations") {
		t.Errorf("the file and its record go in one request: %q", apply)
	}
	if len(db.applied) != 2 || db.applied[1][0] != "002_add_done.sql" {
		t.Errorf("recorded %v", db.applied)
	}

	// Run again: nothing left.
	db.queries = nil
	out = runCaptured(t, newBaaSCmd(), "db", "push", "12", "--dir", dir)
	if !strings.Contains(out, "up to date (2 migration(s) applied)") || len(db.queries) != 1 {
		t.Errorf("second push:\n%s\nqueries %d", out, len(db.queries))
	}
}

func TestBaaSDBPushRefusesAnEditedFile(t *testing.T) {
	dir := writeMigrations(t, map[string]string{
		"001_create_todos.sql": "create table todos (id bigint primary key, title text not null);",
		"002_more.sql":         "select 1;",
	})
	db := &fakeMigrationsDB{exists: true, applied: [][2]string{{"001_create_todos.sql", checksum([]byte("create table todos (id bigint primary key);"))}}}
	productTestServer(t, "table", api.BaaSPrefix, db.handler(t))
	err := baasRunErr(t, "db", "push", "12", "--dir", dir)
	if err == nil || !strings.Contains(err.Error(), "001_create_todos.sql") || !strings.Contains(err.Error(), "nothing was applied") {
		t.Fatalf("err = %v", err)
	}
	if len(db.queries) != 1 {
		t.Errorf("only the tracking table is read: %q", db.queries)
	}
}

func TestBaaSDBPushDryRunChangesNothing(t *testing.T) {
	dir := writeMigrations(t, map[string]string{"001_a.sql": "create table a (id int);", "002_b.sql": "create table b (id int);"})
	db := &fakeMigrationsDB{}
	productTestServer(t, "json", api.BaaSPrefix, db.handler(t))
	out := runCaptured(t, newBaaSCmd(), "db", "push", "12", "--dir", dir, "--dry-run")
	var res struct {
		Pending []string `json:"pending"`
		DryRun  bool     `json:"dry_run"`
	}
	if err := json.Unmarshal([]byte(out), &res); err != nil || !res.DryRun || strings.Join(res.Pending, ",") != "001_a.sql,002_b.sql" {
		t.Errorf("output %s (%v)", out, err)
	}
	if len(db.queries) != 1 || !db.readOnly[0] || strings.Contains(db.queries[0], "CREATE") || db.exists {
		t.Errorf("a dry run only reads, read only: %q %v", db.queries, db.readOnly)
	}
}

const baasTodosDetail = `{"data":{"schema":"public","name":"todos","kind":"table","rls_enabled":true,"rls_forced":false,"row_estimate":0,"size_bytes":8192,
	"comment":"Things to do","columns":[
	{"position":1,"name":"id","type":"bigint","nullable":false,"default":null,"identity":"by_default","generated":false,"comment":null,"primary_key":true,"unique":false,"references":null},
	{"position":2,"name":"title","type":"character varying(200)","nullable":false,"default":null,"identity":null,"generated":false,"comment":"What","primary_key":false,"unique":false,"references":null},
	{"position":3,"name":"user_id","type":"uuid","nullable":true,"default":"auth.uid()","identity":null,"generated":false,"comment":null,"primary_key":false,"unique":false,"references":"public.profiles(id)"},
	{"position":4,"name":"tags","type":"text[]","nullable":true,"default":null,"identity":null,"generated":false,"comment":null,"primary_key":false,"unique":false,"references":null},
	{"position":5,"name":"meta","type":"jsonb","nullable":false,"default":"'{}'::jsonb","identity":null,"generated":false,"comment":null,"primary_key":false,"unique":false,"references":null},
	{"position":6,"name":"slug","type":"text","nullable":true,"default":null,"identity":null,"generated":true,"comment":null,"primary_key":false,"unique":false,"references":null}],
	"primary_key":["id"],
	"constraints":[{"name":"todos_pkey","type":"primary_key","columns":["id"],"definition":"PRIMARY KEY (id)"},
	{"name":"todos_user_id_fkey","type":"foreign_key","columns":["user_id"],"definition":"FOREIGN KEY (user_id) REFERENCES profiles(id) ON DELETE CASCADE"}],
	"indexes":[{"name":"todos_pkey","unique":true,"primary":true,"definition":"CREATE UNIQUE INDEX todos_pkey ON public.todos USING btree (id)"},
	{"name":"todos_user_idx","unique":false,"primary":false,"definition":"CREATE INDEX todos_user_idx ON public.todos USING btree (user_id)"}],
	"policies":[{"name":"own todos","command":"ALL","permissive":true,"roles":["authenticated"],"using":"(user_id = auth.uid())","with_check":"(user_id = auth.uid())"}],
	"triggers":[]}}`

const baasProfilesDetail = `{"data":{"schema":"public","name":"profiles","kind":"table","rls_enabled":false,"rls_forced":false,"comment":null,"columns":[
	{"position":1,"name":"id","type":"integer","nullable":false,"default":"nextval('profiles_id_seq'::regclass)","identity":null,"generated":false,"comment":null},
	{"position":2,"name":"order","type":"integer","nullable":true,"default":null,"identity":null,"generated":false,"comment":null}],
	"primary_key":["id"],"constraints":[{"name":"profiles_pkey","type":"primary_key","columns":["id"],"definition":"PRIMARY KEY (id)"}],
	"indexes":[],"policies":[],"triggers":[]}}`

// baasOlderRegion answers like a region without the schema, types and
// migrations endpoints: 404.
func baasOlderRegion(w http.ResponseWriter, r *http.Request) bool {
	switch {
	case strings.HasSuffix(r.URL.Path, "/migrations"), strings.HasSuffix(r.URL.Path, "/schema"), strings.HasSuffix(r.URL.Path, "/types/typescript"):
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, `{"error":"Not found.","code":"not_found"}`)
		return true
	}
	return false
}

func baasIntrospection(t *testing.T) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if baasOlderRegion(w, r) {
			return
		}
		switch r.URL.Path {
		case "/baas/v1/projects/12/tables":
			if r.URL.Query().Get("schema") != "public" {
				t.Errorf("schema %q", r.URL.RawQuery)
			}
			_, _ = io.WriteString(w, `{"data":[{"schema":"public","name":"todos","kind":"table"},{"schema":"public","name":"profiles","kind":"table"},
				{"schema":"public","name":"open_todos","kind":"view"}]}`)
		case "/baas/v1/projects/12/tables/public.todos":
			_, _ = io.WriteString(w, baasTodosDetail)
		case "/baas/v1/projects/12/tables/public.profiles":
			_, _ = io.WriteString(w, baasProfilesDetail)
		case "/baas/v1/projects/12/tables/public.open_todos":
			_, _ = io.WriteString(w, `{"data":{"schema":"public","name":"open_todos","kind":"view","columns":[
				{"position":1,"name":"id","type":"bigint","nullable":true,"default":null,"identity":null,"generated":false}],
				"primary_key":[],"constraints":[],"indexes":[],"policies":[],"triggers":[]}}`)
		default:
			t.Errorf("unexpected %s", r.URL.Path)
		}
	}
}

func TestBaaSDBPullWritesTheSchema(t *testing.T) {
	productTestServer(t, "table", api.BaaSPrefix, baasIntrospection(t))
	out := runCaptured(t, newBaaSCmd(), "db", "pull", "12", "--file", "-")
	for _, want := range []string{
		"CREATE TABLE public.profiles (\n  id serial NOT NULL,\n  \"order\" integer\n);",
		"  id bigint GENERATED BY DEFAULT AS IDENTITY NOT NULL,",
		"  title character varying(200) NOT NULL,",
		"  user_id uuid DEFAULT auth.uid(),",
		"  meta jsonb DEFAULT '{}'::jsonb NOT NULL,",
		"  slug text -- generated column",
		"ALTER TABLE public.todos ADD CONSTRAINT todos_pkey PRIMARY KEY (id);",
		"CREATE INDEX todos_user_idx ON public.todos USING btree (user_id);",
		"COMMENT ON TABLE public.todos IS 'Things to do';",
		"COMMENT ON COLUMN public.todos.title IS 'What';",
		"ALTER TABLE public.todos ENABLE ROW LEVEL SECURITY;",
		`CREATE POLICY "own todos" ON public.todos AS PERMISSIVE FOR ALL TO authenticated USING ((user_id = auth.uid())) WITH CHECK ((user_id = auth.uid()));`,
		"-- view public.open_todos: not written",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("snapshot lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "CREATE UNIQUE INDEX todos_pkey") {
		t.Error("the primary key's index comes with the constraint")
	}
	fk := strings.Index(out, "ADD CONSTRAINT todos_user_id_fkey FOREIGN KEY")
	if fk < strings.Index(out, "CREATE TABLE public.todos") || fk < strings.Index(out, "CREATE TABLE public.profiles") {
		t.Error("foreign keys come after every table")
	}
}

func TestBaaSDBPullWritesAFileAndCanMarkItApplied(t *testing.T) {
	db := &fakeMigrationsDB{}
	intro := baasIntrospection(t)
	productTestServer(t, "table", api.BaaSPrefix, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/sql") {
			db.handler(t)(w, r)
			return
		}
		intro(w, r)
	})
	dir := filepath.Join(t.TempDir(), "migrations")
	runCaptured(t, newBaaSCmd(), "db", "pull", "12", "--dir", dir, "--mark-applied")
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 || !strings.HasSuffix(entries[0].Name(), "_remote_schema.sql") {
		t.Fatalf("files %v (%v)", entries, err)
	}
	text, _ := os.ReadFile(filepath.Join(dir, entries[0].Name()))
	if len(db.applied) != 1 || db.applied[0][0] != entries[0].Name() || db.applied[0][1] != checksum(text) {
		t.Errorf("recorded %v", db.applied)
	}
}

func TestBaaSTypesTypeScript(t *testing.T) {
	productTestServer(t, "table", api.BaaSPrefix, baasIntrospection(t))
	out := runCaptured(t, newBaaSCmd(), "types", "12", "--lang", "ts")
	for _, want := range []string{
		"export type Json =",
		"export type Database = {\n  public: {\n    Tables: {\n      profiles: {",
		"        Row: {\n          id: number\n          title: string\n          user_id: string | null\n          tags: string[] | null\n          meta: Json\n          slug: string | null\n        }",
		"        Insert: {\n          id?: number\n          title: string\n          user_id?: string | null\n          tags?: string[] | null\n          meta?: Json\n          slug?: never\n        }",
		"        Update: {\n          id?: number\n          title?: string\n",
		"            foreignKeyName: \"todos_user_id_fkey\"\n            columns: [\"user_id\"]\n            isOneToOne: false\n            referencedRelation: \"profiles\"\n            referencedColumns: [\"id\"]",
		"    Views: {\n      open_todos: {\n        Row: {\n          id: number | null\n        }",
		"    Functions: {\n      [_ in never]: never\n    }",
		"    Enums: {",
		"    CompositeTypes: {",
		"export type Tables<T extends keyof DefaultSchema[\"Tables\"]>",
		"Insert: {\n          id?: number\n          order?: number | null\n        }",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("types lack %q:\n%s", want, out)
		}
	}
	if err := baasRunErr(t, "types", "12", "--lang", "go"); err == nil {
		t.Error("only ts is supported")
	}
}

func TestBaaSTypeMapping(t *testing.T) {
	cases := map[string]string{
		"integer": "number", "bigint": "number", "numeric(10,2)": "number", "double precision": "number",
		"boolean": "boolean", "jsonb": "Json", "json[]": "Json[]", "uuid": "string", "timestamp with time zone": "string",
		"character varying(20)[]": "string[]", "integer[][]": "number[][]", `"Mood"`: "string", "bytea": "string",
	}
	for pg, want := range cases {
		if got := tsType(pg); got != want {
			t.Errorf("tsType(%q) = %q, want %q", pg, got, want)
		}
	}
	if relationName(`public."My Table"`) != "My Table" || relationName("other.t") != "t" || relationName("t") != "t" {
		t.Error("relationName")
	}
}

func TestBaaSEnv(t *testing.T) {
	var revealed int
	productTestServer(t, "table", api.BaaSPrefix, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "GET /baas/v1/projects/12":
			_, _ = io.WriteString(w, `{"data":`+baasProjectJSON+`}`)
		case "POST /baas/v1/projects/12/keys/anon/reveal":
			revealed++
			_, _ = io.WriteString(w, `{"data":{"kind":"anon","kid":"k2","key":"eyJanon"}}`)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
	})
	out := runCaptured(t, newBaaSCmd(), "env", "12")
	for _, want := range []string{
		"ARIANET_BAAS_URL=https://abcdefghij0123456789.baas.example\n", "ARIANET_BAAS_ANON_KEY=eyJanon\n",
		"NEXT_PUBLIC_BAAS_URL=https://", "NEXT_PUBLIC_BAAS_ANON_KEY=eyJanon", "VITE_BAAS_URL=", "VITE_BAAS_ANON_KEY=eyJanon",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("dotenv lacks %q:\n%s", want, out)
		}
	}
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if !strings.HasPrefix(line, "#") && !regexp.MustCompile(`^[A-Z_]+=\S+$`).MatchString(line) {
			t.Errorf("not a dotenv line: %q", line)
		}
	}
	out = runCaptured(t, newBaaSCmd(), "env", "12", "--format", "json")
	var vars map[string]string
	if err := json.Unmarshal([]byte(out), &vars); err != nil || vars["ARIANET_BAAS_ANON_KEY"] != "eyJanon" || vars["VITE_BAAS_URL"] == "" {
		t.Errorf("json %s (%v)", out, err)
	}
	if revealed != 2 {
		t.Errorf("reveals %d", revealed)
	}
	if err := baasRunErr(t, "env", "12", "--format", "yaml"); err == nil {
		t.Error("an unknown format is refused")
	}
}

func TestBaaSStorageBucketsAndListing(t *testing.T) {
	var got []string
	productTestServer(t, "table", api.BaaSPrefix, func(w http.ResponseWriter, r *http.Request) {
		body := baasBody(t, r)
		got = append(got, r.Method+" "+r.URL.Path+"?"+r.URL.RawQuery)
		switch r.Method + " " + r.URL.Path {
		case "GET /baas/v1/projects/12/storage/buckets":
			_, _ = io.WriteString(w, `{"data":[{"id":"avatars","name":"avatars","public":true}]}`)
		case "POST /baas/v1/projects/12/storage/buckets":
			if body["name"] != "docs" || body["public"] != false || len(body["allowed_mime_types"].([]any)) != 2 {
				t.Errorf("body %v", body)
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(w, `{"data":{"name":"docs"}}`)
		case "GET /baas/v1/projects/12/storage/buckets/avatars/objects":
			_, _ = io.WriteString(w, `{"data":[{"name":"users","id":null,"folder":true},{"name":"me.png","id":"o1","folder":false,"size":2048,"mimetype":"image/png"}]}`)
		case "DELETE /baas/v1/projects/12/storage/buckets/avatars/objects":
			paths, _ := body["paths"].([]any)
			if len(paths) != 2 || paths[0] != "users/42/me.png" {
				t.Errorf("paths %v", body)
			}
			_, _ = io.WriteString(w, `{"data":{"deleted":["users/42/me.png","b.txt"]}}`)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
	})
	out := runCaptured(t, newBaaSCmd(), "storage", "buckets", "12")
	if !strings.Contains(out, "avatars") {
		t.Errorf("buckets:\n%s", out)
	}
	runCaptured(t, newBaaSCmd(), "storage", "buckets", "create", "12", "--name", "docs", "--public=false", "--types", "image/png,application/pdf")
	out = runCaptured(t, newBaaSCmd(), "storage", "ls", "12", "avatars", "users/", "--limit", "10")
	if !strings.Contains(out, "me.png") || !strings.Contains(out, "2048") {
		t.Errorf("ls:\n%s", out)
	}
	out = runCaptured(t, newBaaSCmd(), "storage", "rm", "12", "avatars", "/users/42/me.png", "b.txt", "--yes")
	if !strings.Contains(out, "Deleted 2 object(s)") {
		t.Errorf("rm:\n%s", out)
	}
	if got[2] != "GET /baas/v1/projects/12/storage/buckets/avatars/objects?limit=10&prefix=users%2F" {
		t.Errorf("ls request %q", got[2])
	}
}

func TestBaaSStorageUploadAndDownload(t *testing.T) {
	dir := t.TempDir()
	local := filepath.Join(dir, "report.pdf")
	if err := os.WriteFile(local, []byte("%PDF-1.7 hello"), 0o600); err != nil {
		t.Fatal(err)
	}
	productTestServer(t, "table", api.BaaSPrefix, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/baas/v1/projects/12/storage/buckets/docs/object" {
			t.Errorf("path %s", r.URL.Path)
		}
		if r.Header.Get(api.RegionHeader) != "thr-1" {
			t.Errorf("region %q", r.Header.Get(api.RegionHeader))
		}
		switch r.Method {
		case http.MethodPut:
			raw, _ := io.ReadAll(r.Body)
			if string(raw) != "%PDF-1.7 hello" || r.Header.Get("Content-Type") != "application/pdf" || r.ContentLength != 14 ||
				r.URL.Query().Get("path") != "2026/report.pdf" || r.URL.Query().Get("upsert") != "true" {
				t.Errorf("upload %q %s %d %s", raw, r.Header.Get("Content-Type"), r.ContentLength, r.URL.RawQuery)
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(w, `{"data":{"bucket":"docs","path":"2026/report.pdf","id":"o9"}}`)
		case http.MethodGet:
			if r.URL.Query().Get("path") != "2026/report.pdf" {
				t.Errorf("download %s", r.URL.RawQuery)
			}
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = io.WriteString(w, "%PDF-1.7 hello")
		}
	})
	out := runCaptured(t, newBaaSCmd(), "storage", "upload", "12", "docs", local, "2026/report.pdf", "--upsert", "--region", "thr-1")
	if !strings.Contains(out, "Uploaded") || !strings.Contains(out, "14 bytes") {
		t.Errorf("upload:\n%s", out)
	}
	target := filepath.Join(dir, "copy.pdf")
	runCaptured(t, newBaaSCmd(), "storage", "download", "12", "docs", "2026/report.pdf", target, "--region", "thr-1")
	if b, err := os.ReadFile(target); err != nil || string(b) != "%PDF-1.7 hello" {
		t.Errorf("downloaded %q (%v)", b, err)
	}
	out = runCaptured(t, newBaaSCmd(), "storage", "download", "12", "docs", "2026/report.pdf", "-", "--region", "thr-1")
	if out != "%PDF-1.7 hello" {
		t.Errorf("to stdout %q", out)
	}
}

func TestBaaSAuthUsers(t *testing.T) {
	var got []string
	productTestServer(t, "table", api.BaaSPrefix, func(w http.ResponseWriter, r *http.Request) {
		body := baasBody(t, r)
		got = append(got, r.Method+" "+r.URL.Path)
		switch r.Method {
		case http.MethodGet:
			if r.URL.Query().Get("q") != "ana" {
				t.Errorf("query %s", r.URL.RawQuery)
			}
			_, _ = io.WriteString(w, `{"data":[{"id":"7f1c0a8e-4a8b-4c55-9d5e-1a2b3c4d5e6f","email":"ana@example.com","created_at":"2026-11-21T09:00:00Z"}],"total":1,"page":1,"per_page":50}`)
		case http.MethodPost:
			if body["email"] != "ana@example.com" || body["password"] != "S3cret-pass" || body["email_confirm"] != true {
				t.Errorf("body %v", body)
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(w, `{"data":{"id":"7f1c0a8e-4a8b-4c55-9d5e-1a2b3c4d5e6f","email":"ana@example.com"}}`)
		case http.MethodDelete:
			w.WriteHeader(http.StatusNoContent)
		}
	})
	out := runCaptured(t, newBaaSCmd(), "auth", "users", "list", "12", "--search", "ana")
	if !strings.Contains(out, "ana@example.com") {
		t.Errorf("list:\n%s", out)
	}
	runCaptured(t, newBaaSCmd(), "auth", "users", "create", "12", "--email", "ana@example.com", "--password", "S3cret-pass", "--email-confirm")
	out = runCaptured(t, newBaaSCmd(), "auth", "users", "delete", "12", "7f1c0a8e-4a8b-4c55-9d5e-1a2b3c4d5e6f", "--yes")
	if !strings.Contains(out, "deleted") {
		t.Errorf("delete:\n%s", out)
	}
	want := "GET /baas/v1/projects/12/auth/users|POST /baas/v1/projects/12/auth/users|DELETE /baas/v1/projects/12/auth/users/7f1c0a8e-4a8b-4c55-9d5e-1a2b3c4d5e6f"
	if strings.Join(got, "|") != want {
		t.Errorf("requests %v", got)
	}
}

func TestBaaSLogs(t *testing.T) {
	productTestServer(t, "table", api.BaaSPrefix, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		from, err := time.Parse(time.RFC3339, q.Get("from"))
		if r.URL.Path != "/baas/v1/projects/12/logs" || q.Get("source") != "auth" || q.Get("status") != "4" || err != nil ||
			time.Since(from) < 59*time.Minute || time.Since(from) > 61*time.Minute {
			t.Errorf("%s ?%s", r.URL.Path, r.URL.RawQuery)
		}
		_, _ = io.WriteString(w, `{"data":[{"id":991,"at":"2026-11-21T09:00:00Z","source":"auth","method":"POST","path":"/auth/v1/token","status":400,
			"duration_ms":12,"ip":"185.126.7.x","request_id":null}],"next":991}`)
	})
	out := runCaptured(t, newBaaSCmd(), "logs", "12", "--source", "auth", "--since", "1h", "--status", "4")
	for _, want := range []string{"/auth/v1/token", "185.126.7.x", "400", "--before 991"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
}

func TestBaaSUsage(t *testing.T) {
	productTestServer(t, "table", api.BaaSPrefix, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/baas/v1/projects/12/usage" {
			t.Errorf("path %s", r.URL.Path)
		}
		_, _ = io.WriteString(w, `{"data":{"plan":"free","period_start":"2026-11-01T00:00:00Z","period_end":"2026-12-01T00:00:00Z",
			"metrics":{"db_bytes":{"used":1048576,"limit":524288000},"mau":{"used":3,"limit":null}},"daily":{}}}`)
	})
	out := runCaptured(t, newBaaSCmd(), "usage", "12")
	for _, want := range []string{"free", "db bytes", "1048576", "524288000", "mau", "unlimited"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
}

func TestBaaSAPIBaseCallsTheServiceDirectly(t *testing.T) {
	var paths []string
	direct := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok" {
			t.Errorf("auth %q", r.Header.Get("Authorization"))
		}
		paths = append(paths, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"data":[]}`)
	}))
	t.Cleanup(direct.Close)
	productTestServer(t, "table", api.BaaSPrefix, func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("the gateway is not called: %s", r.URL.Path)
	})
	runCaptured(t, newBaaSCmd(), "projects", "list", "--api-base", direct.URL+"/v1")
	t.Setenv(EnvBaaSAPIBase, direct.URL+"/v1/")
	runCaptured(t, newBaaSCmd(), "plans")
	if strings.Join(paths, ",") != "/v1/projects,/v1/plans" {
		t.Errorf("paths %v", paths)
	}
	if f := newBaaSCmd().PersistentFlags().Lookup("api-base"); f == nil || !f.Hidden {
		t.Error("--api-base is a hidden flag")
	}
	if err := config.ValidateAPIURL("http://baas.example/v1"); err == nil {
		t.Error("plain http to a remote host must be refused")
	}
}

// ─── link, migrations, schema, types (#1149) ─────────────────────────────────

// inTempDir runs the rest of the test in a new empty directory.
func inTempDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(old) })
	return dir
}

// runCapturedErr is runCaptured that returns the command's error.
func runCapturedErr(t *testing.T, args ...string) (string, error) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	oldOut, oldErr := os.Stdout, os.Stderr
	os.Stdout = w
	devNull, _ := os.Open(os.DevNull)
	os.Stderr = devNull
	var out []byte
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		out, _ = io.ReadAll(r)
	}()
	cmd := newBaaSCmd()
	cmd.SetArgs(args)
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	runErr := cmd.Execute()
	_ = w.Close()
	wg.Wait()
	os.Stdout, os.Stderr = oldOut, oldErr
	_ = devNull.Close()
	return string(out), runErr
}

func TestBaaSLinkLetsTheProjectIDBeLeftOut(t *testing.T) {
	inTempDir(t)
	var got []string
	productTestServer(t, "table", api.BaaSPrefix, func(w http.ResponseWriter, r *http.Request) {
		got = append(got, r.Method+" "+r.URL.Path)
		switch r.URL.Path {
		case "/baas/v1/projects/12":
			_, _ = io.WriteString(w, `{"data":`+baasProjectJSON+`}`)
		case "/baas/v1/projects/12/sql":
			if body := baasBody(t, r); body["query"] != "select 1" {
				t.Errorf("query %v", body)
			}
			_, _ = io.WriteString(w, baasSQLAnswer)
		default:
			_, _ = io.WriteString(w, `{"data":[]}`)
		}
	})
	runCaptured(t, newBaaSCmd(), "link", "12")
	b, err := os.ReadFile(filepath.Join("arianet", ".baas", "project.json"))
	if err != nil {
		t.Fatal(err)
	}
	var l baasLink
	if err := json.Unmarshal(b, &l); err != nil || l.ProjectID != "12" || l.Ref != "abcdefghij0123456789" || l.Name != "shop" ||
		l.Endpoint != "https://abcdefghij0123456789.baas.example" || l.Region != "thr-1" {
		t.Fatalf("link file %s (%v)", b, err)
	}

	got = nil
	runCaptured(t, newBaaSCmd(), "keys", "list")
	runCaptured(t, newBaaSCmd(), "keys", "list", "13")
	runCaptured(t, newBaaSCmd(), "sql", "select 1")
	runCaptured(t, newBaaSCmd(), "storage", "ls", "avatars", "users/")
	runCaptured(t, newBaaSCmd(), "storage", "buckets")
	runCaptured(t, newBaaSCmd(), "projects", "plan", "pro")
	want := []string{"GET /baas/v1/projects/12/keys", "GET /baas/v1/projects/13/keys", "POST /baas/v1/projects/12/sql",
		"GET /baas/v1/projects/12/storage/buckets/avatars/objects", "GET /baas/v1/projects/12/storage/buckets", "PUT /baas/v1/projects/12/plan"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("requests\n%v\nwant\n%v", got, want)
	}

	runCaptured(t, newBaaSCmd(), "unlink")
	if _, err := os.Stat(filepath.Join("arianet", ".baas", "project.json")); !os.IsNotExist(err) {
		t.Errorf("unlink keeps the file: %v", err)
	}
	if err := baasRunErr(t, "keys", "list"); err == nil || !strings.Contains(err.Error(), "baas link") {
		t.Errorf("unlinked: %v", err)
	}
}

func TestBaaSLinkByRefOrTheOnlyProject(t *testing.T) {
	inTempDir(t)
	projects := `{"data":[` + baasProjectJSON + `,{"id":"13","ref":"zzzzzzzzzz0123456789","name":"blog"}]}`
	productTestServer(t, "table", api.BaaSPrefix, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/baas/v1/projects" {
			t.Errorf("path %s", r.URL.Path)
		}
		_, _ = io.WriteString(w, projects)
	})
	runCaptured(t, newBaaSCmd(), "link", "zzzzzzzzzz0123456789")
	if l, err := readBaaSLink(); err != nil || l == nil || l.ProjectID != "13" || l.Name != "blog" {
		t.Fatalf("link %+v (%v)", l, err)
	}
	if _, err := runCapturedErr(t, "link"); err == nil || !strings.Contains(err.Error(), "you have 2 projects") {
		t.Errorf("two projects: %v", err)
	}
	if _, err := runCapturedErr(t, "link", "nosuchref"); err == nil || !strings.Contains(err.Error(), "no project has the ref") {
		t.Errorf("unknown ref: %v", err)
	}
	projects = `{"data":[` + baasProjectJSON + `]}`
	runCaptured(t, newBaaSCmd(), "link")
	if l, _ := readBaaSLink(); l == nil || l.ProjectID != "12" {
		t.Errorf("the only project is linked: %+v", l)
	}
}

func TestBaaSSplitProject(t *testing.T) {
	inTempDir(t)
	cases := []struct {
		args          []string
		minRest, max  int
		id, rest, err string
	}{
		{[]string{"12", "anon"}, 1, 1, "12", "anon", ""},
		{[]string{"1x", "anon"}, 1, 1, "", "", "invalid project ID"},
		{[]string{"anon"}, 1, 1, "", "", "baas link"},
		{[]string{"12", "b", "p"}, 1, 2, "12", "b,p", ""},
		{[]string{"12"}, 0, 1, "12", "", ""},
		{[]string{"12", "a", "b", "c"}, 1, 1, "", "", "expected the project ID"},
	}
	for _, c := range cases {
		id, rest, err := splitProject(c.args, c.minRest, c.max)
		if c.err != "" {
			if err == nil || !strings.Contains(err.Error(), c.err) {
				t.Errorf("%v: err %v, want %q", c.args, err, c.err)
			}
			continue
		}
		if err != nil || id != c.id || strings.Join(rest, ",") != c.rest {
			t.Errorf("%v: %q %v %v", c.args, id, rest, err)
		}
	}
	if err := os.MkdirAll(filepath.Join("arianet", ".baas"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(baasLinkFile, []byte(`{"project_id":"44","name":"x"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if id, rest, err := splitProject([]string{"anon"}, 1, 1); err != nil || id != "44" || rest[0] != "anon" {
		t.Errorf("linked: %q %v %v", id, rest, err)
	}
	if id, _, err := splitProject(nil, 0, 0); err != nil || id != "44" {
		t.Errorf("linked, no args: %q %v", id, err)
	}
}

func TestBaaSMigrationNew(t *testing.T) {
	inTempDir(t)
	productTestServer(t, "table", api.BaaSPrefix, func(w http.ResponseWriter, r *http.Request) { t.Error("nothing is sent") })
	out := runCaptured(t, newBaaSCmd(), "migration", "new", "Create todosTable")
	entries, err := os.ReadDir(filepath.Join("arianet", "migrations"))
	if err != nil || len(entries) != 1 || !regexp.MustCompile(`^\d{14}_create_todos_table\.sql$`).MatchString(entries[0].Name()) {
		t.Fatalf("files %v (%v)", entries, err)
	}
	if !strings.Contains(out, entries[0].Name()) {
		t.Errorf("output %q", out)
	}
	text, _ := os.ReadFile(filepath.Join("arianet", "migrations", entries[0].Name()))
	if !strings.HasPrefix(string(text), "-- Migration create_todos_table, created by arianet baas migration new at ") {
		t.Errorf("header %q", text)
	}
	if err := baasRunErr(t, "migration", "new", "--"); err == nil {
		t.Error("an empty name is refused")
	}

	// A project that still keeps ./migrations (the old default) gets its files there.
	inTempDir(t)
	if err := os.Mkdir("migrations", 0o755); err != nil {
		t.Fatal(err)
	}
	runCaptured(t, newBaaSCmd(), "migration", "new", "add_done")
	if m, _ := filepath.Glob(filepath.Join("migrations", "*_add_done.sql")); len(m) != 1 {
		t.Errorf("legacy dir: %v", m)
	}
	if snakeName("AddDone column!") != "add_done_column" || snakeName("v2 users") != "v2_users" {
		t.Error("snakeName")
	}
}

const (
	migA = "create table a (id int);"
	migB = "create table b (id int);"
	migC = "create table c (id int);"
)

// writeProjectMigrations writes arianet/migrations of the current directory.
func writeProjectMigrations(t *testing.T, files map[string]string) {
	t.Helper()
	dir := filepath.Join("arianet", "migrations")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, text := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func baasHistory(entries ...[2]string) string {
	items := make([]string, len(entries))
	for i, e := range entries {
		items[i] = fmt.Sprintf(`{"name":%q,"checksum":%q,"applied_at":"2026-11-21T09:00:00Z"}`, e[0], e[1])
	}
	return `{"data":[` + strings.Join(items, ",") + `]}`
}

func TestBaaSMigrationList(t *testing.T) {
	inTempDir(t)
	writeProjectMigrations(t, map[string]string{"001_a.sql": migA, "002_b.sql": migB + " -- edited", "003_c.sql": migC})
	productTestServer(t, "json", api.BaaSPrefix, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/baas/v1/projects/12/migrations" {
			t.Errorf("%s %s", r.Method, r.URL.Path)
		}
		_, _ = io.WriteString(w, baasHistory([2]string{"000_old.sql", "ff"}, [2]string{"001_a.sql", checksum([]byte(migA))},
			[2]string{"002_b.sql", checksum([]byte(migB))}))
	})
	out := runCaptured(t, newBaaSCmd(), "migration", "list", "12")
	var list []migrationStatus
	if err := json.Unmarshal([]byte(out), &list); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	var got []string
	for _, m := range list {
		got = append(got, fmt.Sprintf("%s:%t:%s", m.Name, m.Local, m.Status))
	}
	want := "000_old.sql:false:remote only|001_a.sql:true:applied|002_b.sql:true:changed|003_c.sql:true:pending"
	if strings.Join(got, "|") != want {
		t.Errorf("statuses %v", got)
	}

	cfg.Output, outputFormat = "table", "table"
	out = runCaptured(t, newBaaSCmd(), "migration", "list", "12")
	for _, s := range []string{"APPLIED AT", "remote only", "changed", "pending", "2026-11-21T09:00:00Z", "003_c.sql"} {
		if !strings.Contains(out, s) {
			t.Errorf("table lacks %q:\n%s", s, out)
		}
	}
}

func TestBaaSDBPushThroughTheEndpoint(t *testing.T) {
	inTempDir(t)
	writeProjectMigrations(t, map[string]string{"002_b.sql": migB, "001_a.sql": migA, "notes.txt": "x"})
	var bodies []map[string]any
	productTestServer(t, "table", api.BaaSPrefix, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/baas/v1/projects/12/migrations" {
			t.Errorf("%s %s", r.Method, r.URL.Path)
		}
		body := baasBody(t, r)
		bodies = append(bodies, body)
		if body["dry_run"] == true {
			_, _ = io.WriteString(w, `{"data":{"dry_run":true,"applied":[],"skipped":["001_a.sql"],"pending":["002_b.sql"]}}`)
			return
		}
		_, _ = io.WriteString(w, `{"data":{"dry_run":false,"applied":["002_b.sql"],"skipped":["001_a.sql"],"pending":[]}}`)
	})
	out := runCaptured(t, newBaaSCmd(), "db", "push", "12", "--dry-run")
	if !strings.Contains(out, "Would apply 1 migration(s)") || !strings.Contains(out, "002_b.sql") {
		t.Errorf("dry run:\n%s", out)
	}
	out = runCaptured(t, newBaaSCmd(), "db", "push", "12")
	if !strings.Contains(out, "Applied 002_b.sql") || !strings.Contains(out, "Applied 1 migration(s) in one transaction") {
		t.Errorf("push:\n%s", out)
	}
	if len(bodies) != 2 || bodies[0]["dry_run"] != true || bodies[1]["dry_run"] != false {
		t.Fatalf("bodies %v", bodies)
	}
	files, _ := bodies[1]["migrations"].([]any)
	if len(files) != 2 {
		t.Fatalf("files %v", bodies[1])
	}
	first, _ := files[0].(map[string]any)
	second, _ := files[1].(map[string]any)
	if first["name"] != "001_a.sql" || first["sql"] != migA || second["name"] != "002_b.sql" || len(first) != 2 {
		t.Errorf("files in name order with their SQL: %v", files)
	}
}

func TestBaaSDBPushRefusals(t *testing.T) {
	inTempDir(t)
	writeProjectMigrations(t, map[string]string{"001_a.sql": migA})
	productTestServer(t, "table", api.BaaSPrefix, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		_, _ = io.WriteString(w, `{"error":"A recorded migration has changed.","code":"migration_changed","migration":"001_a.sql"}`)
	})
	err := baasRunErr(t, "db", "push", "12")
	if err == nil || !strings.Contains(err.Error(), "001_a.sql") || !strings.Contains(err.Error(), "nothing was applied") {
		t.Errorf("409: %v", err)
	}

	inTempDir(t)
	if err := baasRunErr(t, "db", "push", "12"); err == nil || !strings.Contains(err.Error(), "no migrations directory") {
		t.Errorf("no dir: %v", err)
	}

	sqlErr := &api.APIError{Status: 422, Code: "sql_error", Body: json.RawMessage(`{"error":"x","code":"sql_error","migration":"002_b.sql",
		"sql_error":{"sqlstate":"42601","message":"syntax error","statement":0}}`)}
	if errorField(sqlErr, "migration") != "002_b.sql" || sqlErrorOf(sqlErr) == nil || sqlErrorOf(sqlErr).SQLState != "42601" {
		t.Error("the failing migration and its SQL error are read from the 422")
	}
}

func TestBaaSDBPullThroughTheEndpoint(t *testing.T) {
	inTempDir(t)
	const ddl = "CREATE TABLE public.todos (\n  id bigint NOT NULL\n);\nCREATE VIEW public.open_todos AS SELECT 1;\n"
	productTestServer(t, "table", api.BaaSPrefix, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/baas/v1/projects/12/schema" || r.URL.Query().Get("schemas") != "public,app" {
			t.Errorf("%s ?%s", r.URL.Path, r.URL.RawQuery)
		}
		_, _ = fmt.Fprintf(w, `{"data":{"schemas":["public","app"],"sql":%q,"checksum":"abc","generated_at":"2026-11-21T09:00:00Z"}}`, ddl)
	})
	out := runCaptured(t, newBaaSCmd(), "db", "pull", "12", "--schema", "public,app", "--file", "-")
	if !strings.HasPrefix(out, "-- Schema public, app of Arianet BaaS project 12") || !strings.HasSuffix(out, ddl) {
		t.Errorf("snapshot:\n%s", out)
	}
	runCaptured(t, newBaaSCmd(), "db", "pull", "12", "--schema", "public", "--schema", "app")
	m, _ := filepath.Glob(filepath.Join("arianet", "migrations", "*_remote_schema.sql"))
	if len(m) != 1 {
		t.Fatalf("files %v", m)
	}
	if b, _ := os.ReadFile(m[0]); !strings.HasSuffix(string(b), ddl) {
		t.Errorf("file %s", b)
	}
}

func TestBaaSDBDiff(t *testing.T) {
	inTempDir(t)
	snapshot := "-- Schema public of Arianet BaaS project 12, written by arianet baas db pull at 2026-11-20T00:00:00Z.\n\n" +
		"CREATE TABLE public.a (\n  id integer\n);\nCREATE TABLE public.b (\n  id integer\n);\n"
	writeProjectMigrations(t, map[string]string{"001_a.sql": migA, "20261120000000_remote_schema.sql": snapshot})
	remoteSQL := "-- generated 2026-11-21\nCREATE TABLE public.a (\n  id integer\n);  \nCREATE TABLE public.b (\n  id integer\n);\n\n"
	history := baasHistory([2]string{"001_a.sql", checksum([]byte(migA))}, [2]string{"20261120000000_remote_schema.sql", checksum([]byte(snapshot))})
	productTestServer(t, "table", api.BaaSPrefix, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/baas/v1/projects/12/migrations":
			_, _ = io.WriteString(w, history)
		case "/baas/v1/projects/12/schema":
			_, _ = fmt.Fprintf(w, `{"data":{"schemas":["public"],"sql":%q}}`, remoteSQL)
		default:
			t.Errorf("unexpected %s", r.URL.Path)
		}
	})
	out, err := runCapturedErr(t, "db", "diff", "12")
	if err != nil || !strings.Contains(out, "every file") || !strings.Contains(out, "matches") {
		t.Fatalf("no difference: %v\n%s", err, out)
	}

	remoteSQL = "CREATE TABLE public.a (\n  id integer,\n  done boolean\n);\nCREATE TABLE public.b (\n  id integer\n);\n"
	writeProjectMigrations(t, map[string]string{"002_c.sql": migC})
	out, err = runCapturedErr(t, "db", "diff", "12")
	if exitCode(err) != exitCodeDifferences {
		t.Fatalf("exit %d (%v)", exitCode(err), err)
	}
	for _, want := range []string{"002_c.sql", "pending", "--- arianet/migrations/20261120000000_remote_schema.sql", "+++ project 12 (public)",
		"@@ -1,5 +1,6 @@", "-  id integer\n+  id integer,\n+  done boolean\n"} {
		if !strings.Contains(out, want) {
			t.Errorf("diff lacks %q:\n%s", want, out)
		}
	}

	cfg.Output, outputFormat = "json", "json"
	out, err = runCapturedErr(t, "db", "diff", "12")
	var rep diffReport
	if exitCode(err) != exitCodeDifferences || json.Unmarshal([]byte(out), &rep) != nil || !rep.Differences || rep.Schema.Identical ||
		len(rep.Migrations) != 1 || rep.Migrations[0].Name != "002_c.sql" || rep.Schema.Against == nil {
		t.Errorf("json %v: %s", err, out)
	}
}

func TestBaaSUnifiedDiff(t *testing.T) {
	a := strings.Split("a b c d e f g h i j k l m", " ")
	b := strings.Split("a b X d e f g h i j k l m Y", " ")
	want := "--- old\n+++ new\n@@ -1,6 +1,6 @@\n a\n b\n-c\n+X\n d\n e\n f\n@@ -11,3 +11,4 @@\n k\n l\n m\n+Y\n"
	if got := unifiedDiff("old", "new", a, b); got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
	if unifiedDiff("old", "new", a, a) != "" {
		t.Error("same lines: no diff")
	}
	if got := unifiedDiff("x", "y", nil, []string{"one"}); got != "--- x\n+++ y\n@@ -0,0 +1,1 @@\n+one\n" {
		t.Errorf("from empty: %q", got)
	}
	ops := diffLines(strings.Split("q w e r t y", " "), strings.Split("w q e t y u", " "))
	var ra, rb []string
	for _, op := range ops {
		if op.kind != '+' {
			ra = append(ra, op.line)
		}
		if op.kind != '-' {
			rb = append(rb, op.line)
		}
	}
	if strings.Join(ra, " ") != "q w e r t y" || strings.Join(rb, " ") != "w q e t y u" {
		t.Errorf("the script does not rebuild both sides: %v", ops)
	}
}

func TestBaaSGenTypes(t *testing.T) {
	inTempDir(t)
	const ts = "export type Json = string\nexport type Database = { public: {} }\n"
	var calls int
	productTestServer(t, "table", api.BaaSPrefix, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != "/baas/v1/projects/12/types/typescript" || r.URL.Query().Get("schemas") != "public" {
			t.Errorf("%s ?%s", r.URL.Path, r.URL.RawQuery)
		}
		_, _ = fmt.Fprintf(w, `{"data":{"lang":"typescript","schemas":["public"],"types":%q}}`, ts)
	})
	if out := runCaptured(t, newBaaSCmd(), "gen", "types", "12", "--lang", "typescript"); out != ts {
		t.Errorf("stdout %q", out)
	}
	runCaptured(t, newBaaSCmd(), "gen", "types", "12", "--file", filepath.Join("src", "database.types.ts"))
	if b, err := os.ReadFile(filepath.Join("src", "database.types.ts")); err != nil || string(b) != ts {
		t.Errorf("file %q (%v)", b, err)
	}
	if out := runCaptured(t, newBaaSCmd(), "types", "12"); out != ts {
		t.Errorf("the types alias calls the endpoint too: %q", out)
	}
	if calls != 3 {
		t.Errorf("calls %d", calls)
	}
	if err := baasRunErr(t, "gen", "types", "12", "--lang", "python"); err == nil {
		t.Error("only typescript")
	}
}

func TestBaaSAdvisorsDomainsTransferAPISettings(t *testing.T) {
	var got []string
	productTestServer(t, "table", api.BaaSPrefix, func(w http.ResponseWriter, r *http.Request) {
		body := baasBody(t, r)
		got = append(got, r.Method+" "+r.URL.Path+"?"+r.URL.RawQuery+" "+fmt.Sprint(body))
		switch {
		case strings.Contains(r.URL.Path, "/advisors"):
			_, _ = io.WriteString(w, `{"data":{"run":{"id":1,"trigger":"manual","status":"ok","error_code":null,"finished_at":"2026-11-21T09:00:00Z"},
				"counts":{"security":{"ERROR":1,"WARN":0,"INFO":0},"performance":{"ERROR":0,"WARN":0,"INFO":0}},
				"lints":[{"name":"rls_disabled_in_public","title":"RLS disabled","level":"ERROR","category":"security","detail":"todos has no RLS",
				"object":{"type":"table","schema":"public","name":"todos"},"remediation":"Enable RLS","sql":"ALTER TABLE public.todos ENABLE ROW LEVEL SECURITY;"}]}}`)
		case strings.HasSuffix(r.URL.Path, "/domains") && r.Method == http.MethodGet:
			_, _ = io.WriteString(w, `{"data":{"limit":1,"plan_allows":true,"target":"abc.baas.arianet.app","domains":[{"id":7,"hostname":"api.example.com",
				"state":"pending","records":[{"type":"CNAME","name":"api.example.com","value":"abc.baas.arianet.app","ok":false}]}]}}`)
		case strings.Contains(r.URL.Path, "/domains") && r.Method == http.MethodPost:
			w.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(w, `{"data":{"id":7,"hostname":"api.example.com","state":"pending","records":[{"type":"TXT","name":"_arianet-verify.api.example.com","value":"arianet-verify=t","ok":true}]}}`)
		case r.Method == http.MethodDelete:
			w.WriteHeader(http.StatusNoContent)
		default:
			_, _ = io.WriteString(w, `{"data":{"id":5,"state":"pending","max_rows":500}}`)
		}
	})
	out := runCaptured(t, newBaaSCmd(), "advisors", "12", "--kind", "security", "--run", "--details")
	for _, want := range []string{"rls_disabled_in_public", "table public.todos", "Fix: Enable RLS", "ALTER TABLE public.todos ENABLE ROW LEVEL SECURITY;", "manual"} {
		if !strings.Contains(out, want) {
			t.Errorf("advisors lack %q:\n%s", want, out)
		}
	}
	out = runCaptured(t, newBaaSCmd(), "domains", "list", "12")
	if !strings.Contains(out, "api.example.com") || !strings.Contains(out, "CNAME") || !strings.Contains(out, "missing") {
		t.Errorf("domains:\n%s", out)
	}
	out = runCaptured(t, newBaaSCmd(), "domains", "add", "12", "api.example.com")
	if !strings.Contains(out, "_arianet-verify.api.example.com") {
		t.Errorf("add:\n%s", out)
	}
	runCaptured(t, newBaaSCmd(), "domains", "check", "12", "7")
	runCaptured(t, newBaaSCmd(), "domains", "remove", "12", "7", "--yes")
	runCaptured(t, newBaaSCmd(), "transfer", "12", "--to", "123", "--confirm", "abcdefghij0123456789")
	runCaptured(t, newBaaSCmd(), "transfer", "accept", "5")
	runCaptured(t, newBaaSCmd(), "api-settings", "set", "12", "--max-rows", "500")
	want := []string{
		"POST /baas/v1/projects/12/advisors/run?kind=security map[]",
		"GET /baas/v1/projects/12/domains? map[]",
		"POST /baas/v1/projects/12/domains? map[hostname:api.example.com]",
		"POST /baas/v1/projects/12/domains/7/check? map[]",
		"DELETE /baas/v1/projects/12/domains/7? map[]",
		"POST /baas/v1/projects/12/transfer? map[confirm:abcdefghij0123456789 target_account_id:123]",
		"POST /baas/v1/transfers/5/accept? map[]",
		"PUT /baas/v1/projects/12/settings/api? map[max_rows:500]",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("requests\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}
