package commands

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ariaservice/arianet-cli/internal/api"
	"github.com/ariaservice/arianet-cli/internal/config"
	"github.com/spf13/cobra"
)

// cloudTestServer serves the cloud API at /cloud/v1 and points cfg at it.
func cloudTestServer(t *testing.T, output string, h http.HandlerFunc) {
	t.Helper()
	cloudTestServerAs(t, output, "tok", h)
}

// cloudTestServerAs is cloudTestServer with token configured ("" for none).
func cloudTestServerAs(t *testing.T, output, token string, h http.HandlerFunc) {
	t.Helper()
	wantAuth := ""
	if token != "" {
		wantAuth = "Bearer " + token
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != wantAuth {
			t.Errorf("auth header = %q, want %q", r.Header.Get("Authorization"), wantAuth)
		}
		if !strings.HasPrefix(r.URL.Path, "/cloud/v1/") {
			t.Errorf("path %q is outside /cloud/v1", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		h(w, r)
	}))
	t.Cleanup(srv.Close)

	// cobra.OnInitialize(initConfig) runs on every Execute, so the test
	// must steer what it loads: never the developer's own config or token.
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", home)
	t.Setenv(config.EnvToken, token)
	t.Setenv(config.EnvAPIURL, srv.URL)
	oldCfg, oldErr := cfg, cfgErr
	oldToken, oldURL, oldOutput := tokenOverride, apiURLOverride, outputFormat
	tokenOverride, apiURLOverride, outputFormat = token, srv.URL, output
	cfg, cfgErr = &config.Config{APIURL: srv.URL, Token: token, Output: output, APIURLSource: "test"}, nil
	t.Cleanup(func() {
		cfg, cfgErr = oldCfg, oldErr
		tokenOverride, apiURLOverride, outputFormat = oldToken, oldURL, oldOutput
	})
}

// runCaptured runs a command and returns what it wrote to stdout.
func runCaptured(t *testing.T, cmd *cobra.Command, args ...string) string {
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

	cmd.SetArgs(args)
	runErr := cmd.Execute()

	w.Close()
	wg.Wait()
	os.Stdout, os.Stderr = oldOut, oldErr
	devNull.Close()
	if runErr != nil {
		t.Fatalf("command failed: %v", runErr)
	}
	return string(out)
}

func serverListHandler(t *testing.T) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/cloud/v1/servers" {
			t.Errorf("path %q", r.URL.Path)
		}
		if got := r.URL.Query().Get("label_selector"); got != "env=prod" {
			t.Errorf("label_selector = %q", got)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []any{map[string]any{"id": 12, "name": "web-1", "status": "running", "datacenter": "eu-central-1",
				"plan_id": 3, "image": "ubuntu-24.04", "ipv4": "203.0.113.4", "ipv6": "", "labels": map[string]any{"env": "prod", "tier": "web"}}},
			"meta": map[string]any{"pagination": map[string]any{"page": 1, "per_page": 100, "last_page": 1, "total_entries": 1}},
		})
	}
}

func TestCloudServersListJSON(t *testing.T) {
	cloudTestServer(t, "json", serverListHandler(t))
	out := runCaptured(t, newCloudCmd(), "servers", "list", "--label-selector", "env=prod")
	var items []map[string]any
	if err := json.Unmarshal([]byte(out), &items); err != nil {
		t.Fatalf("not a JSON array: %v\n%s", err, out)
	}
	if len(items) != 1 || items[0]["name"] != "web-1" {
		t.Errorf("items = %v", items)
	}
}

func TestCloudServersListTable(t *testing.T) {
	cloudTestServer(t, "table", serverListHandler(t))
	out := runCaptured(t, newCloudCmd(), "servers", "list", "--label-selector", "env=prod")
	for _, want := range []string{"| ID", "NAME", "web-1", "203.0.113.4", "env=prod,tier=web"} {
		if !strings.Contains(out, want) {
			t.Errorf("table lacks %q:\n%s", want, out)
		}
	}
	if strings.HasPrefix(strings.TrimSpace(out), "[") {
		t.Error("table mode printed JSON")
	}
}

func TestCloudServerCreateWaitsForItsAction(t *testing.T) {
	fastPolling(t)
	var polls int32
	cloudTestServer(t, "json", func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/cloud/v1/servers":
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["name"] != "web-1" || body["datacenter"] != "eu-central-1" || body["image"] != "ubuntu-24.04" {
				t.Errorf("body = %v", body)
			}
			if body["plan_id"] != float64(3) || body["project_id"] != float64(2) {
				t.Errorf("plan/project = %v / %v", body["plan_id"], body["project_id"])
			}
			labels, _ := body["labels"].(map[string]any)
			if labels["env"] != "prod" || labels["tier"] != "web" {
				t.Errorf("labels = %v", body["labels"])
			}
			if ids, _ := body["ssh_key_ids"].([]any); len(ids) != 2 {
				t.Errorf("ssh_key_ids = %v", body["ssh_key_ids"])
			}
			nets, _ := body["networks"].(map[string]any)
			if nets["enable_ipv6"] != false {
				t.Errorf("networks = %v", body["networks"])
			}
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": map[string]any{"id": 12, "name": "web-1", "status": "initializing"}, "action_id": 70, "root_password": "pw",
				"servers": []any{map[string]any{"data": map[string]any{"id": 12}, "action_id": 70}},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/cloud/v1/actions/70":
			status, progress := "running", 40
			if atomic.AddInt32(&polls, 1) >= 3 {
				status, progress = "success", 100
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{
				"id": 70, "command": "create_server", "status": status, "progress": progress,
				"resource": map[string]any{"type": "server", "id": 12},
			}})
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			w.WriteHeader(404)
		}
	})

	out := runCaptured(t, newCloudCmd(), "servers", "create", "--name", "web-1", "--datacenter", "eu-central-1",
		"--plan", "3", "--image", "ubuntu-24.04", "--labels", "env=prod", "--labels", "tier=web",
		"--ssh-key-id", "5,6", "--no-ipv6", "--project-id", "2", "--wait")

	if atomic.LoadInt32(&polls) < 3 {
		t.Errorf("returned after %d polls, before the action finished", polls)
	}
	var resp struct {
		Data struct {
			ID int64 `json:"id"`
		} `json:"data"`
		ActionID int64             `json:"action_id"`
		Actions  []api.CloudAction `json:"actions"`
	}
	if err := json.Unmarshal([]byte(out), &resp); err != nil {
		t.Fatalf("output is not one JSON document: %v\n%s", err, out)
	}
	if resp.Data.ID != 12 || resp.ActionID != 70 || len(resp.Actions) != 1 || resp.Actions[0].Status != "success" {
		t.Errorf("resp = %+v", resp)
	}
}

func TestCloudServerCreateTableShowsActionToFollow(t *testing.T) {
	cloudTestServer(t, "table", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{"id": 12, "name": "web-1"}, "action_id": 70, "root_password": "s3cret\x1b[2J",
		})
	})
	out := runCaptured(t, newCloudCmd(), "servers", "create", "--name", "web-1", "--datacenter", "eu",
		"--plan", "3", "--image", "ubuntu-24.04")
	for _, want := range []string{"ID: 12 (web-1)", "Root password: s3cret", "arianet cloud actions wait 70"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "\x1b") {
		t.Error("terminal escape from the server reached the terminal")
	}
}

func TestCloudFirewallApplyToLabelSelector(t *testing.T) {
	cloudTestServer(t, "json", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/cloud/v1/firewalls/2/actions/apply_to_resources" {
			t.Errorf("path %q", r.URL.Path)
		}
		var body struct {
			ApplyTo []map[string]any `json:"apply_to"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if len(body.ApplyTo) != 2 || body.ApplyTo[0]["type"] != "server" || body.ApplyTo[1]["type"] != "label_selector" {
			t.Errorf("apply_to = %v", body.ApplyTo)
		}
		if sel, _ := body.ApplyTo[1]["label_selector"].(map[string]any); sel["selector"] != "env=prod,tier!=db" {
			t.Errorf("selector = %v", body.ApplyTo[1])
		}
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(map[string]any{"action_id": 9})
	})
	out := runCaptured(t, newCloudCmd(), "firewalls", "apply-to", "2", "--server", "12", "--label-selector", "env=prod,tier!=db")
	if !strings.Contains(out, `"action_id": 9`) {
		t.Errorf("out = %s", out)
	}
}

func TestCloudServerDeleteSendsOptionsAsQuery(t *testing.T) {
	cloudTestServer(t, "json", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if r.Method != http.MethodDelete || r.URL.Path != "/cloud/v1/servers/12" || q.Get("reason") != "other" || q.Get("note") != "moving" {
			t.Errorf("%s %s", r.Method, r.URL)
		}
		if q.Get("keep_backups") != "true" || q.Get("delete_snapshots") != "true" {
			t.Errorf("query = %s", r.URL.RawQuery)
		}
		if b, _ := io.ReadAll(r.Body); len(b) != 0 {
			t.Errorf("delete must not send a body: %s", b)
		}
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(map[string]any{"action_id": 3})
	})
	runCaptured(t, newCloudCmd(), "servers", "delete", "12", "--yes", "--reason", "other", "--note", "moving", "--keep-backups", "--delete-snapshots")
}

func TestCloudServerDeleteLeavesDefaultsOut(t *testing.T) {
	cloudTestServer(t, "json", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Has("keep_backups") || q.Has("delete_snapshots") || q.Has("reason") {
			t.Errorf("query = %s", r.URL.RawQuery)
		}
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(map[string]any{"action_id": 3})
	})
	runCaptured(t, newCloudCmd(), "servers", "delete", "12", "--yes")
}

var publicPricingBody = map[string]any{"pricing": map[string]any{
	"vat_included": false, "hours_per_month": 730,
	"server_types": []any{map[string]any{
		"id": 3, "name": "CX22", "cores": 2, "memory_mb": 4096, "disk_gb": 40, "included_traffic_gb": nil,
		"prices": []any{
			map[string]any{"location": "ovh", "price_hourly": map[string]any{"net": 120, "currency": "IRR"}, "price_monthly": map[string]any{"net": 87600, "currency": "IRR"}},
			map[string]any{"location": "fsn", "price_hourly": map[string]any{"net": 99, "currency": "IRR"}, "price_monthly": map[string]any{"net": 72270, "currency": "IRR"}},
		},
	}},
	"resources": []any{map[string]any{
		"sku": "volume_gb", "location": "ovh", "unit": "gb",
		"price_hourly": map[string]any{"net": 1, "currency": "IRR"}, "price_monthly": map[string]any{"net": 730, "currency": "IRR"}, "percent": nil,
	}},
}}

func TestCloudPricingIsPublicAndNeedsNoToken(t *testing.T) {
	cloudTestServerAs(t, "table", "", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/cloud/v1/public/pricing" {
			t.Errorf("path %q", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(publicPricingBody)
	})
	out := runCaptured(t, newCloudCmd(), "pricing", "--datacenter", "ovh")
	for _, want := range []string{"Server types", "CX22", "87600", "Other resources", "volume_gb", "730", "exclude VAT", "730 hours"} {
		if !strings.Contains(out, want) {
			t.Errorf("pricing lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "72270") {
		t.Errorf("--datacenter ovh still shows fsn:\n%s", out)
	}
}

func TestCloudPricingJSONPassesThrough(t *testing.T) {
	cloudTestServerAs(t, "json", "", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(publicPricingBody)
	})
	out := runCaptured(t, newCloudCmd(), "pricing")
	var got struct {
		Pricing struct {
			HoursPerMonth int   `json:"hours_per_month"`
			ServerTypes   []any `json:"server_types"`
		} `json:"pricing"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil || got.Pricing.HoursPerMonth != 730 || len(got.Pricing.ServerTypes) != 1 {
		t.Errorf("got %+v err %v\n%s", got, err, out)
	}
}

func TestCloudPricingAccountUsesTheAccountRows(t *testing.T) {
	cloudTestServer(t, "table", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/cloud/v1/pricing" {
			t.Errorf("path %q", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": []any{
			map[string]any{"datacenter": "eu", "sku": "volume_gb", "monthly_price": 0.05, "currency": "EUR", "new_field": "x"},
		}})
	})
	out := runCaptured(t, newCloudCmd(), "pricing", "--account")
	for _, want := range []string{"volume_gb", "0.05", "NEW FIELD", "x"} {
		if !strings.Contains(out, want) {
			t.Errorf("pricing lacks %q:\n%s", want, out)
		}
	}
}

var serverCostsBody = map[string]any{"data": map[string]any{
	"server_id": 12, "hours_per_month": 730,
	"lines": []any{
		map[string]any{"type": "server", "id": 12, "name": "web-1", "sku": "plan_3", "quantity": 1, "unit": "hour", "hourly_price": 120, "monthly_price": 87600, "currency": "IRR"},
		map[string]any{"type": "volume", "id": 7, "name": "data", "sku": "volume_gb", "quantity": 50, "unit": "gb", "hourly_price": 50, "monthly_price": 36500, "currency": "IRR"},
	},
	"totals":       []any{map[string]any{"currency": "IRR", "hourly_price": 170, "monthly_price": 124100}},
	"snapshots":    map[string]any{"count": 2, "size_gb": 12.5, "hourly_price": 3, "monthly_price": 2190, "currency": "IRR"},
	"kept_backups": map[string]any{"count": 0, "size_gb": 0, "hourly_price": 0, "monthly_price": 0, "currency": "IRR"},
}}

func TestCloudServerCostsTable(t *testing.T) {
	cloudTestServer(t, "table", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/cloud/v1/servers/12/costs" {
			t.Errorf("path %q", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(serverCostsBody)
	})
	out := runCaptured(t, newCloudCmd(), "servers", "costs", "12")
	for _, want := range []string{"Billed now", "web-1", "volume_gb", "36500", "Total", "124100", "After a delete", "Snapshots", "12.5", "730 hours"} {
		if !strings.Contains(out, want) {
			t.Errorf("costs lack %q:\n%s", want, out)
		}
	}
}

func TestCloudServerCostsJSON(t *testing.T) {
	cloudTestServer(t, "json", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(serverCostsBody)
	})
	out := runCaptured(t, newCloudCmd(), "servers", "costs", "12")
	var got struct {
		ServerID int64 `json:"server_id"`
		Lines    []any `json:"lines"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil || got.ServerID != 12 || len(got.Lines) != 2 {
		t.Errorf("got %+v err %v\n%s", got, err, out)
	}
}

func TestWaitForActionsReportsFailure(t *testing.T) {
	fastPolling(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		status := "success"
		if strings.HasSuffix(r.URL.Path, "/2") {
			status = "error"
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"id": 1, "status": status, "error": map[string]any{"message": "boom"}}})
	}))
	defer srv.Close()

	devNull, _ := os.Open(os.DevNull)
	oldErr := os.Stderr
	os.Stderr = devNull
	defer func() { os.Stderr = oldErr; devNull.Close() }()

	cc := api.New(srv.URL, "tok").Cloud()
	if _, got := waitForActions(cc, []int64{1}, time.Second); got != waitDone {
		t.Errorf("outcome = %v, want done", got)
	}
	if _, got := waitForActions(cc, []int64{1, 2}, time.Second); got != waitFailed {
		t.Errorf("outcome = %v, want failed", got)
	}
}

func TestParseLabelsAndRules(t *testing.T) {
	l, err := parseLabels([]string{"env=prod,tier=web", "empty="})
	if err != nil || l["env"] != "prod" || l["tier"] != "web" || l["empty"] != "" {
		t.Fatalf("labels %v err %v", l, err)
	}
	if _, err := parseLabels([]string{"novalue"}); err == nil {
		t.Error("a label without = must be refused")
	}

	r, err := parseRule("direction=in,protocol=tcp,port=22,source=0.0.0.0/0,source=::/0,description=ssh")
	if err != nil {
		t.Fatal(err)
	}
	if r["direction"] != "in" || r["port"] != "22" || len(r["source_ips"].([]string)) != 2 {
		t.Errorf("rule = %v", r)
	}
	if _, err := parseRule("port=22"); err == nil {
		t.Error("a rule without direction/protocol must be refused")
	}

	if tg, err := parseTargetShorthand("label_selector:role=web"); err != nil || tg["label_selector"] != "role=web" {
		t.Errorf("target %v err %v", tg, err)
	}
	if svc, err := parseServiceShorthand("tcp:80:8080"); err != nil || svc["destination_port"] != 8080 {
		t.Errorf("service %v err %v", svc, err)
	}
}

func TestDescribeErrorAcceptsCloudCodes(t *testing.T) {
	msg, hints := describeError(&api.APIError{Code: "forbidden", Status: 403, Message: "Forbidden"})
	if msg != "Forbidden" || len(hints) == 0 || !strings.Contains(hints[0], "cloud:servers:write") {
		t.Errorf("msg=%q hints=%v", msg, hints)
	}
	msg, _ = describeError(&api.APIError{Code: "rate_limit_exceeded", Status: 429, RetryAfter: 30})
	if !strings.Contains(msg, "30 seconds") {
		t.Errorf("msg = %q", msg)
	}
}
