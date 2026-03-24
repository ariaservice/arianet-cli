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
		Example: `  arianet os list
  arianet os list --region 1
  arianet os list --output json`,
	}

	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List all available OS templates",
		Example: `  arianet os list
  arianet os list --region 1
  arianet os list --output json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := requireAuth()
			if err != nil {
				handleAPIError(err)
				return nil
			}

			p := printer.New(cfg.Output)

			if datacenterID > 0 {
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
				rows := osRows(result)
				p.Table(osHeaders(), rows)
				return nil
			}

			result, err := client.ListOS()
			if err != nil {
				handleAPIError(err)
				return nil
			}
			if cfg.Output == printer.FormatJSON {
				p.PrintJSON(result)
				return nil
			}
			if len(result) == 0 {
				printer.Info("No OS templates available.")
				return nil
			}
			p.Table(osHeaders(), osRows(result))
			return nil
		},
	}

	listCmd.Flags().IntVar(&datacenterID, "region", 0, "filter by datacenter/region ID")
	cmd.AddCommand(listCmd)
	return cmd
}

func osHeaders() []string {
	return []string{"ID", "Name", "Region ID", "Status"}
}

func osRows(templates []api.OSTemplate) [][]string {
	rows := make([][]string, len(templates))
	for i, t := range templates {
		status := printer.BoolCheck(t.Status)
		if !t.Status {
			status = printer.Dim("inactive")
		}
		rows[i] = []string{
			strconv.Itoa(t.ID),
			t.Name,
			strconv.Itoa(t.DatacenterID),
			status,
		}
	}
	return rows
}
