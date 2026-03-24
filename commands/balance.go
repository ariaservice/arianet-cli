package commands

import (
	"fmt"

	"github.com/arianet/arianet-cli/internal/printer"
	"github.com/spf13/cobra"
)

func newBalanceCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "balance",
		Short: "View account balance and transactions",
		Example: `  arianet balance
  arianet balance --output json
  arianet balance transactions
  arianet balance transactions --page 2`,
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := requireAuth()
			if err != nil {
				handleAPIError(err)
				return nil
			}

			data, err := client.GetBalance()
			if err != nil {
				handleAPIError(err)
				return nil
			}

			p := printer.New(cfg.Output)
			if cfg.Output == printer.FormatJSON {
				p.PrintJSON(data)
				return nil
			}

			if len(data.Wallets) == 0 {
				printer.Info("No wallets found.")
				return nil
			}

			rows := make([][]string, len(data.Wallets))
			for i, w := range data.Wallets {
				defaultLabel := "-"
				if w.Primary {
					defaultLabel = printer.BoolCheck(true)
				}
				symbol := ""
				code := ""
				name := ""
				if w.Currency != nil {
					symbol = derefStr(w.Currency.Symbol)
					code = w.Currency.Code
					name = w.Currency.Name
				}
				balanceStr := fmt.Sprintf("%.2f", w.Balance)
				if symbol != "" {
					balanceStr += " " + symbol
				}
				rows[i] = []string{
					fmt.Sprintf("%d", w.ID),
					balanceStr,
					code,
					name,
					printer.StatusColor(w.Status),
					defaultLabel,
				}
			}
			p.Table(
				[]string{"ID", "Balance", "Currency", "Name", "Status", "Default"},
				rows,
			)
			return nil
		},
	}

	txCmd := newTransactionsCmd()
	cmd.AddCommand(txCmd)
	return cmd
}

func newTransactionsCmd() *cobra.Command {
	var (
		page  int
		limit int
	)

	cmd := &cobra.Command{
		Use:   "transactions",
		Short: "View transaction history",
		Example: `  arianet balance transactions
  arianet balance transactions --page 2 --limit 20
  arianet balance transactions --output json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := requireAuth()
			if err != nil {
				handleAPIError(err)
				return nil
			}

			txs, pagination, err := client.ListTransactions(page, limit)
			if err != nil {
				handleAPIError(err)
				return nil
			}

			p := printer.New(cfg.Output)
			if cfg.Output == printer.FormatJSON {
				p.PrintJSON(txs)
				return nil
			}

			if len(txs) == 0 {
				printer.Info("No transactions found.")
				return nil
			}

			rows := make([][]string, len(txs))
			for i, tx := range txs {
				symbol := ""
				if tx.Currency != nil {
					symbol = derefStr(tx.Currency.Symbol)
				}
				amountStr := fmt.Sprintf("%.2f", tx.Amount)
				if symbol != "" {
					amountStr += " " + symbol
				}
				rows[i] = []string{
					fmt.Sprintf("%d", tx.ID),
					amountStr,
					tx.Mode,
					derefStr(tx.Type),
					derefStr(tx.Status),
					tx.CreatedAt,
				}
			}
			p.Table(
				[]string{"ID", "Amount", "Mode", "Type", "Status", "Date"},
				rows,
			)

			if pagination != nil {
				fmt.Printf("\nPage %d of %d  -  %d total transactions\n",
					pagination.CurrentPage, pagination.LastPage, pagination.Total,
				)
			}
			return nil
		},
	}

	cmd.Flags().IntVarP(&page, "page", "p", 1, "page number")
	cmd.Flags().IntVarP(&limit, "limit", "l", 15, "results per page")
	return cmd
}
