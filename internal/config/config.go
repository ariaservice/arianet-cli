package config

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"

	"github.com/spf13/viper"
)

const (
	DefaultAPIURL = "https://api.ariaservice.net"
	EnvToken      = "ARIANET_TOKEN"
	EnvAPIURL     = "ARIANET_API_URL"
)

// Config holds all CLI configuration.
type Config struct {
	Token  string `mapstructure:"token"`
	APIURL string `mapstructure:"api_url"`
	Output string `mapstructure:"output"`

	// APIURLSource names where APIURL came from ("config file", the
	// ARIANET_API_URL variable, a flag) or is empty for the built-in default.
	APIURLSource string `mapstructure:"-"`
}

// Dir returns the config directory path (~/.config/arianet).
func Dir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		home, herr := os.UserHomeDir()
		if herr != nil {
			return "", fmt.Errorf("cannot determine config directory: %w", err)
		}
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "arianet"), nil
}

// FilePath returns the full path to the config file.
func FilePath() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.yaml"), nil
}

// Load reads the config file and environment variables into a Config.
// Environment variables take precedence over the config file.
// The tokenOverride (from --token flag) takes highest precedence.
func Load(tokenOverride string) (*Config, error) {
	return load(tokenOverride, true)
}

// LoadFile reads the config file alone. Commands that rewrite the file start
// from this, so a token or URL that only lives in the environment for this
// session is never written to disk behind the user's back.
func LoadFile() (*Config, error) {
	return load("", false)
}

func load(tokenOverride string, withEnv bool) (*Config, error) {
	dir, err := Dir()
	if err != nil {
		return nil, err
	}

	v := viper.New()
	v.SetConfigName("config")
	v.SetConfigType("yaml")
	v.AddConfigPath(dir)

	v.SetDefault("api_url", DefaultAPIURL)
	v.SetDefault("output", "table")

	if withEnv {
		v.SetEnvPrefix("ARIANET")
		v.BindEnv("token", EnvToken)
		v.BindEnv("api_url", EnvAPIURL)
	}

	if err := v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return nil, fmt.Errorf("error reading config: %w", err)
		}
		// Config file not found — use defaults/env only
	}

	cfg := &Config{}
	if err := v.Unmarshal(cfg); err != nil {
		return nil, fmt.Errorf("error parsing config: %w", err)
	}

	// --token flag takes highest precedence
	if tokenOverride != "" {
		cfg.Token = tokenOverride
	}

	switch {
	case withEnv && os.Getenv(EnvAPIURL) != "":
		cfg.APIURLSource = EnvAPIURL
	case v.ConfigFileUsed() != "" && v.InConfig("api_url"):
		cfg.APIURLSource = "config file"
	}

	if err := ValidateAPIURL(cfg.APIURL); err != nil {
		return nil, err
	}

	return cfg, nil
}

// ValidateAPIURL refuses an API address that would send the token somewhere
// it can be read: plain http is only accepted for a loopback host (local
// development), and credentials embedded in the URL are never accepted.
func ValidateAPIURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return fmt.Errorf("api_url %q is not a valid URL", raw)
	}
	if u.User != nil {
		return fmt.Errorf("api_url must not contain credentials")
	}
	switch u.Scheme {
	case "https":
		return nil
	case "http":
		host := u.Hostname()
		if host == "localhost" {
			return nil
		}
		if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
			return nil
		}
		return fmt.Errorf("api_url %q uses plain http, which would send your token unencrypted; use https", raw)
	default:
		return fmt.Errorf("api_url %q must start with https://", raw)
	}
}

// Save writes the config to disk.
func Save(cfg *Config) error {
	if err := ValidateAPIURL(cfg.APIURL); err != nil {
		return err
	}
	dir, err := Dir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("cannot create config directory: %w", err)
	}
	if err := os.Chmod(dir, 0700); err != nil {
		return fmt.Errorf("cannot restrict config directory: %w", err)
	}

	v := viper.New()
	v.SetConfigType("yaml")
	v.Set("token", cfg.Token)
	v.Set("api_url", cfg.APIURL)
	v.Set("output", cfg.Output)

	// The token is written to a private temp file (0600 from creation) and
	// renamed into place, so it is never readable by other users, not even
	// for an instant.
	tmp, err := os.CreateTemp(dir, ".config-*.yaml")
	if err != nil {
		return fmt.Errorf("cannot write config: %w", err)
	}
	tmpName := tmp.Name()
	tmp.Close()
	defer os.Remove(tmpName)

	if err := os.Chmod(tmpName, 0600); err != nil {
		return fmt.Errorf("cannot write config: %w", err)
	}
	if err := v.WriteConfigAs(tmpName); err != nil {
		return fmt.Errorf("cannot write config: %w", err)
	}
	if err := os.Rename(tmpName, filepath.Join(dir, "config.yaml")); err != nil {
		return fmt.Errorf("cannot write config: %w", err)
	}
	return nil
}
