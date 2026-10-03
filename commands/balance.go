package commands

import (
	"fmt"
	"strings"

	"github.com/ariaservice/arianet-cli/internal/api"
	"github.com/ariaservice/arianet-cli/internal/printer"
	"github.com/spf13/cobra"
)

func newBalanceCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "balance",
		Short: "View account balance and transactions",
		Example: `  arianet balance
  arianet balance --output json
  arianet balance transactions
  arianet balance transactions --page 2
  arianet balance invoices
  arianet balance invoices get INV-1042`,
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

	cmd.AddCommand(newTransactionsCmd(), newInvoicesCmd())
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

func invoiceCurrency(inv api.Invoice) string {
	for _, it := range inv.Items {
		if it.Currency != nil {
			return it.Currency.Code
		}
	}
	return ""
}

func newInvoicesCmd() *cobra.Command {
	var (
		page   int
		limit  int
		status string
	)

	cmd := &cobra.Command{
		Use:     "invoices",
		Aliases: []string{"invoice"},
		Short:   "List and view invoices",
		Example: `  arianet balance invoices
  arianet balance invoices --status paid
  arianet balance invoices get INV-1042`,
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := requireAuth()
			if err != nil {
				handleAPIError(err)
				return nil
			}

			invoices, pagination, err := client.ListInvoices(page, limit, status)
			if err != nil {
				handleAPIError(err)
				return nil
			}

			p := printer.New(cfg.Output)
			if jsonMode() {
				p.PrintJSON(invoices)
				return nil
			}

			if len(invoices) == 0 {
				printer.Info("No invoices found.")
				return nil
			}

			rows := make([][]string, len(invoices))
			for i, inv := range invoices {
				rows[i] = []string{
					inv.Number,
					inv.Status,
					inv.PaymentStatus,
					fmt.Sprintf("%.2f", inv.Total),
					ptrOrDash(inv.IssuedAt),
					ptrOrDash(inv.PaidAt),
				}
			}
			p.Table([]string{"Number", "Status", "Payment", "Total", "Issued", "Paid"}, rows)

			if pagination != nil {
				printer.PaginationFooter(pagination.CurrentPage, pagination.LastPage, int(pagination.Total), "invoices")
			}
			return nil
		},
	}

	cmd.Flags().IntVarP(&page, "page", "p", 1, "page number")
	cmd.Flags().IntVarP(&limit, "limit", "l", 15, "results per page (max 100)")
	cmd.Flags().StringVar(&status, "status", "", "filter by invoice status")

	cmd.AddCommand(&cobra.Command{
		Use:   "get <number>",
		Short: "Show one invoice with its line items",
		Args:  cobra.ExactArgs(1),
		Example: `  arianet balance invoices get INV-1042
  arianet balance invoices get 1042 --output json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := requireAuth()
			if err != nil {
				handleAPIError(err)
				return nil
			}

			inv, err := client.GetInvoice(strings.TrimSpace(args[0]))
			if err != nil {
				handleAPIError(err)
				return nil
			}

			p := printer.New(cfg.Output)
			if jsonMode() {
				p.PrintJSON(inv)
				return nil
			}

			p.Table(
				[]string{"Field", "Value"},
				[][]string{
					{"Number", inv.Number},
					{"Status", inv.Status},
					{"Payment", inv.PaymentStatus},
					{"Subtotal", fmt.Sprintf("%.2f", inv.Subtotal)},
					{"Discount", fmt.Sprintf("%.2f", inv.Discount)},
					{"Tax", fmt.Sprintf("%.2f", inv.Tax)},
					{"Total", fmt.Sprintf("%.2f %s", inv.Total, invoiceCurrency(*inv))},
					{"Issued", ptrOrDash(inv.IssuedAt)},
					{"Due", ptrOrDash(inv.DueAt)},
					{"Paid", ptrOrDash(inv.PaidAt)},
				},
			)

			if len(inv.Items) > 0 {
				fmt.Println()
				rows := make([][]string, len(inv.Items))
				for i, it := range inv.Items {
					code := ""
					if it.Currency != nil {
						code = it.Currency.Code
					}
					rows[i] = []string{ptrOrDash(it.Description), fmt.Sprintf("%.2f %s", it.Amount, code)}
				}
				p.Table([]string{"Item", "Amount"}, rows)
			}
			return nil
		},
	})
	return cmd
}
