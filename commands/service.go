package commands

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/arianet/arianet-cli/internal/api"
	"github.com/arianet/arianet-cli/internal/printer"
	"github.com/mattn/go-runewidth"
	"github.com/spf13/cobra"
)

func newServerCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "server",
		Aliases: []string{"servers", "srv"},
		Short:   "Manage cloud servers",
	}

	cmd.AddCommand(
		newServiceListCmd(),
		newServiceGetCmd(),
		newServiceCreateCmd(),
		newServiceDeleteCmd(),
		newServiceStatusCmd(),
		newServiceRestartCmd(),
		newServicePowerOnCmd(),
		newServicePowerOffCmd(),
		newServiceRenameCmd(),
		newServiceReinstallCmd(),
		newServiceProtectCmd(),
	)
	return cmd
}

func newServiceListCmd() *cobra.Command {
	var (
		page   int
		limit  int
		status string
	)

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List your cloud servers",
		Example: `  arianet server list
  arianet server list --status active
  arianet server list --status suspended
  arianet server list --page 2 --limit 20
  arianet server list --output json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := requireAuth()
			if err != nil {
				handleAPIError(err)
				return nil
			}

			// Get all servers from API (no status filter on API side)
			services, pagination, err := client.ListServices(page, limit, "")
			if err != nil {
				handleAPIError(err)
				return nil
			}

			// Client-side filtering: default shows only active and suspended
			if status == "" {
				filtered := make([]api.Service, 0)
				for _, s := range services {
					if s.Status == "active" || s.Status == "suspended" {
						filtered = append(filtered, s)
					}
				}
				services = filtered
			} else if status != "" {
				// Filter to specific status if provided
				filtered := make([]api.Service, 0)
				for _, s := range services {
					if s.Status == status {
						filtered = append(filtered, s)
					}
				}
				services = filtered
			}

			p := printer.New(cfg.Output)
			if cfg.Output == printer.FormatJSON {
				p.PrintJSON(services)
				return nil
			}

			if len(services) == 0 {
				if status == "" {
					printer.EmptyState(
						"No active or suspended servers found",
						"Use --status to view all servers or create a new one with: arianet server create",
					)
				} else {
					printer.EmptyState(
						fmt.Sprintf("No %s servers found", status),
						"Try a different status or create a new server with: arianet server create",
					)
				}
				return nil
			}

			rows := make([][]string, len(services))
			for i, s := range services {
				ip := s.PrimaryIP()
				if ip == "" {
					ip = printer.Dim("pending")
				}

				hostname := derefStr(s.Hostname)
				if hostname == "" {
					hostname = printer.Dim(fmt.Sprintf("(ID: %d)", s.ID))
				}

				providerStatus := derefStr(s.InstanceStatus)
				if providerStatus == "" {
					providerStatus = printer.Dim("-")
				}

				protected := printer.Dim("-")
				if derefBool(s.Protected) {
					protected = printer.BoolCheck(true)
				}

				planName := ""
				if s.Plan != nil {
					planName = s.Plan.Name
				}
				dcName := ""
				if s.Datacenter != nil {
					dcName = s.Datacenter.Name
				}
				osName := ""
				if s.OS != nil {
					osName = s.OS.Name
				}

				rows[i] = []string{
					strconv.Itoa(s.ID),
					hostname,
					printer.StatusBadge(s.Status),
					providerStatus,
					ip,
					planName,
					dcName,
					osName,
					protected,
				}
			}

			p.TableAdvanced(
				"Cloud Servers",
				[]string{"ID", "Hostname", "Status", "Provider", "IP Address", "Plan", "Region", "OS", "Protected"},
				rows,
			)

			if pagination != nil && pagination.LastPage > 1 {
				printer.PaginationFooter(
					pagination.CurrentPage,
					pagination.LastPage,
					int(pagination.Total),
					"servers",
				)
			}
			return nil
		},
	}

	cmd.Flags().IntVarP(&page, "page", "p", 1, "page number")
	cmd.Flags().IntVarP(&limit, "limit", "l", 15, "results per page (max 100)")
	cmd.Flags().StringVarP(&status, "status", "s", "", "filter by status (active, suspended) - default: active,suspended")
	return cmd
}

func newServiceGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get <id>",
		Short: "Get details of a server",
		Args:  cobra.ExactArgs(1),
		Example: `  arianet server get 42
  arianet server get 42 --output json`,
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
				handleAPIError(err)
				return nil
			}

			p := printer.New(cfg.Output)
			if cfg.Output == printer.FormatJSON {
				p.PrintJSON(s)
				return nil
			}

			ip := s.PrimaryIP()
			if ip == "" {
				ip = printer.Dim("—")
			}

			planStr := "-"
			if s.Plan != nil {
				planStr = fmt.Sprintf("%s (ID: %d)", s.Plan.Name, s.Plan.ID)
			}
			dcStr := "-"
			if s.Datacenter != nil {
				dcStr = fmt.Sprintf("%s (ID: %d)", s.Datacenter.Name, s.Datacenter.ID)
			}
			osStr := "-"
			if s.OS != nil {
				osStr = fmt.Sprintf("%s (ID: %d)", s.OS.Name, s.OS.ID)
			}

			p.Table(
				[]string{"Field", "Value"},
				[][]string{
					{"ID", strconv.Itoa(s.ID)},
					{"Hostname", derefStr(s.Hostname)},
					{"Status", printer.StatusColor(s.Status)},
					{"Protected", printer.BoolCheck(derefBool(s.Protected))},
					{"Billing Cycle", derefStr(s.Cycle)},
					{"IP Address", ip},
					{"Plan", planStr},
					{"Region", dcStr},
					{"OS", osStr},
					{"Created At", s.CreatedAt},
				},
			)
			return nil
		},
	}
}

func newServiceCreateCmd() *cobra.Command {
	var (
		planID       int
		datacenterID int
		osID         int
		hostname     string
		yes          bool
		noWatch      bool
	)

	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a new cloud server",
		Long: `Create a new cloud server.

If required flags are omitted, an interactive wizard will guide you through
selecting a region, plan, OS template, and hostname.`,
		Example: `  arianet server create                                          # interactive wizard
  arianet server create --plan 5 --region 1 --os 3 --hostname web-01
  arianet server create --plan 5 --region 1 --os 3 --hostname web-01 --ssh-key 2
  arianet server create --plan 5 --region 1 --os 3 --hostname web-01 --no-watch`,
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := requireAuth()
			if err != nil {
				handleAPIError(err)
				return nil
			}

			// Interactive wizard when required flags are missing
			if planID == 0 || datacenterID == 0 || osID == 0 || hostname == "" {
				datacenterID, planID, osID, hostname, err = runServerCreateWizard(
					client, datacenterID, planID, osID, hostname,
				)
				if err != nil {
					printer.Error(err.Error())
					return nil
				}
			}

			// Confirm
			if !yes {
				fmt.Printf("\nCreate server %q with plan #%d in region #%d? [y/N]: ", hostname, planID, datacenterID)
				reader := bufio.NewReader(os.Stdin)
				answer, _ := reader.ReadString('\n')
				answer = strings.TrimSpace(strings.ToLower(answer))
				if answer != "y" && answer != "yes" {
					printer.Info("Cancelled.")
					return nil
				}
			}

			req := api.CreateServiceRequest{
				PlanID:       planID,
				DatacenterID: datacenterID,
				OsID:         osID,
				Hostname:     hostname,
			}

			created, err := client.CreateService(req)
			if err != nil {
				handleAPIError(err)
				return nil
			}

			printer.Success(fmt.Sprintf("Server %q created (ID: %d)", hostname, created.ID))

			if !noWatch {
				watchServerProvisioning(client, created.ID)
			} else {
				printer.Info(fmt.Sprintf("Track progress: arianet server status %d", created.ID))
			}
			return nil
		},
	}

	cmd.Flags().IntVar(&planID, "plan", 0, "plan ID (see: arianet plan list)")
	cmd.Flags().IntVar(&datacenterID, "region", 0, "datacenter/region ID (see: arianet region list)")
	cmd.Flags().IntVar(&osID, "os", 0, "OS template ID (see: arianet os list)")
	cmd.Flags().StringVar(&hostname, "hostname", "", "hostname for the server")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "skip confirmation prompt")
	cmd.Flags().BoolVar(&noWatch, "no-watch", false, "skip status monitoring after creation")

	return cmd
}

// runServerCreateWizard interactively collects missing creation parameters.
func runServerCreateWizard(client *api.Client, datacenterID, planID, osID int, hostname string) (int, int, int, string, error) {
	reader := bufio.NewReader(os.Stdin)

	fmt.Println()
	fmt.Println("  Arianet Server Creation Wizard")
	fmt.Println("  " + strings.Repeat("-", 38))

	// Step 1: Region
	if datacenterID == 0 {
		regions, err := client.ListRegions()
		if err != nil {
			return 0, 0, 0, "", fmt.Errorf("could not fetch regions: %w", err)
		}
		active := regions
		if len(active) == 0 {
			return 0, 0, 0, "", fmt.Errorf("no active regions available")
		}

		fmt.Println("\n  Step 1 of 4 — Select a Region")
		fmt.Println()
		for i, r := range active {
			fmt.Printf("    %2d)  %s  (ID: %d)\n", i+1, wizardPad(derefStr(r.DisplayName), 28), r.ID)
		}
		choice := wizardPromptInt(reader, "\n  Enter number", 1, len(active))
		datacenterID = active[choice-1].ID
		fmt.Printf("  -> Region: %s\n", derefStr(active[choice-1].DisplayName))
	}

	// Step 2: Plan
	if planID == 0 {
		allPlans, err := client.ListPlansByDatacenter(datacenterID)
		if err != nil {
			return 0, 0, 0, "", fmt.Errorf("could not fetch plans: %w", err)
		}
		// Filter out plans with no name
		plans := make([]api.Plan, 0, len(allPlans))
		for _, p := range allPlans {
			if strings.TrimSpace(p.Name) != "" {
				plans = append(plans, p)
			}
		}
		if len(plans) == 0 {
			return 0, 0, 0, "", fmt.Errorf("no plans available for this region")
		}

		fmt.Println("\n  Step 2 of 4 — Select a Plan")
		fmt.Println()
		for i, p := range plans {
			price := ""
			if len(p.Prices) > 0 && p.Prices[0].Monthly != nil {
				price = fmt.Sprintf("  %.2f/mo", *p.Prices[0].Monthly)
			}
			rec := ""
			if p.Recommended {
				rec = "  [recommended]"
			}
			fmt.Printf("    %2d)  %s (ID: %d)%s%s\n", i+1, wizardPad(p.Name, 28), p.ID, price, rec)
		}
		choice := wizardPromptInt(reader, "\n  Enter number", 1, len(plans))
		planID = plans[choice-1].ID
		fmt.Printf("  -> Plan: %s\n", plans[choice-1].Name)
	}

	// Step 3: OS
	if osID == 0 {
		templates, err := client.ListOSByDatacenter(datacenterID)
		if err != nil {
			return 0, 0, 0, "", fmt.Errorf("could not fetch OS templates: %w", err)
		}
		active := templates
		if len(active) == 0 {
			return 0, 0, 0, "", fmt.Errorf("no OS templates available")
		}

		fmt.Println("\n  Step 3 of 4 — Select an OS Template")
		fmt.Println()
		for i, t := range active {
			fmt.Printf("    %2d)  %s  (ID: %d)\n", i+1, wizardPad(t.Name, 28), t.ID)
		}
		choice := wizardPromptInt(reader, "\n  Enter number", 1, len(active))
		osID = active[choice-1].ID
		fmt.Printf("  -> OS: %s\n", active[choice-1].Name)
	}

	// Step 4: Hostname
	if hostname == "" {
		fmt.Println("\n  Step 4 of 4 — Hostname")
		for {
			fmt.Print("\n  Enter hostname: ")
			line, _ := reader.ReadString('\n')
			hostname = strings.TrimSpace(line)
			if hostname != "" {
				break
			}
			fmt.Println("  Hostname cannot be empty.")
		}
		fmt.Printf("  -> Hostname: %s\n", hostname)
	}

	fmt.Println()
	return datacenterID, planID, osID, hostname, nil
}

// wizardPad pads s to width visible columns, correctly handling Unicode (Persian, Arabic, etc.).
func wizardPad(s string, width int) string {
	pad := width - runewidth.StringWidth(s)
	if pad < 0 {
		pad = 0
	}
	return s + strings.Repeat(" ", pad)
}

// wizardPromptInt reads a number in [min, max] from the user.
func wizardPromptInt(reader *bufio.Reader, label string, min, max int) int {
	for {
		fmt.Printf("%s [%d-%d]: ", label, min, max)
		line, _ := reader.ReadString('\n')
		n, err := strconv.Atoi(strings.TrimSpace(line))
		if err == nil && n >= min && n <= max {
			return n
		}
		fmt.Printf("  Invalid choice. Please enter a number between %d and %d.\n", min, max)
	}
}

// watchServerProvisioning polls server status every 30s and prints a progress bar.
func watchServerProvisioning(client *api.Client, serverID int) {
	const interval = 30 * time.Second
	const timeout = 15 * time.Minute
	const barWidth = 30

	terminalStates := map[string]bool{
		"active":    true,
		"failed":    true,
		"suspended": true,
	}

	// Progress weight per status (0–barWidth)
	statusProgress := map[string]int{
		"request_creating":  4,
		"creating":          10,
		"pending":           18,
		"request_activate":  22,
		"request_unsuspend": 24,
		"active":            barWidth,
		"failed":            barWidth,
	}

	fmt.Println()
	fmt.Println("  Monitoring provisioning (checking every 30s, Ctrl+C to stop)")
	fmt.Println("  " + strings.Repeat("-", 52))
	fmt.Printf("  %-8s  %-34s  %s\n", "Elapsed", "Progress", "Status")
	fmt.Println("  " + strings.Repeat("-", 52))

	start := time.Now()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	printRow := func(elapsed time.Duration, status string, isErr bool) {
		filled := statusProgress[status]
		if filled == 0 && !isErr {
			// Unknown status — animate based on elapsed time
			sec := int(elapsed.Seconds())
			filled = (sec * barWidth) / int(timeout.Seconds())
			if filled >= barWidth {
				filled = barWidth - 1
			}
		}
		bar := "[" + strings.Repeat("#", filled) + strings.Repeat("-", barWidth-filled) + "]"
		elapsedStr := fmt.Sprintf("%ds", int(elapsed.Seconds()))
		fmt.Printf("  %-8s  %s  %s\n", elapsedStr, bar, status)
	}

	// First poll immediately (no need to wait 30s)
	check := func() (done bool) {
		elapsed := time.Since(start)
		st, err := client.GetServiceStatus(serverID)
		if err != nil {
			printRow(elapsed, "error: "+err.Error(), true)
			return false
		}
		printRow(elapsed, st.Status, false)
		return terminalStates[st.Status]
	}

	if check() {
		printFinalProvisionResult(client, serverID)
		return
	}

	deadline := time.After(timeout)
	for {
		select {
		case <-ticker.C:
			if check() {
				printFinalProvisionResult(client, serverID)
				return
			}
		case <-deadline:
			fmt.Println()
			printer.Info(fmt.Sprintf("Timed out after %s. Check manually: arianet server status %d",
				timeout, serverID))
			return
		}
	}
}

func printFinalProvisionResult(client *api.Client, serverID int) {
	st, err := client.GetServiceStatus(serverID)
	fmt.Println("  " + strings.Repeat("-", 52))
	fmt.Println()
	if err != nil || st.Status != "active" {
		status := "unknown"
		if err == nil {
			status = st.Status
		}
		printer.Error(fmt.Sprintf("Server #%d ended with status: %s", serverID, status))
		printer.Info(fmt.Sprintf("Details: arianet server get %d", serverID))
	} else {
		printer.Success(fmt.Sprintf("Server #%d is online.", serverID))
		printer.Info(fmt.Sprintf("Details: arianet server get %d", serverID))
	}
}

func newServiceDeleteCmd() *cobra.Command {
	var yes bool

	cmd := &cobra.Command{
		Use:   "delete <id>",
		Short: "Delete a server",
		Args:  cobra.ExactArgs(1),
		Example: `  arianet server delete 42
  arianet server delete 42 --yes`,
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := strconv.Atoi(args[0])
			if err != nil {
				return fmt.Errorf("invalid service ID: %s", args[0])
			}

			if !yes {
				if !confirm(fmt.Sprintf("Delete service #%d? This action cannot be undone", id)) {
					printer.Info("Cancelled.")
					return nil
				}
			}

			client, err := requireAuth()
			if err != nil {
				handleAPIError(err)
				return nil
			}

			if err := client.DeleteService(id); err != nil {
				handleAPIError(err)
				return nil
			}

			printer.Success(fmt.Sprintf("Service #%d deleted.", id))
			return nil
		},
	}

	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "skip confirmation prompt")
	return cmd
}

func newServiceStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status <id>",
		Short: "Get the current status of a server",
		Args:  cobra.ExactArgs(1),
		Example: `  arianet server status 42
  arianet server status 42 --output json`,
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

			status, err := client.GetServiceStatus(id)
			if err != nil {
				handleAPIError(err)
				return nil
			}

			p := printer.New(cfg.Output)
			if cfg.Output == printer.FormatJSON {
				p.PrintJSON(status)
				return nil
			}

			p.Table(
				[]string{"Field", "Value"},
				[][]string{
					{"Status", printer.StatusColor(status.Status)},
					{"Instance Status", status.InstanceStatus},
				},
			)
			return nil
		},
	}
}

func newServiceRestartCmd() *cobra.Command {
	var yes bool

	cmd := &cobra.Command{
		Use:   "restart <id>",
		Short: "Restart a server",
		Args:  cobra.ExactArgs(1),
		Example: `  arianet server restart 42
  arianet server restart 42 --yes`,
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := strconv.Atoi(args[0])
			if err != nil {
				return fmt.Errorf("invalid service ID: %s", args[0])
			}

			if !yes {
				if !confirm(fmt.Sprintf("Restart service #%d?", id)) {
					printer.Info("Cancelled.")
					return nil
				}
			}

			client, err := requireAuth()
			if err != nil {
				handleAPIError(err)
				return nil
			}

			if err := client.RestartService(id); err != nil {
				handleAPIError(err)
				return nil
			}

			printer.Success(fmt.Sprintf("Service #%d is restarting.", id))
			return nil
		},
	}

	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "skip confirmation prompt")
	return cmd
}

func newServicePowerOnCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "power-on <id>",
		Short: "Power on a server",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := strconv.Atoi(args[0])
			if err != nil {
				return fmt.Errorf("invalid server ID: %s", args[0])
			}

			client, err := requireAuth()
			if err != nil {
				handleAPIError(err)
				return nil
			}

			if err := client.PowerOnService(id); err != nil {
				handleAPIError(err)
				return nil
			}

			printer.Success(fmt.Sprintf("Server #%d powering on.", id))
			return nil
		},
	}
}

func newServicePowerOffCmd() *cobra.Command {
	var yes bool

	cmd := &cobra.Command{
		Use:   "power-off <id>",
		Short: "Power off a server",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := strconv.Atoi(args[0])
			if err != nil {
				return fmt.Errorf("invalid server ID: %s", args[0])
			}

			if !yes {
				if !confirm(fmt.Sprintf("Power off server #%d?", id)) {
					printer.Info("Cancelled.")
					return nil
				}
			}

			client, err := requireAuth()
			if err != nil {
				handleAPIError(err)
				return nil
			}

			if err := client.PowerOffService(id); err != nil {
				handleAPIError(err)
				return nil
			}

			printer.Success(fmt.Sprintf("Server #%d powering off.", id))
			return nil
		},
	}

	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "skip confirmation prompt")
	return cmd
}

func newServiceRenameCmd() *cobra.Command {
	var name string

	cmd := &cobra.Command{
		Use:     "rename <id>",
		Short:   "Rename a server",
		Args:    cobra.ExactArgs(1),
		Example: `  arianet server rename 42 --name new-hostname`,
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

			if err := client.RenameService(id, name); err != nil {
				handleAPIError(err)
				return nil
			}

			printer.Success(fmt.Sprintf("Server #%d renamed to %q.", id, name))
			return nil
		},
	}

	cmd.Flags().StringVar(&name, "name", "", "new hostname")
	_ = cmd.MarkFlagRequired("name")
	return cmd
}

func newServiceReinstallCmd() *cobra.Command {
	var (
		osID int
		yes  bool
	)

	cmd := &cobra.Command{
		Use:   "reinstall <id>",
		Short: "Reinstall the OS on a server",
		Args:  cobra.ExactArgs(1),
		Example: `  arianet server reinstall 42 --os 3
  arianet server reinstall 42 --os 3 --yes`,
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := strconv.Atoi(args[0])
			if err != nil {
				return fmt.Errorf("invalid service ID: %s", args[0])
			}

			if !yes {
				printer.Warn("Reinstalling will erase all data on the server.")
				if !confirm(fmt.Sprintf("Reinstall OS on server #%d?", id)) {
					printer.Info("Cancelled.")
					return nil
				}
			}

			client, err := requireAuth()
			if err != nil {
				handleAPIError(err)
				return nil
			}

			if err := client.ReinstallService(id, osID); err != nil {
				handleAPIError(err)
				return nil
			}

			printer.Success(fmt.Sprintf("Server #%d OS reinstall queued.", id))
			return nil
		},
	}

	cmd.Flags().IntVar(&osID, "os", 0, "OS template ID (see: arianet os list)")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "skip confirmation prompt")
	_ = cmd.MarkFlagRequired("os")
	return cmd
}

func newServiceProtectCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "toggle-protection <id>",
		Short:   "Toggle deletion protection on a server",
		Args:    cobra.ExactArgs(1),
		Example: `  arianet server toggle-protection 42`,
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

			if err := client.ToggleServiceProtection(id); err != nil {
				handleAPIError(err)
				return nil
			}

			printer.Success(fmt.Sprintf("Server #%d deletion protection toggled.", id))
			return nil
		},
	}
}
