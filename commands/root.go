package commands

import (
	"fmt"
	"os"

	"github.com/arianet/arianet-cli/internal/api"
	"github.com/arianet/arianet-cli/internal/config"
	"github.com/arianet/arianet-cli/internal/printer"
	"github.com/spf13/cobra"
)

var (
	outputFormat   string
	tokenOverride  string
	apiURLOverride string
	cfg            *config.Config
)

var rootCmd = &cobra.Command{
	Use:   "arianet",
	Short: "Arianet CLI — manage your cloud infrastructure",
	Long: `Arianet CLI is the official command-line interface for the Arianet cloud platform.

Manage services, SSH keys, firewalls, and your account from the terminal.

Get started:
  arianet configure            Configure your API token
  arianet service list         List your cloud services
  arianet region list          List available regions

Documentation: https://docs.arianet.ir/cli`,
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
	if err != nil {
		// Non-fatal — commands that require config will check themselves
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
	if apiErr, ok := err.(*api.APIError); ok {
		if apiErr.Code == "INTERNAL_ERROR" {
			printer.Error("Service temporarily unavailable. Please try again later or contact support at https://ariaservice.net")
		} else {
			printer.Error(apiErr.Message)
		}
	} else {
		printer.Error(err.Error())
	}
	os.Exit(1)
}
