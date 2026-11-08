package commands

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/ariaservice/arianet-cli/internal/api"
	"github.com/ariaservice/arianet-cli/internal/printer"
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

			if datacenterID == 0 {
				id, ok, err := chooseDatacenter(client, "Enter region ID to list plans")
				if err != nil {
					handleAPIError(err)
					return nil
				}
				if !ok {
					return nil
				}
				datacenterID = id
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
				fmt.Println(printer.Bold(printer.CleanLine(group.Name)))
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
					{"CPU", fmtCPU(plan.CPU)},
					{"RAM", fmtRAM(plan.RAM)},
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
					if pr.Hourly != "" {
						hourly = pr.Hourly
					}
					if pr.Monthly != "" {
						monthly = pr.Monthly
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
	return []string{"ID", "Name", "CPU", "RAM", "Storage", "Hourly", "Monthly", "Currency"}
}

func fmtResource(r *api.ResourceValue) string {
	if r == nil {
		return "-"
	}
	return fmt.Sprintf("%d %s", r.Size, r.Unit)
}

// fmtCPU prints the plan's CPU as a vCPU count; the size is always a number
// of virtual cores, whatever unit label the catalog carries.
func fmtCPU(r *api.ResourceValue) string {
	if r == nil {
		return "-"
	}
	return fmt.Sprintf("%d vCPU", r.Size)
}

// fmtRAM prints memory, switching MB values of 1024 and above to GB
// (1024 MB -> "1 GB", 1536 MB -> "1.5 GB").
func fmtRAM(r *api.ResourceValue) string {
	if r == nil {
		return "-"
	}
	switch strings.ToLower(r.Unit) {
	case "mb", "megabyte", "":
		if r.Size >= 1024 {
			gb := math.Round(float64(r.Size)/1024*100) / 100
			return strconv.FormatFloat(gb, 'f', -1, 64) + " GB"
		}
		return fmt.Sprintf("%d MB", r.Size)
	}
	return fmtResource(r)
}

func planToRow(pl api.Plan) []string {
	hourly, monthly, currency := "-", "-", "-"
	if len(pl.Prices) > 0 {
		pr := pl.Prices[0]
		currency = pr.Code
		if pr.Hourly != "" {
			hourly = pr.Hourly
		}
		if pr.Monthly != "" {
			monthly = pr.Monthly
		}
	}
	name := pl.DisplayName
	if name == "" {
		name = pl.Name
	}
	return []string{
		strconv.Itoa(pl.ID),
		name,
		fmtCPU(pl.CPU),
		fmtRAM(pl.RAM),
		fmtResource(pl.Storage),
		hourly,
		monthly,
		currency,
	}
}
