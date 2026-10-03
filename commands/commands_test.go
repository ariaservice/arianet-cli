package commands

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/arianet/arianet-cli/internal/api"
)

func TestGeneratePassword(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 50; i++ {
		pw, err := generatePassword(20)
		if err != nil {
			t.Fatal(err)
		}
		if len(pw) != 20 {
			t.Fatalf("length %d", len(pw))
		}
		var up, lo, di, sy bool
		for _, r := range pw {
			switch {
			case r >= 'A' && r <= 'Z':
				up = true
			case r >= 'a' && r <= 'z':
				lo = true
			case r >= '0' && r <= '9':
				di = true
			case strings.ContainsRune("@#%+=", r):
				sy = true
			default:
				t.Fatalf("unexpected character %q", r)
			}
		}
		if !up || !lo || !di || !sy {
			t.Fatalf("%q lacks a character class", pw)
		}
		seen[pw] = true
	}
	if len(seen) != 50 {
		t.Errorf("passwords repeat: %d unique of 50", len(seen))
	}
}

func TestIdempotencyKeys(t *testing.T) {
	k := newIdempotencyKey()
	if !validIdempotencyKey(k) {
		t.Errorf("generated key %q is rejected by our own validator", k)
	}
	if newIdempotencyKey() == k {
		t.Error("keys must be unique")
	}
	for key, want := range map[string]bool{
		"short":                  false,
		"abcdefgh":               true,
		"has space in it":        false,
		"ok_-:.1234":             true,
		strings.Repeat("a", 129): false,
		strings.Repeat("a", 128): true,
	} {
		if got := validIdempotencyKey(key); got != want {
			t.Errorf("validIdempotencyKey(%q) = %v, want %v", key, got, want)
		}
	}
}

func TestParseIDList(t *testing.T) {
	got, err := parseIDList([]string{"1,2", "3", " 4 , "})
	if err != nil || len(got) != 4 || got[3] != 4 {
		t.Fatalf("got %v err %v", got, err)
	}
	for _, bad := range [][]string{{}, {"x"}, {"0"}, {"-3"}} {
		if _, err := parseIDList(bad); err == nil {
			t.Errorf("parseIDList(%v) should fail", bad)
		}
	}
}

func TestOutcomeUnknown(t *testing.T) {
	cases := map[int]bool{422: false, 402: false, 429: false, 409: true, 500: true, 502: true, 503: true}
	for status, want := range cases {
		if got := outcomeUnknown(&api.APIError{Status: status}); got != want {
			t.Errorf("status %d: %v, want %v", status, got, want)
		}
	}
	if !outcomeUnknown(errPlain("timeout")) {
		t.Error("a transport error leaves the outcome unknown")
	}
}

type errPlain string

func (e errPlain) Error() string { return string(e) }

func TestDescribeError(t *testing.T) {
	msg, hints := describeError(&api.APIError{Code: "RATE_LIMIT_EXCEEDED", Status: 429, RetryAfter: 720})
	if !strings.Contains(msg, "12 minutes") {
		t.Errorf("msg = %q", msg)
	}
	if hints != nil {
		t.Errorf("hints = %v", hints)
	}

	msg, _ = describeError(&api.APIError{Code: "UPSTREAM_ERROR", Status: 502, Message: "internal detail"})
	if strings.Contains(msg, "internal detail") {
		t.Errorf("upstream details must not leak: %q", msg)
	}

	msg, hints = describeError(&api.APIError{Code: "INSUFFICIENT_BALANCE", Message: "Not enough balance"})
	if msg != "Not enough balance" || len(hints) == 0 {
		t.Errorf("msg=%q hints=%v", msg, hints)
	}
}

// statusServer answers GET /servers/1/status with the given sequence of
// (status, provider) pairs, repeating the last one.
func statusServer(t *testing.T, seq [][2]string) (*api.Client, *int32) {
	t.Helper()
	var n int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		i := int(atomic.AddInt32(&n, 1)) - 1
		if i >= len(seq) {
			i = len(seq) - 1
		}
		data := map[string]any{"id": 1, "status": seq[i][0]}
		if seq[i][1] != "" {
			data["instance_status"] = seq[i][1]
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "data": data})
	}))
	t.Cleanup(srv.Close)
	return api.New(srv.URL, "tok"), &n
}

func fastPolling(t *testing.T) {
	t.Helper()
	oldPoll, oldGrace := pollInterval, transitionGrace
	pollInterval, transitionGrace = time.Millisecond, 50*time.Millisecond
	t.Cleanup(func() { pollInterval, transitionGrace = oldPoll, oldGrace })
}

func TestWaitProvisioned(t *testing.T) {
	fastPolling(t)
	c, _ := statusServer(t, [][2]string{{"creating", ""}, {"pending", ""}, {"active", "RUNNING"}})
	if got := waitForServer(c, 1, waitProvisioned(), time.Second); got != waitDone {
		t.Errorf("outcome = %v", got)
	}

	c, _ = statusServer(t, [][2]string{{"creating", ""}, {"failed", ""}})
	if got := waitForServer(c, 1, waitProvisioned(), time.Second); got != waitFailed {
		t.Errorf("outcome = %v, want failed", got)
	}
}

func TestWaitRestartIgnoresStaleActiveUntilTransitionOrGrace(t *testing.T) {
	fastPolling(t)

	// "active" right after the request must not count; the later
	// restarting -> active sequence must.
	c, n := statusServer(t, [][2]string{{"active", "RUNNING"}, {"restarting", "RUNNING"}, {"active", "RUNNING"}})
	if got := waitForServer(c, 1, waitRestarted(), time.Second); got != waitDone {
		t.Fatalf("outcome = %v", got)
	}
	if atomic.LoadInt32(n) < 3 {
		t.Errorf("returned after %d polls, before the restart was observed", *n)
	}

	// A restart so fast it never shows a transition completes after the grace.
	c, _ = statusServer(t, [][2]string{{"active", "RUNNING"}})
	start := time.Now()
	if got := waitForServer(c, 1, waitRestarted(), 2*time.Second); got != waitDone {
		t.Fatalf("outcome = %v", got)
	}
	if time.Since(start) < transitionGrace {
		t.Error("finished before the grace period")
	}
}

func TestWaitPowerOffNeedsStoppedProvider(t *testing.T) {
	fastPolling(t)
	c, _ := statusServer(t, [][2]string{{"active", "RUNNING"}, {"powering_off", "RUNNING"}, {"active", "STOPPED"}})
	if got := waitForServer(c, 1, waitPoweredOff(), time.Second); got != waitDone {
		t.Errorf("outcome = %v", got)
	}
}

func TestWaitDeletedAcceptsNotFound(t *testing.T) {
	fastPolling(t)
	var n int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if atomic.AddInt32(&n, 1) < 3 {
			_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "data": map[string]any{"id": 1, "status": "active"}})
			return
		}
		w.WriteHeader(404)
		_ = json.NewEncoder(w).Encode(map[string]any{"success": false, "error": map[string]any{"code": "NOT_FOUND", "message": "gone"}})
	}))
	defer srv.Close()

	if got := waitForServer(api.New(srv.URL, "tok"), 1, waitDeleted(), time.Second); got != waitDone {
		t.Errorf("outcome = %v", got)
	}
}

func TestWaitTimesOutAndAbortsOnAuthError(t *testing.T) {
	fastPolling(t)
	c, _ := statusServer(t, [][2]string{{"creating", ""}})
	if got := waitForServer(c, 1, waitProvisioned(), 30*time.Millisecond); got != waitTimedOut {
		t.Errorf("outcome = %v, want timeout", got)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
		_ = json.NewEncoder(w).Encode(map[string]any{"success": false, "error": map[string]any{"code": "UNAUTHORIZED", "message": "bad token"}})
	}))
	defer srv.Close()
	if got := waitForServer(api.New(srv.URL, "x"), 1, waitProvisioned(), time.Second); got != waitAborted {
		t.Errorf("outcome = %v, want aborted", got)
	}
}

func TestIsStoppedAcceptsProviderVariants(t *testing.T) {
	for state, want := range map[string]bool{"STOPPED": true, "SHUTOFF": true, "RUNNING": false, "POWERING_OFF": false} {
		s := state
		if got := isStopped(&api.ServiceStatus{Status: "active", InstanceStatus: &s}); got != want {
			t.Errorf("isStopped(%s) = %v, want %v", state, got, want)
		}
	}
}

func TestSupportLabel(t *testing.T) {
	supports := map[string]bool{"firewalls": true, "ssh_keys": false}
	if got := supportLabel(supports, "firewalls"); got != "yes" {
		t.Errorf("firewalls = %q", got)
	}
	if got := supportLabel(supports, "ssh_keys"); got != "no" {
		t.Errorf("ssh_keys = %q", got)
	}
	if got := supportLabel(nil, "firewalls"); got != "-" {
		t.Errorf("unknown = %q", got)
	}
}
