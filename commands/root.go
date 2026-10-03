package commands

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/ariaservice/arianet-cli/internal/api"
	"github.com/ariaservice/arianet-cli/internal/config"
	"github.com/ariaservice/arianet-cli/internal/printer"
	"github.com/spf13/cobra"
)

var (
	outputFormat   string
	tokenOverride  string
	apiURLOverride string
	cfg            *config.Config
	cfgErr         error
)

var rootCmd = &cobra.Command{
	Use:   "arianet",
	Short: "Arianet CLI — manage your cloud infrastructure",
	Long: `Arianet CLI is the official command-line interface for the Arianet cloud platform.

Manage servers, SSH keys, firewalls, and your account from the terminal.

Get started:
  arianet configure            Configure your API token
  arianet server list          List your cloud servers
  arianet region list          List available regions

Documentation: https://docs.ariaservice.net/cli`,
	SilenceUsage:  true,
	SilenceErrors: true,
}

// Execute is the entry point called from main.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		printer.Error(err.Error())
		os.Exit(1)
	}
}

func init() {
	rootCmd.PersistentFlags().StringVarP(&outputFormat, "output", "o", "", "output format: table|json (default: table)")
	rootCmd.PersistentFlags().StringVar(&tokenOverride, "token", "", "API token (overrides config and $ARIANET_TOKEN)")
	rootCmd.PersistentFlags().StringVar(&apiURLOverride, "api-url", "", "API base URL (overrides config)")

	cobra.OnInitialize(initConfig)

	// Register all sub-commands
	rootCmd.AddCommand(
		newConfigureCmd(),
		newAuthCmd(),
		newServerCmd(),
		newBalanceCmd(),
		newSSHCmd(),
		newFirewallCmd(),
		newRegionCmd(),
		newOSCmd(),
		newPlanCmd(),
		newVersionCmd(),
	)
}

// initConfig loads the configuration once per run.
func initConfig() {
	var err error
	cfg, err = config.Load(tokenOverride)
	cfgErr = err
	if err != nil {
		// Commands that need the config report cfgErr from requireAuth.
		cfg = &config.Config{
			APIURL: config.DefaultAPIURL,
			Output: "table",
		}
	}
	// Flag overrides config file
	if outputFormat != "" {
		cfg.Output = outputFormat
	}
	if apiURLOverride != "" {
		cfg.APIURL = apiURLOverride
	}
}

// requireAuth checks that a token is configured and returns an API client.
// Commands that need authentication call this helper.
func requireAuth() (*api.Client, error) {
	if cfgErr != nil {
		return nil, cfgErr
	}
	if err := config.ValidateAPIURL(cfg.APIURL); err != nil {
		return nil, err
	}
	if cfg.Token == "" {
		return nil, fmt.Errorf(
			"no API token configured\n\n" +
				"Run:  arianet configure --token <your-token>\n" +
				"Or set the ARIANET_TOKEN environment variable.\n\n" +
				"Generate tokens at: https://cloud.ariaservice.net/users/api-tokens",
		)
	}
	return api.New(cfg.APIURL, cfg.Token), nil
}

// handleAPIError prints an API error in a user-friendly way and exits.
func handleAPIError(err error) {
	msg, hints := describeError(err)
	if cfg != nil && cfg.Output == printer.FormatJSON {
		out := map[string]any{"success": false, "error": map[string]any{"message": msg}}
		if apiErr, ok := err.(*api.APIError); ok {
			out["error"] = map[string]any{"code": apiErr.Code, "message": apiErr.Message, "status": apiErr.Status}
		}
		b, _ := json.Marshal(out)
		fmt.Fprintln(os.Stderr, string(b))
		os.Exit(1)
	}
	printer.Error(msg)
	for _, h := range hints {
		printer.Hint(h)
	}
	if len(hints) > 0 {
		fmt.Println()
	}
	os.Exit(1)
}

// describeError turns an API error into a message and follow-up hints.
func describeError(err error) (string, []string) {
	apiErr, ok := err.(*api.APIError)
	if !ok {
		return err.Error(), nil
	}

	switch apiErr.Code {
	case "UNAUTHORIZED":
		return "Authentication failed: " + apiErr.Message, []string{
			"Check your token with: arianet configure show",
			"Create a new one at https://cloud.ariaservice.net/users/api-tokens",
		}
	case "INSUFFICIENT_SCOPE":
		return apiErr.Message, []string{"Create a token that includes the required scope at https://cloud.ariaservice.net/users/api-tokens"}
	case "IP_NOT_ALLOWED":
		return apiErr.Message, []string{"Your token is restricted to an IP allowlist; add this machine's address to the token."}
	case "RATE_LIMIT_EXCEEDED":
		msg := "Rate limit exceeded."
		if apiErr.RetryAfter > 0 {
			msg += " " + retryAfterText(apiErr.RetryAfter)
		}
		return msg, nil
	case "INSUFFICIENT_BALANCE":
		return apiErr.Message, []string{"Check your wallets with: arianet balance"}
	case "NO_DEFAULT_WALLET":
		return apiErr.Message, []string{"Pass --currency <id> (see: arianet balance)"}
	case "UPSTREAM_ERROR", "UPSTREAM_UNAVAILABLE", "WRITES_UNAVAILABLE", "INTERNAL_ERROR", "BAD_GATEWAY_RESPONSE":
		return "Service temporarily unavailable. Please try again later or contact support at https://ariaservice.net", nil
	}

	if apiErr.Status == 429 {
		msg := "Too many requests."
		if apiErr.RetryAfter > 0 {
			msg += " " + retryAfterText(apiErr.RetryAfter)
		}
		return msg, nil
	}
	return apiErr.Message, nil
}

func retryAfterText(seconds int) string {
	if seconds >= 120 {
		return fmt.Sprintf("Try again in about %d minutes.", (seconds+59)/60)
	}
	return fmt.Sprintf("Try again in %d seconds.", seconds)
}
