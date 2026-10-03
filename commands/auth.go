package commands

import (
	"fmt"
	"strconv"

	"github.com/ariaservice/arianet-cli/internal/config"
	"github.com/ariaservice/arianet-cli/internal/printer"
	"github.com/spf13/cobra"
)

func newAuthCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "auth",
		Short: "Manage authentication and API tokens",
	}

	cmd.AddCommand(
		newWhoamiCmd(),
		newLogoutCmd(),
		newTokenCmd(),
	)
	return cmd
}

func newWhoamiCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "whoami",
		Short: "Show the current authenticated user",
		Example: `  arianet auth whoami
  arianet auth whoami --output json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := requireAuth()
			if err != nil {
				handleAPIError(err)
				return nil
			}

			user, err := client.GetMe()
			if err != nil {
				handleAPIError(err)
				return nil
			}

			p := printer.New(cfg.Output)
			if cfg.Output == printer.FormatJSON {
				p.PrintJSON(user)
				return nil
			}

			verified := printer.Dim("–")
			if user.Verified {
				verified = printer.BoolCheck(true)
			}
			email := derefStr(user.Email)
			if email == "" {
				email = printer.Dim("—")
			}
			mobile := derefStr(user.Mobile)
			if mobile == "" {
				mobile = printer.Dim("—")
			}
			p.Table(
				[]string{"ID", "Email", "Mobile", "Status", "Verified"},
				[][]string{{
					strconv.Itoa(user.ID),
					email,
					mobile,
					printer.StatusColor(user.Status),
					verified,
				}},
			)
			return nil
		},
	}
}

func newLogoutCmd() *cobra.Command {
	var yes bool

	cmd := &cobra.Command{
		Use:   "logout",
		Short: "Revoke the current API token",
		Long:  "Revokes the current API token on the server and removes it from your local config.",
		RunE: func(cmd *cobra.Command, args []string) error {
			if !yes {
				if !confirm("Revoke current API token and log out?") {
					printer.Info("Cancelled.")
					return nil
				}
			}

			client, err := requireAuth()
			if err != nil {
				handleAPIError(err)
				return nil
			}

			if err := client.Logout(); err != nil {
				handleAPIError(err)
				return nil
			}

			// Forget the token only if it is the one stored in the file, and
			// rewrite the file from its own contents so nothing from the
			// environment or flags is persisted.
			if stored, err := config.LoadFile(); err == nil && stored.Token != "" && stored.Token == cfg.Token {
				stored.Token = ""
				_ = config.Save(stored)
			}

			printer.Success("Logged out. Token has been revoked.")
			return nil
		},
	}

	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "skip confirmation prompt")
	return cmd
}

func newTokenCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "tokens",
		Short: "Manage API tokens",
	}

	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List all API tokens",
		Example: `  arianet auth tokens list
  arianet auth tokens list --output json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := requireAuth()
			if err != nil {
				handleAPIError(err)
				return nil
			}

			tokens, err := client.ListTokens()
			if err != nil {
				handleAPIError(err)
				return nil
			}

			p := printer.New(cfg.Output)
			if cfg.Output == printer.FormatJSON {
				p.PrintJSON(tokens)
				return nil
			}

			if len(tokens) == 0 {
				printer.Info("No tokens found.")
				return nil
			}

			rows := make([][]string, len(tokens))
			for i, t := range tokens {
				lastUsed := derefStr(t.LastUsed)
				if lastUsed == "" {
					lastUsed = printer.Dim("never")
				}
				expiresAt := derefStr(t.Expires)
				if expiresAt == "" {
					expiresAt = printer.Dim("never")
				}
				rows[i] = []string{
					strconv.Itoa(t.ID),
					t.Name,
					t.Prefix + "...",
					lastUsed,
					expiresAt,
					t.CreatedAt,
				}
			}
			p.Table([]string{"ID", "Name", "Prefix", "Last Used", "Expires", "Created"}, rows)
			return nil
		},
	}

	revokeCmd := &cobra.Command{
		Use:   "revoke <id>",
		Short: "Revoke an API token by ID",
		Args:  cobra.ExactArgs(1),
		Example: `  arianet auth tokens revoke 5
  arianet auth tokens revoke 5 --yes`,
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := strconv.Atoi(args[0])
			if err != nil {
				return fmt.Errorf("invalid token ID: %s", args[0])
			}

			yes, _ := cmd.Flags().GetBool("yes")
			if !yes {
				if !confirm(fmt.Sprintf("Revoke token #%d?", id)) {
					printer.Info("Cancelled.")
					return nil
				}
			}

			client, err := requireAuth()
			if err != nil {
				handleAPIError(err)
				return nil
			}

			if err := client.RevokeToken(id); err != nil {
				handleAPIError(err)
				return nil
			}

			printer.Success(fmt.Sprintf("Token #%d revoked.", id))
			return nil
		},
	}
	revokeCmd.Flags().BoolP("yes", "y", false, "skip confirmation prompt")

	cmd.AddCommand(listCmd, revokeCmd)
	return cmd
}
