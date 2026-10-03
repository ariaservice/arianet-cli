package commands

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/arianet/arianet-cli/internal/config"
	"github.com/arianet/arianet-cli/internal/printer"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

func newConfigureCmd() *cobra.Command {
	var (
		token      string
		tokenStdin bool
		apiURL     string
		output     string
	)

	cmd := &cobra.Command{
		Use:   "configure",
		Short: "Configure the Arianet CLI",
		Long: `Configure authentication and default settings for the Arianet CLI.

Running without flags starts an interactive setup wizard. The token is typed
without being echoed to the screen.

The token is stored in ~/.config/arianet/config.yaml, readable only by you
(mode 0600). Prefer --token-stdin (or the interactive prompt) over --token:
a token passed as an argument is visible to other users in the process list
and is saved in your shell history.

Examples:
  arianet configure
  echo "$ARIANET_TOKEN" | arianet configure --token-stdin
  arianet configure --token <your-api-token>
  arianet configure --output json
  arianet configure show`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Non-interactive: flags provided
			if cmd.Flags().Changed("token") && tokenStdin {
				return fmt.Errorf("use either --token or --token-stdin, not both")
			}
			if tokenStdin {
				raw, err := io.ReadAll(io.LimitReader(cmd.InOrStdin(), 4096))
				if err != nil {
					return fmt.Errorf("cannot read token from stdin: %w", err)
				}
				token = strings.TrimSpace(string(raw))
				if token == "" {
					return fmt.Errorf("no token received on stdin")
				}
			}
			if cmd.Flags().Changed("token") || tokenStdin || cmd.Flags().Changed("api-url") || cmd.Flags().Changed("output") {
				existing, _ := config.Load("")
				if existing == nil {
					existing = &config.Config{
						APIURL: config.DefaultAPIURL,
						Output: "table",
					}
				}
				if token != "" {
					existing.Token = token
				}
				if apiURL != "" {
					existing.APIURL = apiURL
				}
				if output != "" {
					if output != "table" && output != "json" {
						return fmt.Errorf("output must be 'table' or 'json'")
					}
					existing.Output = output
				}
				if err := config.Save(existing); err != nil {
					return err
				}
				filePath, _ := config.FilePath()
				printer.Success(fmt.Sprintf("Configuration saved to %s", filePath))
				return nil
			}

			// Interactive wizard
			return runInteractiveConfigure()
		},
	}

	cmd.Flags().StringVar(&token, "token", "", "API token")
	cmd.Flags().BoolVar(&tokenStdin, "token-stdin", false, "read the API token from stdin (keeps it out of process list and shell history)")
	cmd.Flags().StringVar(&apiURL, "api-url", "", fmt.Sprintf("API base URL (default: %s)", config.DefaultAPIURL))
	cmd.Flags().StringVar(&output, "output", "", "default output format: table|json")

	// show sub-command
	showCmd := &cobra.Command{
		Use:   "show",
		Short: "Show current configuration",
		RunE: func(cmd *cobra.Command, args []string) error {
			existing, err := config.Load("")
			if err != nil {
				return err
			}
			filePath, _ := config.FilePath()
			fmt.Printf("  Config file : %s\n", printer.Dim(filePath))
			fmt.Printf("  API URL     : %s\n", printer.Cyan(existing.APIURL))
			fmt.Printf("  Output      : %s\n", existing.Output)
			if existing.Token != "" {
				masked := maskToken(existing.Token)
				fmt.Printf("  Token       : %s\n", printer.Dim(masked))
			} else {
				fmt.Printf("  Token       : %s\n", printer.Dim("not set"))
			}
			return nil
		},
	}

	cmd.AddCommand(showCmd)
	return cmd
}

func runInteractiveConfigure() error {
	reader := bufio.NewReader(os.Stdin)
	existing, _ := config.Load("")
	if existing == nil {
		existing = &config.Config{
			APIURL: config.DefaultAPIURL,
			Output: "table",
		}
	}

	fmt.Println(printer.Bold("\nArianet CLI Configuration"))
	fmt.Println(printer.Dim("Press Enter to keep current value.\n"))

	// Token
	if existing.Token != "" {
		fmt.Printf("API Token [%s]: ", maskToken(existing.Token))
	} else {
		fmt.Print("API Token: ")
	}
	tokenInput := readLine(reader)
	if tokenInput != "" {
		existing.Token = tokenInput
	}

	// API URL
	fmt.Printf("API URL [%s]: ", existing.APIURL)
	urlInput := readLine(reader)
	if urlInput != "" {
		existing.APIURL = urlInput
	}

	// Output format
	fmt.Printf("Default output format [%s] (table/json): ", existing.Output)
	outputInput := readLine(reader)
	if outputInput != "" && (outputInput == "table" || outputInput == "json") {
		existing.Output = outputInput
	}

	if err := config.Save(existing); err != nil {
		return err
	}

	filePath, _ := config.FilePath()
	fmt.Println()
	printer.Success(fmt.Sprintf("Configuration saved to %s", filePath))
	return nil
}

// readSecret reads a line without echoing it when stdin is a terminal.
func readSecret(r *bufio.Reader) string {
	fd := int(os.Stdin.Fd())
	if term.IsTerminal(fd) {
		b, err := term.ReadPassword(fd)
		fmt.Println()
		if err != nil {
			return ""
		}
		return strings.TrimSpace(string(b))
	}
	return readLine(r)
}

func readLine(r *bufio.Reader) string {
	line, _ := r.ReadString('\n')
	return strings.TrimSpace(line)
}

func maskToken(token string) string {
	if len(token) <= 8 {
		return strings.Repeat("*", len(token))
	}
	return token[:4] + strings.Repeat("*", len(token)-8) + token[len(token)-4:]
}
