package commands

import (
	"github.com/ariaservice/arianet-cli/internal/printer"
	"github.com/spf13/cobra"
)

func newRegionCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "region",
		Aliases: []string{"regions"},
		Short:   "List available regions",
		Example: `  arianet region list
  arianet region list --output json`,
	}

	cmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List all orderable locations",
		Long: `List all orderable locations.

The ID column is the datacenter ID: pass it as --region to the other commands.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := requireAuth()
			if err != nil {
				handleAPIError(err)
				return nil
			}

			regions, err := client.ListRegions()
			if err != nil {
				handleAPIError(err)
				return nil
			}

			p := printer.New(cfg.Output)
			if cfg.Output == printer.FormatJSON {
				p.PrintJSON(regions)
				return nil
			}

			list := flattenForDisplay(regions)
			if len(list) == 0 {
				printer.Info("No regions available.")
				return nil
			}

			datacenterTable(p, list)
			return nil
		},
	})

	return cmd
}
