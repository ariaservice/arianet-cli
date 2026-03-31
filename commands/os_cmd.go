package commands

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"

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

			// If no region ID provided, show available regions and prompt for selection
			if datacenterID == 0 {
				regions, err := client.ListRegions()
				if err != nil {
					handleAPIError(err)
					return nil
				}

				if len(regions) == 0 {
					printer.Info("No regions available.")
					return nil
				}

				// Display regions
				rows := make([][]string, len(regions))
				for i, r := range regions {
					rows[i] = []string{
						strconv.Itoa(r.ID),
						r.Name,
						derefStr(r.DisplayName),
						derefStr(r.Country),
					}
				}
				p := printer.New(cfg.Output)
				p.Table([]string{"ID", "Name", "Display Name", "Country"}, rows)

				// If JSON output is requested, return regions as JSON
				if cfg.Output == printer.FormatJSON {
					fmt.Println()
					p.PrintJSON(regions)
					return nil
				}

				// Interactive prompt for region ID
				fmt.Println()
				reader := bufio.NewReader(os.Stdin)
				for {
					fmt.Print("Enter region ID For return OS Template List: ")
					input, _ := reader.ReadString('\n')
					input = strings.TrimSpace(input)

					regionID, err := strconv.Atoi(input)
					if err != nil || regionID <= 0 {
						printer.Warn("Invalid input. Please enter a valid region ID.")
						continue
					}

					// Verify region ID exists
					found := false
					for _, r := range regions {
						if r.ID == regionID {
							found = true
							break
						}
					}
					if !found {
						printer.Warn(fmt.Sprintf("Region ID %d not found. Please try again.", regionID))
						continue
					}

					datacenterID = regionID
					break
				}
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
	return []string{"ID", "Name"}
}

func osRows(templates []api.OSTemplate) [][]string {
	rows := make([][]string, len(templates))
	for i, t := range templates {
		name := t.Name
		if t.DisplayName != "" && t.DisplayName != t.Name {
			name = t.DisplayName
		}
		rows[i] = []string{
			strconv.Itoa(t.ID),
			name,
		}
	}
	return rows
}
