package commands

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/ariaservice/arianet-cli/internal/api"
	"github.com/ariaservice/arianet-cli/internal/printer"
	"github.com/spf13/cobra"
)

func newSSHCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "ssh",
		Aliases: []string{"ssh-keys", "ssh-key"},
		Short:   "Manage SSH keys",
	}

	cmd.AddCommand(
		newSSHListCmd(),
		newSSHGetCmd(),
		newSSHAddCmd(),
		newSSHUpdateCmd(),
		newSSHDeleteCmd(),
	)
	return cmd
}

func newSSHListCmd() *cobra.Command {
	var (
		page  int
		limit int
	)

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List your SSH keys",
		Example: `  arianet ssh list
  arianet ssh list --page 2 --limit 20
  arianet ssh list --output json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := requireAuth()
			if err != nil {
				handleAPIError(err)
				return nil
			}

			keys, pagination, err := client.ListSSHKeys(page, limit)
			if err != nil {
				handleAPIError(err)
				return nil
			}

			p := printer.New(cfg.Output)
			if jsonMode() {
				p.PrintJSON(keys)
				return nil
			}

			if len(keys) == 0 {
				printer.Info("No SSH keys found.")
				printer.Info("Add one with: arianet ssh add --name my-key --key-file ~/.ssh/id_ed25519.pub --region 1")
				return nil
			}

			rows := make([][]string, len(keys))
			for i, k := range keys {
				region := "-"
				if k.DatacenterID != nil {
					region = strconv.Itoa(*k.DatacenterID)
				}
				rows[i] = []string{strconv.Itoa(k.ID), ptrOrDash(k.Name), region, k.CreatedAt}
			}
			p.Table([]string{"ID", "Name", "Region", "Created"}, rows)

			if pagination != nil {
				printer.PaginationFooter(pagination.CurrentPage, pagination.LastPage, int(pagination.Total), "SSH keys")
			}
			return nil
		},
	}

	cmd.Flags().IntVarP(&page, "page", "p", 1, "page number")
	cmd.Flags().IntVarP(&limit, "limit", "l", 15, "results per page (max 100)")
	return cmd
}

func newSSHGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get <id>",
		Short: "Get details of an SSH key",
		Args:  cobra.ExactArgs(1),
		Example: `  arianet ssh get 3
  arianet ssh get 3 --output json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := strconv.Atoi(args[0])
			if err != nil {
				return fmt.Errorf("invalid SSH key ID: %s", args[0])
			}

			client, err := requireAuth()
			if err != nil {
				handleAPIError(err)
				return nil
			}

			key, err := client.GetSSHKey(id)
			if err != nil {
				handleAPIError(err)
				return nil
			}

			p := printer.New(cfg.Output)
			if jsonMode() {
				p.PrintJSON(key)
				return nil
			}

			region := "-"
			if key.DatacenterID != nil {
				region = strconv.Itoa(*key.DatacenterID)
			}

			pubKey := key.Key
			if len(pubKey) > 60 {
				pubKey = pubKey[:57] + "..."
			}

			p.Table(
				[]string{"Field", "Value"},
				[][]string{
					{"ID", strconv.Itoa(key.ID)},
					{"Name", printer.Bold(derefStr(key.Name))},
					{"Region", region},
					{"Public Key", printer.Dim(pubKey)},
					{"Created At", key.CreatedAt},
				},
			)
			return nil
		},
	}
}

func newSSHAddCmd() *cobra.Command {
	var (
		name         string
		publicKey    string
		keyFile      string
		datacenterID int
	)

	cmd := &cobra.Command{
		Use:   "add",
		Short: "Add an SSH key",
		Example: `  arianet ssh add --name my-laptop --key-file ~/.ssh/id_ed25519.pub --region 1
  arianet ssh add --name my-laptop --key "ssh-ed25519 AAAA..." --region 1`,
		RunE: func(cmd *cobra.Command, args []string) error {
			keyValue := publicKey

			// Read from file if --key-file provided
			if keyFile != "" {
				read, err := readPublicKeyFile(expandHome(keyFile))
				if err != nil {
					return fmt.Errorf("cannot read key file %s: %w", keyFile, err)
				}
				keyValue = read
			}

			if keyValue == "" {
				return fmt.Errorf("provide a public key via --key or --key-file")
			}
			if err := checkPublicKey(keyValue); err != nil {
				return err
			}

			client, err := requireAuth()
			if err != nil {
				handleAPIError(err)
				return nil
			}

			req := api.CreateSSHKeyRequest{
				Name:         name,
				PublicKey:    keyValue,
				DatacenterID: datacenterID,
			}

			key, err := client.CreateSSHKey(req)
			if err != nil {
				handleAPIError(err)
				return nil
			}

			if jsonMode() {
				printer.New(cfg.Output).PrintJSON(key)
				return nil
			}

			printer.Success(fmt.Sprintf("SSH key %q added (ID: %d)", derefStr(key.Name), key.ID))
			return nil
		},
	}

	cmd.Flags().StringVar(&name, "name", "", "label for this SSH key")
	cmd.Flags().StringVar(&publicKey, "key", "", "public key content (ssh-ed25519 AAAA...)")
	cmd.Flags().StringVar(&keyFile, "key-file", "", "path to public key file (e.g. ~/.ssh/id_ed25519.pub)")
	cmd.Flags().IntVar(&datacenterID, "region", 0, "region ID the key belongs to (see: arianet region list)")

	_ = cmd.MarkFlagRequired("name")
	_ = cmd.MarkFlagRequired("region")
	return cmd
}

func newSSHUpdateCmd() *cobra.Command {
	var name string

	cmd := &cobra.Command{
		Use:     "update <id>",
		Short:   "Update an SSH key",
		Args:    cobra.ExactArgs(1),
		Example: `  arianet ssh update 3 --name new-label`,
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := strconv.Atoi(args[0])
			if err != nil {
				return fmt.Errorf("invalid SSH key ID: %s", args[0])
			}

			client, err := requireAuth()
			if err != nil {
				handleAPIError(err)
				return nil
			}

			key, err := client.UpdateSSHKey(id, api.UpdateSSHKeyRequest{Name: optStrPtr(name)})
			if err != nil {
				handleAPIError(err)
				return nil
			}

			if jsonMode() {
				printer.New(cfg.Output).PrintJSON(key)
				return nil
			}

			printer.Success(fmt.Sprintf("SSH key #%d updated to %q.", key.ID, derefStr(key.Name)))
			return nil
		},
	}

	cmd.Flags().StringVar(&name, "name", "", "new label for the SSH key")
	_ = cmd.MarkFlagRequired("name")
	return cmd
}

func newSSHDeleteCmd() *cobra.Command {
	var yes bool

	cmd := &cobra.Command{
		Use:   "delete <id>",
		Short: "Delete an SSH key",
		Args:  cobra.ExactArgs(1),
		Example: `  arianet ssh delete 3
  arianet ssh delete 3 --yes`,
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := strconv.Atoi(args[0])
			if err != nil {
				return fmt.Errorf("invalid SSH key ID: %s", args[0])
			}

			if !yes {
				if !confirm(fmt.Sprintf("Delete SSH key #%d?", id)) {
					printer.Info("Cancelled.")
					return nil
				}
			}

			client, err := requireAuth()
			if err != nil {
				handleAPIError(err)
				return nil
			}

			if err := client.DeleteSSHKey(id); err != nil {
				handleAPIError(err)
				return nil
			}

			reportDeleted("SSH key", strconv.Itoa(id))
			return nil
		},
	}

	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "skip confirmation prompt")
	return cmd
}

// expandHome replaces a leading ~ with the user's home directory.
func expandHome(path string) string {
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err == nil {
			return home + path[1:]
		}
	}
	return path
}

const maxPublicKeyBytes = 16 << 10

func readPublicKeyFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, maxPublicKeyBytes+1))
	if err != nil {
		return "", err
	}
	if len(b) > maxPublicKeyBytes {
		return "", errors.New("the file is too large to be a public key")
	}
	return strings.TrimSpace(string(b)), nil
}

// checkPublicKey refuses anything that is not an OpenSSH public key before
// it leaves the machine, in particular a private key picked by mistake.
func checkPublicKey(key string) error {
	if strings.Contains(key, "PRIVATE KEY") {
		return errors.New("this is a private key; pass the matching .pub file instead")
	}
	for _, prefix := range []string{"ssh-ed25519 ", "ssh-rsa ", "ecdsa-sha2-", "sk-ssh-ed25519@openssh.com ", "sk-ecdsa-sha2-"} {
		if strings.HasPrefix(key, prefix) {
			return nil
		}
	}
	return errors.New("not an OpenSSH public key: it should start with ssh-ed25519, ssh-rsa or ecdsa-sha2-")
}
