package api

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func init() { retryStep = time.Millisecond }

func newTestClient(t *testing.T, h http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return New(srv.URL+"/", "tok")
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func TestNewTrimsTrailingSlashAndSendsBearer(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/auth/me" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer tok" {
			t.Errorf("auth header = %q", r.Header.Get("Authorization"))
		}
		writeJSON(w, 200, map[string]any{"success": true, "data": map[string]any{"id": 7}})
	})
	u, err := c.GetMe()
	if err != nil || u.ID != 7 {
		t.Fatalf("GetMe = %+v, %v", u, err)
	}
}

func TestCreateServiceSendsKeyAndRetriesGateway(t *testing.T) {
	var calls int32
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Idempotency-Key") != "cli-key-12345" {
			t.Errorf("missing key: %q", r.Header.Get("Idempotency-Key"))
		}
		if atomic.AddInt32(&calls, 1) < 3 {
			writeJSON(w, 502, map[string]any{"success": false, "error": map[string]any{"code": "UPSTREAM_ERROR", "message": "x"}})
			return
		}
		w.Header().Set("Idempotent-Replayed", "true")
		writeJSON(w, 202, map[string]any{"success": true, "data": map[string]any{"id": 9, "status": "creating", "root_password": "pw"}})
	})

	res, err := c.CreateService(CreateServiceRequest{PlanID: 1, DatacenterID: 2, OsID: 3}, "cli-key-12345")
	if err != nil {
		t.Fatal(err)
	}
	if calls != 3 {
		t.Errorf("calls = %d, want 3", calls)
	}
	if !res.Replayed || res.Server.ID != 9 || res.Server.RootPassword != "pw" {
		t.Errorf("result = %+v %+v", res, res.Server)
	}
}

func TestCreateServiceRetriesConflictButNotValidation(t *testing.T) {
	var calls int32
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&calls, 1) == 1 {
			writeJSON(w, 409, map[string]any{"success": false, "error": map[string]any{"code": "CONFLICT", "message": "in progress"}})
			return
		}
		writeJSON(w, 422, map[string]any{"success": false, "error": map[string]any{"code": "VALIDATION_ERROR", "message": "bad"}})
	})

	_, err := c.CreateService(CreateServiceRequest{}, "cli-key-12345")
	apiErr, ok := err.(*APIError)
	if !ok || apiErr.Status != 422 {
		t.Fatalf("err = %v", err)
	}
	if calls != 2 {
		t.Errorf("calls = %d, want 2 (409 retried, 422 not)", calls)
	}
}

func TestActionsAreNotRetried(t *testing.T) {
	var calls int32
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		writeJSON(w, 502, map[string]any{"success": false, "error": map[string]any{"code": "UPSTREAM_ERROR", "message": "x"}})
	})
	if _, err := c.RestartService(1); err == nil {
		t.Fatal("expected error")
	}
	if calls != 1 {
		t.Errorf("a restart must not be repeated automatically; calls = %d", calls)
	}
}

func TestReadsAreRetried(t *testing.T) {
	var calls int32
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&calls, 1) == 1 {
			writeJSON(w, 503, map[string]any{"success": false, "error": map[string]any{"code": "UPSTREAM_UNAVAILABLE", "message": "x"}})
			return
		}
		writeJSON(w, 200, map[string]any{"success": true, "data": map[string]any{"id": 1, "status": "active"}})
	})
	st, err := c.GetServiceStatus(1)
	if err != nil || st.Status != "active" || calls != 2 {
		t.Fatalf("st=%+v err=%v calls=%d", st, err, calls)
	}
}

func TestRateLimitKeepsRetryAfterAndIsNotRetried(t *testing.T) {
	var calls int32
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.Header().Set("Retry-After", "720")
		writeJSON(w, 429, map[string]any{"success": false, "error": map[string]any{"code": "RATE_LIMIT_EXCEEDED", "message": "slow down"}})
	})
	_, err := c.CreateService(CreateServiceRequest{}, "cli-key-12345")
	apiErr, ok := err.(*APIError)
	if !ok || apiErr.RetryAfter != 720 || apiErr.Code != "RATE_LIMIT_EXCEEDED" {
		t.Fatalf("err = %#v", err)
	}
	if calls != 1 {
		t.Errorf("calls = %d, want 1", calls)
	}
}

func TestNonJSONAnswerBecomesBadGateway(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(504)
		_, _ = io.WriteString(w, "<html>gateway timeout</html>")
	})
	_, err := c.GetMe()
	apiErr, ok := err.(*APIError)
	if !ok || apiErr.Code != "BAD_GATEWAY_RESPONSE" || apiErr.Status != 504 {
		t.Fatalf("err = %#v", err)
	}
}

func TestListServicesPassesStatusAndPagination(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("status") != "terminated" || q.Get("page") != "2" || q.Get("limit") != "5" {
			t.Errorf("query = %v", q)
		}
		writeJSON(w, 200, map[string]any{
			"success":    true,
			"data":       []map[string]any{{"id": 1, "status": "terminated"}},
			"pagination": map[string]any{"current_page": 2, "per_page": 5, "total": 11, "last_page": 3},
		})
	})
	list, pg, err := c.ListServices(2, 5, "terminated")
	if err != nil || len(list) != 1 || pg == nil || pg.LastPage != 3 {
		t.Fatalf("list=%v pg=%+v err=%v", list, pg, err)
	}
}

func TestInvoiceNumberIsEscapedAndRuleIDIsAString(t *testing.T) {
	var paths []string
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.EscapedPath())
		writeJSON(w, 200, map[string]any{"success": true, "data": map[string]any{"number": "INV-1"}})
	})
	if _, err := c.GetInvoice("INV/1"); err != nil {
		t.Fatal(err)
	}
	if err := c.DeleteFirewallRule(3, "a b"); err != nil {
		t.Fatal(err)
	}
	want := []string{"/api/v1/invoices/INV%2F1", "/api/v1/firewalls/3/rules/a%20b"}
	for i := range want {
		if paths[i] != want[i] {
			t.Errorf("path[%d] = %q, want %q", i, paths[i], want[i])
		}
	}
}

func TestFlattenOSUsesVersionsAndSkipsDisabled(t *testing.T) {
	groups := []OSGroup{{
		Name: "Linux",
		Templates: OSTemplates{Data: []OSTemplate{
			{ID: 1, Name: "Ubuntu", Status: true, Children: OSChildren{Data: []OSChild{
				{ID: 11, Name: "22.04", Status: true},
				{ID: 12, Name: "20.04", Status: false},
				{ID: 13, Name: "Ubuntu 24.04 LTS", Status: true},
			}}},
			{ID: 2, Name: "Alpine", Status: true},
			{ID: 3, Name: "Old", Status: false},
		}},
	}}
	got := FlattenOS(groups)
	if len(got) != 3 || got[0].ID != 11 || got[0].Name != "Ubuntu 22.04" || got[1].Name != "Ubuntu 24.04 LTS" || got[2].ID != 2 {
		t.Fatalf("got %+v", got)
	}
}

func TestFlattenDatacentersFallsBackToRegionCountry(t *testing.T) {
	cc := "DE"
	got := FlattenDatacenters([]Region{{Name: "Europe", CountryCode: &cc, Datacenters: []Datacenter{{ID: 9, Name: "fsn1", Status: "active"}}}})
	if len(got) != 1 || got[0].ID != 9 || got[0].Country != "DE" || got[0].Region != "Europe" {
		t.Fatalf("got %+v", got)
	}
}

func TestPrimaryIP(t *testing.T) {
	s := Service{IPAddresses: []IPAddressSummary{{IP: "2.2.2.2", Type: "floating"}, {IP: "1.1.1.1", Type: "primary"}}}
	if s.PrimaryIP() != "1.1.1.1" {
		t.Errorf("PrimaryIP = %q", s.PrimaryIP())
	}
	s.IPAddresses = s.IPAddresses[:1]
	if s.PrimaryIP() != "2.2.2.2" {
		t.Errorf("fallback = %q", s.PrimaryIP())
	}
}

func TestRedirectToOtherHostIsNotFollowed(t *testing.T) {
	var leaked int32
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&leaked, 1)
		writeJSON(w, 200, map[string]any{"success": true})
	}))
	t.Cleanup(other.Close)

	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL+"/steal", http.StatusTemporaryRedirect)
	})
	if _, err := c.GetMe(); err == nil {
		t.Fatal("expected an error for a cross-host redirect")
	}
	if atomic.LoadInt32(&leaked) != 0 {
		t.Fatal("request (with bearer token) was forwarded to another host")
	}
}

func TestOversizedResponseIsBounded(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		chunk := make([]byte, 1<<20)
		for i := 0; i < 40; i++ {
			if _, err := w.Write(chunk); err != nil {
				return
			}
		}
	})
	if _, err := c.GetMe(); err == nil {
		t.Fatal("expected an error for an oversized/invalid body")
	}
}
