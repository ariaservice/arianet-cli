package commands

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/ariaservice/arianet-cli/internal/api"
	"github.com/ariaservice/arianet-cli/internal/config"
	"github.com/spf13/cobra"
)

// productTestServer serves one product API at prefix and points cfg at it.
func productTestServer(t *testing.T, output, prefix string, h http.HandlerFunc) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok" {
			t.Errorf("auth header = %q", r.Header.Get("Authorization"))
		}
		if !strings.HasPrefix(r.URL.Path, prefix+"/") {
			t.Errorf("path %q is outside %s", r.URL.Path, prefix)
		}
		w.Header().Set("Content-Type", "application/json")
		h(w, r)
	}))
	t.Cleanup(srv.Close)

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", home)
	t.Setenv(config.EnvToken, "tok")
	t.Setenv(config.EnvAPIURL, srv.URL)
	oldCfg, oldErr := cfg, cfgErr
	oldToken, oldURL, oldOutput := tokenOverride, apiURLOverride, outputFormat
	tokenOverride, apiURLOverride, outputFormat = "tok", srv.URL, output
	cfg, cfgErr = &config.Config{APIURL: srv.URL, Token: "tok", Output: output, APIURLSource: "test"}, nil
	t.Cleanup(func() {
		cfg, cfgErr = oldCfg, oldErr
		tokenOverride, apiURLOverride, outputFormat = oldToken, oldURL, oldOutput
	})
}

func TestHostingAccountsListSendsTheRegion(t *testing.T) {
	productTestServer(t, "table", api.HostingPrefix, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/hosting/v1/accounts" {
			t.Errorf("path %q", r.URL.Path)
		}
		if got := r.Header.Get(api.RegionHeader); got != "iran-1" {
			t.Errorf("region header %q", got)
		}
		_, _ = io.WriteString(w, `{"data":[{"id":7,"domain":"example.com","state":"active","kind":"shared","plan_id":3,"datacenter":"thr","region":"iran-1"}]}`)
	})
	out := runCaptured(t, newHostingCmd(), "accounts", "list", "--region", "iran-1")
	for _, want := range []string{"example.com", "active", "iran-1"} {
		if !strings.Contains(out, want) {
			t.Errorf("table lacks %q:\n%s", want, out)
		}
	}
}

func TestHostingDefaultRegionSendsNoHeader(t *testing.T) {
	productTestServer(t, "json", api.HostingPrefix, func(w http.ResponseWriter, r *http.Request) {
		if _, has := r.Header[api.RegionHeader]; has {
			t.Error("no --region, no header")
		}
		if r.URL.Query().Get("kind") != "reseller" {
			t.Errorf("query %q", r.URL.RawQuery)
		}
		_, _ = io.WriteString(w, `{"data":[]}`)
	})
	out := runCaptured(t, newHostingCmd(), "plans", "--kind", "reseller")
	if !strings.Contains(out, `"data"`) {
		t.Errorf("JSON mode prints the answer as is:\n%s", out)
	}
}

func TestHostingDNSAddBody(t *testing.T) {
	productTestServer(t, "json", api.HostingPrefix, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/hosting/v1/accounts/12/dns-records" {
			t.Errorf("%s %s", r.Method, r.URL.Path)
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["type"] != "A" || body["name"] != "www" || body["content"] != "203.0.113.10" || body["ttl"] != float64(300) {
			t.Errorf("body %v", body)
		}
		if _, has := body["priority"]; has {
			t.Error("an unset flag is not sent")
		}
		_, _ = io.WriteString(w, `{"data":[]}`)
	})
	runCaptured(t, newHostingCmd(), "dns", "add", "12", "--type", "A", "--name", "www", "--content", "203.0.113.10", "--ttl", "300")
}

func TestSMSSendBody(t *testing.T) {
	productTestServer(t, "json", api.SMSPrefix, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/sms/v1/messages" {
			t.Errorf("%s %s", r.Method, r.URL.Path)
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		rec, _ := body["recipients"].([]any)
		if body["from"] != float64(3) || len(rec) != 3 || rec[2] != "09130000000" || body["body"] != "Hi, there" ||
			body["client_reference"] != "order-981" || len(body) != 4 {
			t.Errorf("body %v", body)
		}
		w.WriteHeader(http.StatusAccepted)
		_, _ = io.WriteString(w, `{"batch_id":"01HX","status":"processing","total_recipients":3,"total_cost_rial":4500,"filtered_blocked_count":0}`)
	})
	out := runCaptured(t, newSMSCmd(), "send", "--from", "3", "--to", "09121234567,09351234567", "--to", "09130000000",
		"--body", "Hi, there", "--reference", "order-981")
	if !strings.Contains(out, `"batch_id": "01HX"`) {
		t.Errorf("output:\n%s", out)
	}
}

func TestSMSScheduledTemplateSendShowsItsAnswer(t *testing.T) {
	productTestServer(t, "table", api.SMSPrefix, func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["template_id"] != float64(7) || body["scheduled_at"] != "2026-12-01T09:00:00+03:30" || body["body"] != nil {
			t.Errorf("body %v", body)
		}
		w.WriteHeader(http.StatusAccepted)
		_, _ = io.WriteString(w, `{"scheduled_id":12,"status":"pending","scheduled_at":"2026-12-01T05:30:00Z"}`)
	})
	out := runCaptured(t, newSMSCmd(), "send", "--from", "3", "--to", "09121234567", "--template", "7", "--at", "2026-12-01T09:00:00+03:30")
	for _, want := range []string{"Message accepted.", "scheduled id", "12", "pending"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
}

func TestSMSListsUsePagesAndFilters(t *testing.T) {
	productTestServer(t, "table", api.SMSPrefix, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if r.URL.Path != "/sms/v1/messages" || q.Get("page") != "2" || q.Get("per_page") != "1" || q.Get("status") != "failed" || q.Get("batch_id") != "01HX" {
			t.Errorf("%s ?%s", r.URL.Path, r.URL.RawQuery)
		}
		_, _ = io.WriteString(w, `{"data":[{"id":"m1","to":"09121234567","from":"3000","status":"failed","parts":1,"cost_rial":1500}],
			"meta":{"total":3,"current_page":2,"per_page":1}}`)
	})
	out := runCaptured(t, newSMSCmd(), "messages", "list", "--status", "failed", "--batch", "01HX", "--page", "2", "--per-page", "1")
	for _, want := range []string{"09121234567", "failed", "1500"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
}

func TestSMSPricesAndReportAreTables(t *testing.T) {
	productTestServer(t, "table", api.SMSPrefix, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/sms/v1/prices":
			_, _ = io.WriteString(w, `{"billing_unit":"message","unit_rial":1750,"tiers":[{"min":1000,"min_parts":1,"unit_rial":1600}]}`)
		case "/sms/v1/reports/summary":
			_, _ = io.WriteString(w, `{"from":"2026-11-01","to":"2026-11-02","sent":5,"delivered":4,"failed":1,"pending":0,"parts":6,"spend_rial":8750,
				"daily":[{"date":"2026-11-02","sent":5,"delivered":4,"failed":1,"spend_rial":8750}]}`)
		default:
			t.Errorf("unexpected %s", r.URL.Path)
		}
	})
	out := runCaptured(t, newSMSCmd(), "prices")
	for _, want := range []string{"Billed per", "message", "Volume tiers", "1600"} {
		if !strings.Contains(out, want) {
			t.Errorf("prices output lacks %q:\n%s", want, out)
		}
	}
	out = runCaptured(t, newSMSCmd(), "report")
	for _, want := range []string{"Spend (rial)", "8750", "By day", "2026-11-02"} {
		if !strings.Contains(out, want) {
			t.Errorf("report output lacks %q:\n%s", want, out)
		}
	}
}

func TestSMSTemplateUpdateIsAPut(t *testing.T) {
	var puts int32
	productTestServer(t, "table", api.SMSPrefix, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut && r.URL.Path == "/sms/v1/templates/9" {
			atomic.AddInt32(&puts, 1)
		}
		_, _ = io.WriteString(w, `{"data":{"id":9,"name":"otp","body":"Code {code}"}}`)
	})
	runCaptured(t, newSMSCmd(), "templates", "update", "9", "--name", "otp", "--body", "Code {code}")
	if puts != 1 {
		t.Errorf("puts %d", puts)
	}
}

func TestProductRequiredFlag(t *testing.T) {
	productTestServer(t, "json", api.SMSPrefix, func(w http.ResponseWriter, r *http.Request) {
		t.Error("nothing may be sent without the required flags")
	})
	cmd := newSMSCmd()
	cmd.SetArgs([]string{"templates", "create", "--name", "otp"})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "--body is required") {
		t.Fatalf("err = %v", err)
	}
}

func TestProductDataFlagIsMergedWithFlags(t *testing.T) {
	productTestServer(t, "json", api.BaremetalPrefix, func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["catalog"] != "eco" || body["memory"] != "ram-64g" || body["datacenter"] != "fra" {
			t.Errorf("body %v", body)
		}
		_, _ = io.WriteString(w, `{"data":{"total":"120.00"}}`)
	})
	runCaptured(t, newBaremetalCmd(), "quote", "--data",
		`{"catalog":"eco","plan_code":"x","datacenter":"gra","memory":"ram-32g","storage":"s","bandwidth":"b"}`,
		"--memory", "ram-64g", "--datacenter", "fra")
}

func TestVPSRegionalCreateWaitsForItsAction(t *testing.T) {
	fastPolling(t)
	var polls int32
	productTestServer(t, "json", api.VPSPrefix, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/vps/v1/servers":
			w.WriteHeader(http.StatusAccepted)
			_, _ = io.WriteString(w, `{"data":{"id":5,"hostname":"web-1"},"action":{"id":40,"state":"running"},"password":"s3cret"}`)
		case r.URL.Path == "/vps/v1/actions/40":
			state := "running"
			if atomic.AddInt32(&polls, 1) >= 2 {
				state = "success"
			}
			_, _ = io.WriteString(w, `{"data":{"id":40,"state":"`+state+`"}}`)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
	})
	out := runCaptured(t, newServerCmd(), "regional", "servers", "create", "--datacenter", "thr-1", "--plan-id", "4",
		"--image", "ubuntu-24.04", "--hostname", "web-1", "--billing-mode", "hourly", "--wait")
	var resp map[string]any
	if err := json.Unmarshal([]byte(out), &resp); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out)
	}
	actions, _ := resp["actions"].([]any)
	if len(actions) != 1 || actions[0].(map[string]any)["state"] != "success" {
		t.Errorf("actions %v", resp["actions"])
	}
}

func TestVPSRegionalTableShowsOneTimePassword(t *testing.T) {
	productTestServer(t, "table", api.VPSPrefix, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"action":{"id":41,"state":"running"},"password":"n3w-pass"}`)
	})
	out := runCaptured(t, newServerCmd(), "regional", "servers", "reset-password", "5")
	for _, want := range []string{"Password: n3w-pass", "shown only this once", "arianet vps regional actions wait 41"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
}

func TestProductDeleteNeedsConfirmation(t *testing.T) {
	var deletes int32
	productTestServer(t, "table", api.SMSPrefix, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete && r.URL.Path == "/sms/v1/templates/9" {
			atomic.AddInt32(&deletes, 1)
		}
		w.WriteHeader(http.StatusNoContent)
	})
	out := runCaptured(t, newSMSCmd(), "templates", "delete", "9", "--yes")
	if deletes != 1 || !strings.Contains(out, "Template #9 deleted.") {
		t.Errorf("deletes %d, output:\n%s", deletes, out)
	}
}

func TestProductArgumentsCannotLeaveTheirSegment(t *testing.T) {
	productTestServer(t, "json", api.HostingPrefix, func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("reached %s", r.URL.Path)
	})
	cmd := newHostingCmd()
	cmd.SetArgs([]string{"accounts", "get", "../../internal"})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	if err := cmd.Execute(); err == nil {
		t.Fatal("a path in an argument is refused")
	}
}

func TestProductAPICommand(t *testing.T) {
	productTestServer(t, "table", api.BaremetalPrefix, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/baremetal/v1/plans" || r.URL.Query().Get("range") != "rise" {
			t.Errorf("%s ?%s", r.URL.Path, r.URL.RawQuery)
		}
		_, _ = io.WriteString(w, `{"data":[{"plan_code":"24rise01"}]}`)
	})
	out := runCaptured(t, newBaremetalCmd(), "api", "get", "/plans?range=rise")
	if !strings.Contains(out, `"plan_code": "24rise01"`) {
		t.Errorf("output:\n%s", out)
	}
}

func TestRootHasTheProductGroups(t *testing.T) {
	names := map[string]bool{}
	for _, c := range rootCmd.Commands() {
		names[c.Name()] = true
	}
	for _, want := range []string{"hosting", "baremetal", "sms", "vps", "cloud"} {
		if !names[want] {
			t.Errorf("root lacks %q", want)
		}
	}
	var regional *cobra.Command
	for _, c := range newServerCmd().Commands() {
		if c.Name() == "regional" {
			regional = c
		}
	}
	if regional == nil {
		t.Fatal("vps lacks regional")
	}
}
