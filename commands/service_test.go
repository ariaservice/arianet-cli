package commands

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ariaservice/arianet-cli/internal/config"
)

// vpsTestServer serves the VPS API at /api/v1 and points cfg at it.
func vpsTestServer(t *testing.T, output string, h http.HandlerFunc) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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

// samandari as the API answers it: with the ipv4/ipv6 fields (new) or with
// only ip_addresses, IPv6 first (older API).
func samandari(withFamilies bool) map[string]any {
	s := map[string]any{
		"id": 50021396, "status": "active", "instance_status": "running", "name": "samandari",
		"ip_addresses": []any{
			map[string]any{"ip": "2a01:f900:11:226::1", "type": "primary"},
			map[string]any{"ip": "206.1.100.127", "type": "primary"},
		},
		"plan":       map[string]any{"id": 696, "name": "Basic 1"},
		"datacenter": map[string]any{"id": 17, "name": "Istanbul"},
		"os":         map[string]any{"id": 3, "name": "Ubuntu 22.04 LTS"},
		"created_at": "2026-11-01T10:00:00Z",
	}
	if withFamilies {
		s["ip_addresses"] = []any{
			map[string]any{"ip": "206.1.100.127", "type": "primary"},
			map[string]any{"ip": "2a01:f900:11:226::1", "type": "primary"},
		}
		s["ipv4"] = "206.1.100.127"
		s["ipv6"] = "2a01:f900:11:226::1"
	}
	return s
}

func vpsHandler(t *testing.T, withFamilies bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/servers":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"success": true, "data": []any{samandari(withFamilies)},
				"pagination": map[string]any{"current_page": 1, "last_page": 1, "per_page": 15, "total": 1},
			})
		case "/api/v1/servers/50021396":
			_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "data": samandari(withFamilies)})
		default:
			t.Errorf("unexpected path %q", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}
}

// The IPv4 column holds the IPv4 address and the IPv6 one its own column,
// whatever order the addresses come in.
func assertIPColumns(t *testing.T, out string) {
	t.Helper()
	for _, want := range []string{"IPV4", "IPV6", "206.1.100.127", "2A01:F900:11:226::1", "BASIC 1"} {
		if !strings.Contains(strings.ToUpper(out), want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	if strings.Index(out, "206.1.100.127") > strings.Index(out, "2a01:f900:11:226::1") {
		t.Errorf("IPv4 address is not before the IPv6 one:\n%s", out)
	}
}

func TestVPSListShowsIPv4AndIPv6Columns(t *testing.T) {
	for _, withFamilies := range []bool{true, false} {
		vpsTestServer(t, "table", vpsHandler(t, withFamilies))
		assertIPColumns(t, runCaptured(t, newServerCmd(), "list"))
	}
}

func TestVPSGetShowsIPv4AndIPv6Rows(t *testing.T) {
	for _, withFamilies := range []bool{true, false} {
		vpsTestServer(t, "table", vpsHandler(t, withFamilies))
		out := runCaptured(t, newServerCmd(), "get", "50021396")
		assertIPColumns(t, out)
	}
}
