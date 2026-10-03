package config

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestValidateAPIURL(t *testing.T) {
	ok := []string{
		"https://api.ariaservice.net",
		"http://localhost:8080",
		"http://127.0.0.1:9000",
		"http://[::1]:9000",
	}
	bad := []string{
		"",
		"api.ariaservice.net",
		"http://api.ariaservice.net",
		"http://evil.example/localhost",
		"http://api.localhost",
		"ftp://api.ariaservice.net",
		"https://user:pass@api.ariaservice.net",
		"https://",
	}
	for _, u := range ok {
		if err := ValidateAPIURL(u); err != nil {
			t.Errorf("ValidateAPIURL(%q) rejected: %v", u, err)
		}
	}
	for _, u := range bad {
		if err := ValidateAPIURL(u); err == nil {
			t.Errorf("ValidateAPIURL(%q) accepted", u)
		}
	}
}

func TestSaveWritesPrivateFileAndRoundTrips(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))

	in := &Config{Token: "secret-token-value", APIURL: "https://api.ariaservice.net", Output: "json"}
	if err := Save(in); err != nil {
		t.Fatal(err)
	}
	path, err := FilePath()
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		fi, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if fi.Mode().Perm() != 0o600 {
			t.Errorf("config mode = %o, want 600", fi.Mode().Perm())
		}
		di, _ := os.Stat(filepath.Dir(path))
		if di.Mode().Perm() != 0o700 {
			t.Errorf("config dir mode = %o, want 700", di.Mode().Perm())
		}
	}
	entries, _ := os.ReadDir(filepath.Dir(path))
	if len(entries) != 1 {
		t.Errorf("leftover temp files in config dir: %v", entries)
	}
	out, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	if out.Token != in.Token || out.APIURL != in.APIURL || out.Output != in.Output {
		t.Errorf("round trip = %+v", out)
	}
}

func TestSaveRefusesInsecureURL(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", "")
	if err := Save(&Config{Token: "t", APIURL: "http://api.example.com", Output: "table"}); err == nil {
		t.Fatal("Save accepted a plain-http remote URL")
	}
}

func TestLoadFileIgnoresEnvironment(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	t.Setenv(EnvToken, "env-token")
	t.Setenv(EnvAPIURL, "http://localhost:8030")

	if err := Save(&Config{Token: "file-token", APIURL: DefaultAPIURL, Output: "table"}); err != nil {
		t.Fatal(err)
	}

	fromFile, err := LoadFile()
	if err != nil {
		t.Fatal(err)
	}
	if fromFile.Token != "file-token" || fromFile.APIURL != DefaultAPIURL {
		t.Errorf("LoadFile picked up the environment: %+v", fromFile)
	}
	if fromFile.APIURLSource != "config file" {
		t.Errorf("APIURLSource = %q, want config file", fromFile.APIURLSource)
	}

	merged, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	if merged.Token != "env-token" || merged.APIURL != "http://localhost:8030" || merged.APIURLSource != EnvAPIURL {
		t.Errorf("Load ignored the environment: %+v", merged)
	}
}
