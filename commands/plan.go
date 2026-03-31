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

func newPlanCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "plan",
		Aliases: []string{"plans"},
		Short:   "List and inspect plans",
	}

	cmd.AddCommand(
		newPlanListCmd(),
		newPlanGetCmd(),
	)
	return cmd
}

func newPlanListCmd() *cobra.Command {
	var datacenterID int

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List plans for a region",
		Example: `  arianet plan list --region 1
  arianet plan list --region 1 --output json`,
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
					fmt.Print("Enter region ID to list plans: ")
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

			groups, err := client.ListPlansByDatacenterGrouped(datacenterID)
			if err != nil {
				handleAPIError(err)
				return nil
			}

			p := printer.New(cfg.Output)
			if cfg.Output == printer.FormatJSON {
				p.PrintJSON(groups)
				return nil
			}

			if len(groups) == 0 {
				printer.Info(fmt.Sprintf("No plans available for region #%d.", datacenterID))
				return nil
			}

			// Display each group with its plans
			for i, group := range groups {
				if i > 0 {
					fmt.Println()
				}
				fmt.Println(printer.Bold(group.Name))
				rows := make([][]string, len(group.Plans))
				for j, pl := range group.Plans {
					rows[j] = planToRow(pl)
				}
				p.Table(planHeaders(), rows)
			}
			return nil
		},
	}

	cmd.Flags().IntVar(&datacenterID, "region", 0, "datacenter/region ID (required)")
	return cmd
}

func newPlanGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get <id>",
		Short: "Get details and pricing of a plan",
		Args:  cobra.ExactArgs(1),
		Example: `  arianet plan get 5
  arianet plan get 5 --output json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := strconv.Atoi(args[0])
			if err != nil {
				return fmt.Errorf("invalid plan ID: %s", args[0])
			}

			client, err := requireAuth()
			if err != nil {
				handleAPIError(err)
				return nil
			}

			plan, err := client.GetPlan(id)
			if err != nil {
				handleAPIError(err)
				return nil
			}

			p := printer.New(cfg.Output)
			if cfg.Output == printer.FormatJSON {
				p.PrintJSON(plan)
				return nil
			}

		p.Table(
				[]string{"Field", "Value"},
				[][]string{
					{"ID", strconv.Itoa(plan.ID)},
					{"Name", printer.Bold(plan.Name)},
					{"CPU", fmtResource(plan.CPU)},
					{"RAM", fmtResource(plan.RAM)},
					{"Storage", fmtResource(plan.Storage)},
					{"Billing Cycle", derefStr(plan.Cycle)},
					{"Recommended", printer.BoolCheck(plan.Recommended)},
				},
			)

			if len(plan.Prices) > 0 {
				fmt.Println()
				fmt.Println(printer.Bold("Pricing:"))
				rows := make([][]string, len(plan.Prices))
				for i, pr := range plan.Prices {
					hourly, monthly := "-", "-"
					if pr.Hourly != nil {
						hourly = fmt.Sprintf("%.4f", *pr.Hourly)
					}
					if pr.Monthly != nil {
						monthly = fmt.Sprintf("%.2f", *pr.Monthly)
					}
					rows[i] = []string{pr.Code, pr.Currency, hourly, monthly}
				}
				p.Table([]string{"Code", "Currency", "Hourly", "Monthly"}, rows)
			}
			return nil
		},
	}
}

func planHeaders() []string {
	return []string{"ID", "Name", "CPU", "RAM", "Storage", "Billing", "Recommended"}
}

func fmtResource(r *api.ResourceValue) string {
	if r == nil {
		return "-"
	}
	return fmt.Sprintf("%d %s", r.Size, r.Unit)
}

func planToRow(pl api.Plan) []string {
	rec := "-"
	if pl.Recommended {
		rec = printer.BoolCheck(true)
	}
	return []string{
		strconv.Itoa(pl.ID),
		pl.Name,
		fmtResource(pl.CPU),
		fmtResource(pl.RAM),
		fmtResource(pl.Storage),
		derefStr(pl.Cycle),
		rec,
	}
}
