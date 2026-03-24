package commands

import (
	"fmt"
	"strconv"

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
		Short: "List all available plans",
		Example: `  arianet plan list
  arianet plan list --region 1
  arianet plan list --output json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := requireAuth()
			if err != nil {
				handleAPIError(err)
				return nil
			}

			p := printer.New(cfg.Output)

			if datacenterID > 0 {
				result, err := client.ListPlansByDatacenter(datacenterID)
				if err != nil {
					handleAPIError(err)
					return nil
				}
				if cfg.Output == printer.FormatJSON {
					p.PrintJSON(result)
					return nil
				}
				if len(result) == 0 {
					printer.Info(fmt.Sprintf("No plans available for region #%d.", datacenterID))
					return nil
				}
				rows := make([][]string, len(result))
				for i, pl := range result {
					rows[i] = planToRow(pl)
				}
				p.Table(planHeaders(), rows)
				return nil
			}

			result, err := client.ListPlans()
			if err != nil {
				handleAPIError(err)
				return nil
			}
			if cfg.Output == printer.FormatJSON {
				p.PrintJSON(result)
				return nil
			}
			if len(result) == 0 {
				printer.Info("No plans available.")
				return nil
			}
			rows := make([][]string, len(result))
			for i, pl := range result {
				rows[i] = planToRow(pl)
			}
			p.Table(planHeaders(), rows)
			return nil
		},
	}

	cmd.Flags().IntVar(&datacenterID, "region", 0, "filter by datacenter/region ID")
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

			region := printer.Dim("—")
			if plan.Datacenter != nil {
				region = fmt.Sprintf("%s (%s)", plan.Datacenter.Name, plan.Datacenter.CountryCode)
			}
			price := printer.Dim("—")
			if plan.DollarPrice > 0 {
				price = fmt.Sprintf("$%.4f / %s", plan.DollarPrice, plan.BillingCycle)
			}

			p.Table(
				[]string{"Field", "Value"},
				[][]string{
					{"ID", strconv.Itoa(plan.ID)},
					{"Name", printer.Bold(plan.Name)},
					{"Region", region},
					{"Billing Cycle", plan.BillingCycle},
					{"Price (USD)", price},
					{"Recommended", printer.BoolCheck(plan.IsRecommended)},
				},
			)

			if len(plan.Prices) > 0 {
				fmt.Println()
				fmt.Println(printer.Bold("Local Pricing:"))
				rows := make([][]string, len(plan.Prices))
				for i, pr := range plan.Prices {
					rows[i] = []string{
						pr.CurrencyCode,
						pr.CurrencyName,
						fmt.Sprintf("%.2f", pr.Price),
					}
				}
				p.Table([]string{"Currency Code", "Currency", "Price"}, rows)
			}
			return nil
		},
	}
}

func planHeaders() []string {
	return []string{"ID", "Name", "Region", "Billing", "Price (USD)", "Recommended"}
}

func planToRow(pl api.Plan) []string {
	region := printer.Dim("—")
	if pl.Datacenter != nil {
		region = pl.Datacenter.Name
	}
	price := printer.Dim("—")
	if pl.DollarPrice > 0 {
		price = fmt.Sprintf("$%.4f", pl.DollarPrice)
	}
	rec := printer.Dim("–")
	if pl.IsRecommended {
		rec = printer.BoolCheck(true)
	}
	return []string{
		strconv.Itoa(pl.ID),
		pl.Name,
		region,
		pl.BillingCycle,
		price,
		rec,
	}
}
