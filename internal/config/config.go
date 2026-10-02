package config

import (
	"fmt"
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

	// Environment variables override config file
	v.SetEnvPrefix("ARIANET")
	v.BindEnv("token", EnvToken)
	v.BindEnv("api_url", EnvAPIURL)

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

	return cfg, nil
}

// Save writes the config to disk.
func Save(cfg *Config) error {
	dir, err := Dir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("cannot create config directory: %w", err)
	}

	v := viper.New()
	v.SetConfigType("yaml")
	v.Set("token", cfg.Token)
	v.Set("api_url", cfg.APIURL)
	v.Set("output", cfg.Output)

	path := filepath.Join(dir, "config.yaml")
	if err := v.WriteConfigAs(path); err != nil {
		return fmt.Errorf("cannot write config: %w", err)
	}
	// Restrict config file permissions (contains token)
	return os.Chmod(path, 0600)
}
