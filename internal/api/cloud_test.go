package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func stubRateLimitSleep(t *testing.T) *[]time.Duration {
	t.Helper()
	var slept []time.Duration
	old := rateLimitSleep
	rateLimitSleep = func(d time.Duration) { slept = append(slept, d) }
	t.Cleanup(func() { rateLimitSleep = old })
	return &slept
}

func TestCloudUsesPrefixBearerAndUserAgent(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/cloud/v1/servers/12" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer tok" {
			t.Errorf("auth = %q", r.Header.Get("Authorization"))
		}
		if !strings.HasPrefix(r.Header.Get("User-Agent"), "arianet-cli/") {
			t.Errorf("user agent = %q", r.Header.Get("User-Agent"))
		}
		// The cloud API answers without the gateway's envelope.
		writeJSON(w, 200, map[string]any{"data": map[string]any{"id": 12, "name": "web-1"}})
	})
	var out struct {
		Data struct {
			ID   int64  `json:"id"`
			Name string `json:"name"`
		} `json:"data"`
	}
	if err := c.Cloud().Get("/servers/12", nil, &out); err != nil {
		t.Fatal(err)
	}
	if out.Data.ID != 12 || out.Data.Name != "web-1" {
		t.Errorf("out = %+v", out)
	}
	if got := c.Cloud().URL("/servers", url.Values{"page": {"2"}}); !strings.HasSuffix(got, "/cloud/v1/servers?page=2") {
		t.Errorf("URL = %q", got)
	}
}

func TestCloudPostSendsJSONBodyAndKeepsRawResponse(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/cloud/v1/servers/3/actions/poweron" {
			t.Errorf("%s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("content type %q", r.Header.Get("Content-Type"))
		}
		b, _ := io.ReadAll(r.Body)
		if string(b) != `{"x":1}` {
			t.Errorf("body %s", b)
		}
		writeJSON(w, 202, map[string]any{"action_id": 77})
	})
	var raw json.RawMessage
	if err := c.Cloud().Post("/servers/3/actions/poweron", map[string]int{"x": 1}, &raw); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"action_id":77`) {
		t.Errorf("raw = %s", raw)
	}
}

func TestCloudDeleteSendsQueryAndBody(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete || r.URL.Query().Get("reason") != "cost" {
			t.Errorf("%s %s", r.Method, r.URL.String())
		}
		b, _ := io.ReadAll(r.Body)
		if string(b) != `{"keep_backups":true}` {
			t.Errorf("body %s", b)
		}
		writeJSON(w, 202, map[string]any{"action_id": 5})
	})
	if err := c.Cloud().Delete("/servers/1", url.Values{"reason": {"cost"}}, map[string]bool{"keep_backups": true}, nil); err != nil {
		t.Fatal(err)
	}
}

func TestCloudReadsBothErrorShapes(t *testing.T) {
	cases := []struct {
		name       string
		status     int
		body       string
		code, msg  string
		reason     string
		retryAfter int
	}{
		{"cloud service", 403, `{"error":"Verify your identity to use cloud servers.","code":"identity_needed","reason":"identity_hold"}`,
			"identity_needed", "Verify your identity to use cloud servers.", "identity_hold", 0},
		{"gateway envelope", 403, `{"success":false,"error":{"code":"INSUFFICIENT_SCOPE","message":"Token lacks cloud:servers:write"}}`,
			"INSUFFICIENT_SCOPE", "Token lacks cloud:servers:write", "", 0},
		{"no code", 500, `{"error":"Something went wrong."}`, "REQUEST_FAILED", "Something went wrong.", "", 0},
		{"not json", 502, `<html>bad gateway</html>`, "BAD_GATEWAY_RESPONSE", "unexpected response from the server (HTTP 502)", "", 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stubRateLimitSleep(t)
			c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			})
			err := c.Cloud().Post("/servers", map[string]any{}, nil)
			var apiErr *APIError
			if !errors.As(err, &apiErr) {
				t.Fatalf("err = %v", err)
			}
			if apiErr.Code != tc.code || apiErr.Message != tc.msg || apiErr.Reason != tc.reason || apiErr.Status != tc.status {
				t.Errorf("got %+v", apiErr)
			}
		})
	}
}

func TestCloudReadRetriesAfterRetryAfter(t *testing.T) {
	slept := stubRateLimitSleep(t)
	var calls int32
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&calls, 1) == 1 {
			w.Header().Set("Retry-After", "3")
			w.Header().Set("RateLimit-Limit", "60")
			w.Header().Set("RateLimit-Remaining", "0")
			w.Header().Set("RateLimit-Reset", "3")
			writeJSON(w, 429, map[string]any{"success": false, "error": map[string]any{"code": "RATE_LIMIT_EXCEEDED", "message": "slow down"}})
			return
		}
		writeJSON(w, 200, map[string]any{"data": []any{}})
	})
	if err := c.Cloud().Get("/servers", nil, nil); err != nil {
		t.Fatal(err)
	}
	if calls != 2 || len(*slept) != 1 || (*slept)[0] != 3*time.Second {
		t.Errorf("calls=%d slept=%v", calls, *slept)
	}
}

func TestCloudRateLimitTooLongOrOnWriteIsReported(t *testing.T) {
	slept := stubRateLimitSleep(t)
	var calls int32
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		if r.Method == http.MethodGet {
			w.Header().Set("Retry-After", "600")
		} else {
			// No Retry-After: RateLimit-Reset says when the window resets.
			w.Header().Set("RateLimit-Reset", "42")
		}
		writeJSON(w, 429, map[string]any{"error": "Too many requests.", "code": "rate_limit_exceeded"})
	})

	err := c.Cloud().Get("/servers", nil, nil)
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Status != 429 || apiErr.RetryAfter != 600 {
		t.Fatalf("err = %#v", err)
	}
	if calls != 1 || len(*slept) != 0 {
		t.Errorf("a long Retry-After must not be waited out: calls=%d slept=%v", calls, *slept)
	}

	err = c.Cloud().Post("/servers", map[string]any{}, nil)
	if !errors.As(err, &apiErr) || apiErr.RetryAfter != 42 || apiErr.Code != "rate_limit_exceeded" {
		t.Fatalf("err = %#v", err)
	}
	if calls != 2 {
		t.Errorf("a write must not be retried: calls=%d", calls)
	}
}

func TestCloudListFollowsPagesWithSelector(t *testing.T) {
	var pages []string
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("label_selector") != "env=prod,tier!=db" || q.Get("per_page") != "100" || q.Get("datacenter") != "eu" {
			t.Errorf("query = %s", r.URL.RawQuery)
		}
		pages = append(pages, q.Get("page"))
		page := map[string]any{"page": 1, "per_page": 100, "last_page": 2, "total_entries": 3, "previous_page": nil, "next_page": 2}
		data := []any{map[string]any{"id": 1}, map[string]any{"id": 2}}
		if q.Get("page") == "2" {
			page = map[string]any{"page": 2, "per_page": 100, "last_page": 2, "total_entries": 3, "previous_page": 1, "next_page": nil}
			data = []any{map[string]any{"id": 3}}
		}
		writeJSON(w, 200, map[string]any{"data": data, "meta": map[string]any{"pagination": page}})
	})
	items, page, err := c.Cloud().List("/servers", ListOptions{LabelSelector: "env=prod,tier!=db", Extra: url.Values{"datacenter": {"eu"}, "empty": {""}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 3 || strings.Join(pages, ",") != "1,2" {
		t.Errorf("items=%d pages=%v", len(items), pages)
	}
	if page == nil || page.TotalEntries != 3 {
		t.Errorf("page = %+v", page)
	}
}

func TestCloudListOnePage(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("page") != "2" || r.URL.Query().Get("per_page") != "10" {
			t.Errorf("query = %s", r.URL.RawQuery)
		}
		writeJSON(w, 200, map[string]any{"data": []any{map[string]any{"id": 11}}, "meta": map[string]any{"pagination": map[string]any{"page": 2, "per_page": 10, "last_page": 3, "total_entries": 21}}})
	})
	items, page, err := c.Cloud().List("/volumes", ListOptions{Page: 2, PerPage: 10})
	if err != nil || len(items) != 1 || page.Page != 2 || page.LastPage != 3 {
		t.Fatalf("items=%v page=%+v err=%v", items, page, err)
	}
}

func TestCloudRefusesRedirectToAnotherHost(t *testing.T) {
	var leaked int32
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			atomic.AddInt32(&leaked, 1)
		}
		writeJSON(w, 200, map[string]any{"data": []any{}})
	}))
	defer other.Close()
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, strings.Replace(other.URL, "127.0.0.1", "localhost", 1)+"/x", http.StatusFound)
	})
	stubRateLimitSleep(t)
	if err := c.Cloud().Get("/servers", nil, nil); err == nil {
		t.Error("a redirect off host must not succeed")
	}
	if leaked != 0 {
		t.Error("the token followed a redirect to another host")
	}
}

func TestGetAction(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/cloud/v1/actions/9" {
			t.Errorf("path %q", r.URL.Path)
		}
		writeJSON(w, 200, map[string]any{"data": map[string]any{"id": 9, "command": "create_server", "status": "error", "progress": 0,
			"resource": map[string]any{"type": "server", "id": 4}, "error": map[string]any{"code": "no_capacity", "message": "Sold out"}}})
	})
	a, err := c.Cloud().GetAction(9)
	if err != nil {
		t.Fatal(err)
	}
	if a.Status != "error" || a.Error == nil || a.Error.Code != "no_capacity" || a.Resource.ID != 4 {
		t.Errorf("action = %+v", a)
	}
}
