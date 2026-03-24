package commands

import (
	"strconv"

	"github.com/arianet/arianet-cli/internal/printer"
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
		Short: "List all available regions",
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

			if len(regions) == 0 {
				printer.Info("No regions available.")
				return nil
			}

			rows := make([][]string, len(regions))
			for i, r := range regions {
				rows[i] = []string{
					strconv.Itoa(r.ID),
					r.Name,
					r.FriendlyName,
					r.CountryCode,
					printer.StatusColor(r.Status),
				}
			}
			p.Table([]string{"ID", "Name", "Friendly Name", "Country", "Status"}, rows)
			return nil
		},
	})

	return cmd
}
