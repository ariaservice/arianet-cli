// EXAMPLES: How to use the enhanced printer package in CLI commands

package commands

import (
	"fmt"
	"time"

	"github.com/arianet/arianet-cli/internal/printer"
)

// ─── Example 1: List Command with Multiple Output Formats ──────────────

/*
func newServiceListCmd() *cobra.Command {
	var (
		page   int
		limit  int
		status string
		mode   string // Add this for output mode selection
	)

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List your cloud services",
		Example: `  arianet service list
  arianet service list --status active
  arianet service list --output json
  arianet service list --output yaml
  arianet service list --mode compact`,
		RunE: func(cmd *cobra.Command, args []string) error {
			start := time.Now()
			
			client, err := requireAuth()
			if err != nil {
				handleAPIError(err)
				return nil
			}

			services, pagination, err := client.ListServices(page, limit, status)
			if err != nil {
				handleAPIError(err)
				return nil
			}

			p := printer.New(cfg.Output).
				WithMode(printer.OutputMode(mode))
			
			// Display different formats based on output flag
			switch cfg.Output {
			case printer.FormatJSON:
				p.PrintJSON(services)
			case printer.FormatYAML:
				p.PrintYAML(services)
			case printer.FormatCompact:
				p.PrintCompact(services)
			default:
				// Table format with custom rendering
				if len(services) == 0 {
					printer.EmptyState(
						"No services found",
						"Use 'arianet service create' to add a new service")
					return nil
				}

				rows := make([][]string, len(services))
				for i, s := range services {
					ip := s.IP
					if ip == "" {
						ip = printer.Dim("—")
					}
					protected := printer.Dim("–")
					if s.IsProtected {
						protected = printer.BoolCheck(true)
					}
					rows[i] = []string{
						fmt.Sprint(s.ID),
						s.Hostname,
						printer.StatusBadge(s.Status),
						s.Plan.Name,
						s.Datacenter.Name,
						s.OS.Name,
						ip,
						s.BillingCycle,
						protected,
						s.CreatedAt,
					}
				}

				// Use advanced table with better styling
				p.TableAdvanced(
					"Cloud Services",
					[]string{"ID", "Hostname", "Status", "Plan", "Region", "OS", "IP", "Billing", "Protected", "Created"},
					rows,
				)

				// Show pagination info
				if pagination != nil {
					fmt.Fprintf(p.Out, "%s\n",
						printer.ProgressBar(pagination.CurrentPage, pagination.LastPage, 15))
					fmt.Fprintf(p.Out, "  %s\n",
						printer.Dim(fmt.Sprintf(
							"Page %d of %d • %d total services",
							pagination.CurrentPage,
							pagination.LastPage,
							pagination.Total)))
				}
			}

			// Show timing for list operations
			if cfg.Verbose {
				meta := printer.NewResponseMeta(200, time.Since(start))
				p.renderMeta(meta)
			}

			return nil
		},
	}

	cmd.Flags().IntVarP(&page, "page", "p", 1, "page number")
	cmd.Flags().IntVarP(&limit, "limit", "l", 15, "results per page (max 100)")
	cmd.Flags().StringVarP(&status, "status", "s", "", "filter by status")
	cmd.Flags().StringVar(&mode, "mode", "normal", "output mode: normal, compact, verbose")
	
	return cmd
}
*/

// ─── Example 2: Get Command with Detail Rendering ──────────────────────

/*
func newServiceGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get <id>",
		Short: "Get details of a service",
		Args:  cobra.ExactArgs(1),
		Example: `  arianet service get 42
  arianet service get 42 --output json
  arianet service get 42 --output yaml`,
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := strconv.Atoi(args[0])
			if err != nil {
				return fmt.Errorf("invalid service ID: %s", args[0])
			}

			client, err := requireAuth()
			if err != nil {
				handleAPIError(err)
				return nil
			}

			s, err := client.GetService(id)
			if err != nil {
				// Use error rendering with suggestions
				if apiErr, ok := err.(*api.APIError); ok {
					p := printer.New(cfg.Output)
					printer.RenderError(p, 404, apiErr.Message, "")
				}
				return nil
			}

			p := printer.New(cfg.Output)
			
			if cfg.Output == printer.FormatJSON {
				p.PrintJSON(s)
				return nil
			}
			if cfg.Output == printer.FormatYAML {
				p.PrintYAML(s)
				return nil
			}

			// Detailed view with expandable sections
			expandable := map[string]bool{
				"networking": true,
				"firewall":   true,
				"monitoring": true,
			}

			details := [][]string{
				{"ID", printer.Bold(fmt.Sprint(s.ID))},
				{"Hostname", s.Hostname},
				{"Status", printer.StatusBadge(s.Status)},
				{"Plan", s.Plan.Name},
				{"Region", s.Datacenter.Name},
				{"OS", s.OS.Name},
				{"IP Address", printer.Cyan(s.IP)},
				{"Billing Cycle", s.BillingCycle},
				{"Created", s.CreatedAt},
				{"networking", printer.Dim("Configure firewalls and networking [+]")},
				{"firewall", printer.Dim("Manage firewall rules [+]")},
				{"monitoring", printer.Dim("View monitoring and alerts [+]")},
			}

			p.DetailExpandable("Service Details", details, expandable)

			// Show progress of any pending operations
			if s.Status == "creating" || s.Status == "pending" {
				progress := 0 // Calculate from API response
				bar := printer.ProgressBar(progress, 100, 20)
				fmt.Fprintf(p.Out, "  %s  %s Operation in progress\n\n",
					printer.LoadingSpinner(0), bar)
			}

			return nil
		},
	}
}
*/

// ─── Example 3: Create Command with Response Confirmation ──────────────

/*
func newServiceCreateCmd() *cobra.Command {
	var (
		hostname string
		plan     string
		region   string
		os       string
	)

	return &cobra.Command{
		Use:   "create",
		Short: "Create a new cloud service",
		Example: `  arianet service create --hostname web-server --plan small --region us-west --os ubuntu22
  arianet service create --hostname api-server --plan medium --region eu-central --os debian11`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Validate inputs
			if hostname == "" || plan == "" || region == "" || os == "" {
				printer.ErrorWithSuggestion(
					"Missing required parameters",
					"All of --hostname, --plan, --region, --os are required",
					"Use 'arianet service create --help' for more information",
				)
				return nil
			}

			client, err := requireAuth()
			if err != nil {
				handleAPIError(err)
				return nil
			}

			// Show progress during creation
			fmt.Printf("  %s  Creating service '%s'...\n",
				printer.LoadingSpinner(0), hostname)

			resp, err := client.CreateService(hostname, plan, region, os)
			if err != nil {
				if apiErr, ok := err.(*api.APIError); ok {
					p := printer.New(cfg.Output)
					printer.RenderError(p, 400, apiErr.Message, "")
				}
				return nil
			}

			// Use helper for creation response
			p := printer.New(cfg.Output)
			printer.RenderCreateResponse(p, "Service", resp.ID, map[string]interface{}{
				"Hostname": hostname,
				"Plan":     plan,
				"Region":   region,
				"OS":       os,
				"Status":   "creating",
			})

			printer.Hint("Use 'arianet service get " + fmt.Sprint(resp.ID) + "' to monitor progress")
			return nil
		},
	}
}
*/

// ─── Example 4: Error Handling with Suggestions ─────────────────────────

/*
func handleAuthError(err error) {
	p := printer.New(printer.FormatTable)
	
	suggestions := []string{
		"Set your API token: arianet auth login",
		"Or use environment variable: export ARIANET_TOKEN=your_token",
		"Generate a token: arianet auth tokens create",
		"List tokens: arianet auth tokens list",
	}
	
	printer.ErrorWithSuggestion(
		"Authentication failed",
		"No valid API token found",
		suggestions...,
	)
}

func handleNetworkError(err error) {
	suggestions := []string{
		"Check your internet connection",
		"Verify API endpoint: arianet configure",
		"Check if the service is operational: arianet status",
	}
	
	printer.ErrorWithSuggestion(
		"Network error",
		fmt.Sprintf("Could not reach API: %v", err),
		suggestions...,
	)
}
*/

// ─── Example 5: Pagination Display ──────────────────────────────────────

/*
func displayListWithPagination(services []Service, pagination *Pagination) {
	p := printer.New(printer.FormatTable)

	if len(services) == 0 {
		printer.EmptyState("No results", "Try different filters or go to the next page")
		return
	}

	// Build and display table
	rows := buildServiceRows(services)
	p.Table(headers, rows)

	// Show pagination controls
	if pagination != nil && pagination.LastPage > 1 {
		printer.PaginationFooter(
			pagination.CurrentPage,
			pagination.LastPage,
			pagination.Total,
			"services",
		)
		
		// Show keyboard hints
		printer.Hint("Use --page flag to navigate: arianet service list --page 2")
	}
}
*/

// ─── Example 6: Stats Display ───────────────────────────────────────────

/*
func displayStats(stats api.AccountStats) {
	p := printer.New(printer.FormatTable)

	// Create summary display
	printer.RenderSummary(p, "Account Overview", map[string]interface{}{
		"Total Services":  stats.TotalServices,
		"Active":          stats.ActiveServices,
		"Pending":         stats.PendingServices,
		"Failed":          stats.FailedServices,
		"Total Bandwidth": stats.BandwidthUsed + " GB",
		"CPU Usage":       fmt.Sprintf("%.1f%%", stats.CPUUsage),
	})

	// Or use the stats rendering helper
	printer.RenderStats(p, []struct{
		Label string
		Value interface{}
		Icon  string
	}{
		{"Active Services", stats.ActiveServices, "●"},
		{"Pending Services", stats.PendingServices, "◐"},
		{"Failed Services", stats.FailedServices, "○"},
		{"Storage Used", stats.StorageUsed + " GB", "💾"},
	})
}
*/

// ─── Example 7: Async Operations with Progress ────────────────────────

/*
func displayAsyncOperation(operationID string, status string, progress int) {
	fmt.Printf("\r")
	if status == "pending" {
		fmt.Printf("  %s  Processing... ", printer.LoadingSpinner(int(time.Now().Unix())))
	} else if status == "running" {
		bar := printer.ProgressBar(progress, 100, 20)
		fmt.Printf("  %s  %s", printer.LoadingSpinner(int(time.Now().Unix())), bar)
	}
}
*/

// Common Patterns:
// 1. Always show empty state when no results
// 2. Use status badges for status fields
// 3. Dim optional/null values
// 4. Show timing for verbose/debug mode
// 5. Provide actionable error suggestions
// 6. Use progress bars for long operations
// 7. Support multiple output formats
// 8. Use table for defaults, allow JSON/YAML/compact options
