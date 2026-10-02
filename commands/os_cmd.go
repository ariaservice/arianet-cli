package commands

import (
	"fmt"
	"strconv"

	"github.com/arianet/arianet-cli/internal/api"
	"github.com/arianet/arianet-cli/internal/printer"
	"github.com/spf13/cobra"
)

func newOSCmd() *cobra.Command {
	var datacenterID int

	cmd := &cobra.Command{
		Use:     "os",
		Aliases: []string{"templates"},
		Short:   "List available OS templates",
		Example: `  arianet os list --region 1
  arianet os list --region 1 --output json`,
	}

	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List OS templates for a region",
		Example: `  arianet os list --region 1
  arianet os list --region 1 --output json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := requireAuth()
			if err != nil {
				handleAPIError(err)
				return nil
			}

			if datacenterID == 0 {
				id, ok, err := chooseDatacenter(client, "Enter region ID to list OS templates")
				if err != nil {
					handleAPIError(err)
					return nil
				}
				if !ok {
					return nil
				}
				datacenterID = id
			}

			p := printer.New(cfg.Output)

			result, err := client.ListOSByDatacenter(datacenterID)
			if err != nil {
				handleAPIError(err)
				return nil
			}
			if cfg.Output == printer.FormatJSON {
				p.PrintJSON(result)
				return nil
			}
			if len(result) == 0 {
				printer.Info(fmt.Sprintf("No OS templates available for region #%d.", datacenterID))
				return nil
			}
			fmt.Println()
			p.Table(osHeaders(), osRows(result))
			return nil
		},
	}

	listCmd.Flags().IntVar(&datacenterID, "region", 0, "region/datacenter ID (required)")
	cmd.AddCommand(listCmd)
	return cmd
}

func osHeaders() []string {
	return []string{"ID", "Name", "Family"}
}

func osRows(entries []api.OSEntry) [][]string {
	rows := make([][]string, len(entries))
	for i, e := range entries {
		rows[i] = []string{strconv.Itoa(e.ID), e.Name, e.Family}
	}
	return rows
}
