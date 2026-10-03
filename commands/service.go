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
		newServiceActionsCmd(),
		newServiceRestartCmd(),
		newServicePowerOnCmd(),
		newServicePowerOffCmd(),
		newServiceRenameCmd(),
		newServiceReinstallCmd(),
		newServiceProtectCmd(),
	)
	return cmd
}

func jsonMode() bool { return cfg != nil && cfg.Output == printer.FormatJSON }

func parseServerID(arg string) (int, error) {
	id, err := strconv.Atoi(arg)
	if err != nil || id <= 0 {
		return 0, fmt.Errorf("invalid server ID: %s", arg)
	}
	return id, nil
}

// reportAction prints the answer to an accepted operation.
func reportAction(res *api.ActionResult, fallback string) {
	if jsonMode() {
		printer.New(cfg.Output).PrintJSON(res)
		return
	}
	msg := res.Message
	if msg == "" {
		msg = fallback
	}
	printer.Success(msg)
}

// addWaitFlags registers --wait and --timeout on an operation command.
func addWaitFlags(cmd *cobra.Command, wait *bool, timeout *time.Duration) {
	cmd.Flags().BoolVar(wait, "wait", false, "wait until the operation has finished")
	cmd.Flags().DurationVar(timeout, "timeout", defaultWaitTimeout, "longest time to wait with --wait")
}

func maybeWait(client *api.Client, id int, spec waitSpec, wait bool, timeout time.Duration) {
	if !wait {
		if !jsonMode() {
			printer.Info(fmt.Sprintf("Track progress: arianet server status %d   (or add --wait)", id))
		}
		return
	}
	finishWait(id, spec, waitForServer(client, id, spec, timeout), timeout)
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
		Long: `List your cloud servers.

Without --status, active and suspended servers are shown. Other statuses
include creating, pending, failed, terminated, powering_on, powering_off,
restarting and reinstalling_os.`,
		Example: `  arianet server list
  arianet server list --status active
  arianet server list --status terminated
  arianet server list --page 2 --limit 20
  arianet server list --output json`,
		RunE: func(cmd *cobra.Command, args []string) error {
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

			p := printer.New(cfg.Output)
			if jsonMode() {
				p.PrintJSON(services)
				return nil
			}

			if len(services) == 0 {
				if status == "" {
					printer.EmptyState(
						"No active or suspended servers found",
						"Use --status to view other servers or create one with: arianet server create",
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

				providerStatus := strings.ToLower(derefStr(s.InstanceStatus))
				if providerStatus == "" {
					providerStatus = printer.Dim("-")
				}

				protected := printer.Dim("-")
				if derefBool(s.Protected) {
					protected = printer.BoolCheck(true)
				}

				planName, dcName, osName := "", "", ""
				if s.Plan != nil {
					planName = s.Plan.Name
				}
				if s.Datacenter != nil {
					dcName = s.Datacenter.Name
				}
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
				printer.PaginationFooter(pagination.CurrentPage, pagination.LastPage, int(pagination.Total), "servers")
			}
			return nil
		},
	}

	cmd.Flags().IntVarP(&page, "page", "p", 1, "page number")
	cmd.Flags().IntVarP(&limit, "limit", "l", 15, "results per page (max 100)")
	cmd.Flags().StringVarP(&status, "status", "s", "", "filter by status (default: active and suspended)")
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
			id, err := parseServerID(args[0])
			if err != nil {
				return err
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
			if jsonMode() {
				p.PrintJSON(s)
				return nil
			}

			ip := s.PrimaryIP()
			if ip == "" {
				ip = printer.Dim("-")
			}

			planStr, dcStr, osStr := "-", "-", "-"
			if s.Plan != nil {
				planStr = fmt.Sprintf("%s (ID: %d)", s.Plan.Name, s.Plan.ID)
			}
			if s.Datacenter != nil {
				dcStr = fmt.Sprintf("%s (ID: %d)", s.Datacenter.Name, s.Datacenter.ID)
			}
			if s.OS != nil {
				osStr = fmt.Sprintf("%s (ID: %d)", s.OS.Name, s.OS.ID)
			}

			p.Table(
				[]string{"Field", "Value"},
				[][]string{
					{"ID", strconv.Itoa(s.ID)},
					{"Hostname", ptrOrDash(s.Hostname)},
					{"Status", printer.StatusColor(s.Status)},
					{"Provider Status", strings.ToLower(ptrOrDash(s.InstanceStatus))},
					{"Protected", printer.BoolCheck(derefBool(s.Protected))},
					{"Billing Cycle", ptrOrDash(s.Cycle)},
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
		planID         int
		datacenterID   int
		osID           int
		hostname       string
		sshKeyID       int
		password       string
		currencyID     int
		idempotencyKey string
		yes            bool
		noWait         bool
		timeout        time.Duration
	)

	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a new cloud server",
		Long: `Create a new cloud server.

If --plan, --region or --os is omitted, an interactive wizard guides you
through the choice.

Authentication: pass --ssh-key <id> to log in with a stored SSH key, or
--password to set the root password yourself. With neither, a strong random
root password is generated and printed once - save it.

The command waits until the server is active (up to --timeout). Use
--no-wait to return as soon as the order is accepted.

Every order carries an idempotency key, so a retry after a network failure can
never buy a second server. If the command is interrupted before it can tell
you the outcome, re-run it with the same --idempotency-key it printed.`,
		Example: `  arianet server create                                   # interactive wizard
  arianet server create --plan 5 --region 1 --os 3 --hostname web-01
  arianet server create --plan 5 --region 1 --os 3 --hostname web-01 --ssh-key 2
  arianet server create --plan 5 --region 1 --os 3 --no-wait --yes --output json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if sshKeyID > 0 && password != "" {
				return fmt.Errorf("use either --ssh-key or --password, not both")
			}
			if idempotencyKey != "" && !validIdempotencyKey(idempotencyKey) {
				return fmt.Errorf("--idempotency-key must be 8-128 characters of letters, digits and _-:.")
			}

			client, err := requireAuth()
			if err != nil {
				handleAPIError(err)
				return nil
			}

			if planID == 0 || datacenterID == 0 || osID == 0 {
				if jsonMode() {
					return fmt.Errorf("--plan, --region and --os are required with --output json")
				}
				choice, err := runServerCreateWizard(client, datacenterID, planID, osID, hostname, sshKeyID, password != "")
				if err != nil {
					printer.Error(err.Error())
					os.Exit(1)
				}
				datacenterID, planID, osID, hostname, sshKeyID = choice.datacenterID, choice.planID, choice.osID, choice.hostname, choice.sshKeyID
			}

			if !yes && !jsonMode() {
				name := hostname
				if name == "" {
					name = "(no hostname)"
				}
				if !confirm(fmt.Sprintf("\nCreate server %s with plan #%d in region #%d? Billing starts immediately", name, planID, datacenterID)) {
					printer.Info("Cancelled.")
					return nil
				}
			}

			req := api.CreateServiceRequest{
				PlanID:       planID,
				DatacenterID: datacenterID,
				OsID:         osID,
				Hostname:     hostname,
				CurrencyID:   optIntPtr(currencyID),
			}

			generated := ""
			if sshKeyID > 0 {
				req.AuthType = "ssh"
				req.SSHKeyID = &sshKeyID
			} else {
				req.AuthType = "password"
				if password == "" {
					password, err = generatePassword(20)
					if err != nil {
						return fmt.Errorf("could not generate a password: %w", err)
					}
					generated = password
				}
				req.AuthValue = password
			}

			if idempotencyKey == "" {
				idempotencyKey = newIdempotencyKey()
			}

			result, err := client.CreateService(req, idempotencyKey)
			if err != nil {
				if outcomeUnknown(err) {
					fmt.Fprintf(os.Stderr, "\nThe outcome of the order is unknown. Re-run the same command with\n  --idempotency-key %s\nto find out safely: it returns the server if it was created, and never creates a second one.\n", idempotencyKey)
				}
				handleAPIError(err)
				return nil
			}

			created := result.Server
			if created.RootPassword == "" && generated != "" {
				created.RootPassword = generated
			}

			if jsonMode() {
				printer.New(cfg.Output).PrintJSON(created)
			} else {
				printCreated(created, result.Replayed, req.AuthType == "password" && generated != "")
			}

			if noWait {
				if !jsonMode() {
					printer.Info(fmt.Sprintf("Track progress: arianet server status %d", created.ID))
				}
				return nil
			}
			finishWait(created.ID, waitProvisioned(), waitForServer(client, created.ID, waitProvisioned(), timeout), timeout)
			if !jsonMode() {
				printer.Info(fmt.Sprintf("Details: arianet server get %d", created.ID))
			}
			return nil
		},
	}

	cmd.Flags().IntVar(&planID, "plan", 0, "plan ID (see: arianet plan list)")
	cmd.Flags().IntVar(&datacenterID, "region", 0, "region ID (see: arianet region list)")
	cmd.Flags().IntVar(&osID, "os", 0, "OS template ID (see: arianet os list)")
	cmd.Flags().StringVar(&hostname, "hostname", "", "hostname for the server")
	cmd.Flags().IntVar(&sshKeyID, "ssh-key", 0, "log in with this SSH key ID instead of a password (see: arianet ssh list)")
	cmd.Flags().StringVar(&password, "password", "", "root password (default: a random one is generated and shown once)")
	cmd.Flags().IntVar(&currencyID, "currency", 0, "currency ID to pay with (default: your default wallet)")
	cmd.Flags().StringVar(&idempotencyKey, "idempotency-key", "", "reuse a key to retry an order safely")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "skip confirmation prompt")
	cmd.Flags().BoolVar(&noWait, "no-wait", false, "return as soon as the order is accepted")
	cmd.Flags().BoolVar(&noWait, "no-watch", false, "alias of --no-wait")
	_ = cmd.Flags().MarkHidden("no-watch")
	cmd.Flags().DurationVar(&timeout, "timeout", defaultWaitTimeout, "longest time to wait for the server to become active")

	return cmd
}

func validIdempotencyKey(k string) bool {
	if len(k) < 8 || len(k) > 128 {
		return false
	}
	for _, r := range k {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case strings.ContainsRune("_-:.", r):
		default:
			return false
		}
	}
	return true
}

// outcomeUnknown reports whether an order may or may not have been placed.
func outcomeUnknown(err error) bool {
	apiErr, ok := err.(*api.APIError)
	if !ok {
		return true
	}
	return apiErr.Status >= 500 || apiErr.Status == 409
}

func printCreated(s *api.CreatedService, replayed, showPassword bool) {
	if replayed {
		printer.Info("This is the result of an earlier order with the same idempotency key; no second server was created.")
	}
	name := derefStr(s.Name)
	if name == "" {
		name = fmt.Sprintf("#%d", s.ID)
	}
	printer.Success(fmt.Sprintf("Server %s ordered (ID: %d, status: %s)", name, s.ID, s.Status))

	rows := [][]string{{"ID", strconv.Itoa(s.ID)}, {"Status", s.Status}}
	if s.Plan != nil && s.Plan.Name != nil {
		rows = append(rows, []string{"Plan", *s.Plan.Name})
	}
	if s.OS != nil && s.OS.Name != nil {
		rows = append(rows, []string{"OS", *s.OS.Name})
	}
	for _, ip := range s.IPAddresses {
		rows = append(rows, []string{"IP (" + ip.Type + ")", ip.IP})
	}
	printer.New(cfg.Output).Table([]string{"Field", "Value"}, rows)

	if s.RootPassword != "" && showPassword {
		fmt.Printf("\n  Root password: %s\n", s.RootPassword)
		printer.Warn("Save this password now. It is not shown again.")
	}
}

type createChoice struct {
	datacenterID, planID, osID int
	hostname                   string
	sshKeyID                   int
}

var errNoInput = fmt.Errorf("no input available; pass --plan, --region and --os instead of using the wizard")

// runServerCreateWizard interactively collects the missing order parameters.
func runServerCreateWizard(client *api.Client, datacenterID, planID, osID int, hostname string, sshKeyID int, passwordSet bool) (createChoice, error) {
	reader := bufio.NewReader(os.Stdin)
	c := createChoice{datacenterID: datacenterID, planID: planID, osID: osID, hostname: hostname, sshKeyID: sshKeyID}

	fmt.Println()
	fmt.Println("  Arianet Server Creation Wizard")
	fmt.Println("  " + strings.Repeat("-", 38))

	if c.datacenterID == 0 {
		list, err := client.ListDatacenters()
		if err != nil {
			return c, fmt.Errorf("could not fetch regions: %w", err)
		}
		if len(list) == 0 {
			return c, fmt.Errorf("no regions available")
		}

		fmt.Println("\n  Step 1 - Select a region")
		fmt.Println()
		for i, d := range list {
			fmt.Printf("    %2d)  %s  %s (ID: %d)\n", i+1, wizardPad(d.Name, 28), d.Country, d.ID)
		}
		n, err := wizardPromptInt(reader, "\n  Enter number", 1, len(list))
		if err != nil {
			return c, err
		}
		c.datacenterID = list[n-1].ID
		fmt.Printf("  -> Region: %s\n", list[n-1].Name)
	}

	if c.planID == 0 {
		allPlans, err := client.ListPlansByDatacenter(c.datacenterID)
		if err != nil {
			return c, fmt.Errorf("could not fetch plans: %w", err)
		}
		plans := make([]api.Plan, 0, len(allPlans))
		for _, p := range allPlans {
			if strings.TrimSpace(p.Name) != "" {
				plans = append(plans, p)
			}
		}
		if len(plans) == 0 {
			return c, fmt.Errorf("no plans available for this region")
		}

		fmt.Println("\n  Step 2 - Select a plan")
		fmt.Println()
		for i, p := range plans {
			price := ""
			if len(p.Prices) > 0 && p.Prices[0].Monthly != "" {
				price = fmt.Sprintf("  %s %s/mo", p.Prices[0].Monthly, p.Prices[0].Code)
			}
			rec := ""
			if p.Recommended {
				rec = "  [recommended]"
			}
			fmt.Printf("    %2d)  %s (ID: %d)%s%s\n", i+1, wizardPad(p.Name, 28), p.ID, price, rec)
		}
		n, err := wizardPromptInt(reader, "\n  Enter number", 1, len(plans))
		if err != nil {
			return c, err
		}
		c.planID = plans[n-1].ID
		fmt.Printf("  -> Plan: %s\n", plans[n-1].Name)
	}

	if c.osID == 0 {
		images, err := client.ListOSByDatacenter(c.datacenterID)
		if err != nil {
			return c, fmt.Errorf("could not fetch OS templates: %w", err)
		}
		if len(images) == 0 {
			return c, fmt.Errorf("no OS templates available for this region")
		}

		fmt.Println("\n  Step 3 - Select an operating system")
		fmt.Println()
		for i, t := range images {
			fmt.Printf("    %2d)  %s  (ID: %d)\n", i+1, wizardPad(t.Name, 34), t.ID)
		}
		n, err := wizardPromptInt(reader, "\n  Enter number", 1, len(images))
		if err != nil {
			return c, err
		}
		c.osID = images[n-1].ID
		fmt.Printf("  -> OS: %s\n", images[n-1].Name)
	}

	if c.hostname == "" {
		fmt.Println("\n  Step 4 - Hostname (leave empty for an automatic name)")
		fmt.Print("\n  Enter hostname: ")
		line, err := reader.ReadString('\n')
		if err != nil && strings.TrimSpace(line) == "" {
			return c, errNoInput
		}
		c.hostname = strings.TrimSpace(line)
		if c.hostname != "" {
			fmt.Printf("  -> Hostname: %s\n", c.hostname)
		}
	}

	if c.sshKeyID == 0 && !passwordSet {
		keys, _, err := client.ListSSHKeys(1, 100)
		if err == nil {
			var usable []api.SSHKey
			for _, k := range keys {
				if k.DatacenterID != nil && *k.DatacenterID == c.datacenterID {
					usable = append(usable, k)
				}
			}
			if len(usable) > 0 {
				fmt.Println("\n  Step 5 - Login method")
				fmt.Println()
				fmt.Printf("    %2d)  Generated root password\n", 1)
				for i, k := range usable {
					fmt.Printf("    %2d)  SSH key %s (ID: %d)\n", i+2, derefStr(k.Name), k.ID)
				}
				n, err := wizardPromptInt(reader, "\n  Enter number", 1, len(usable)+1)
				if err != nil {
					return c, err
				}
				if n > 1 {
					c.sshKeyID = usable[n-2].ID
					fmt.Printf("  -> SSH key: %s\n", derefStr(usable[n-2].Name))
				}
			}
		}
	}

	fmt.Println()
	return c, nil
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
func wizardPromptInt(reader *bufio.Reader, label string, min, max int) (int, error) {
	for {
		fmt.Printf("%s [%d-%d]: ", label, min, max)
		line, err := reader.ReadString('\n')
		input := strings.TrimSpace(line)
		if err != nil && input == "" {
			return 0, errNoInput
		}
		n, convErr := strconv.Atoi(input)
		if convErr == nil && n >= min && n <= max {
			return n, nil
		}
		fmt.Printf("  Invalid choice. Please enter a number between %d and %d.\n", min, max)
	}
}

func newServiceDeleteCmd() *cobra.Command {
	var (
		yes     bool
		wait    bool
		timeout time.Duration
	)

	cmd := &cobra.Command{
		Use:   "delete <id>",
		Short: "Delete a server",
		Args:  cobra.ExactArgs(1),
		Example: `  arianet server delete 42
  arianet server delete 42 --yes --wait`,
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := parseServerID(args[0])
			if err != nil {
				return err
			}

			if !yes {
				if !confirm(fmt.Sprintf("Delete server #%d? This cannot be undone", id)) {
					printer.Info("Cancelled.")
					return nil
				}
			}

			client, err := requireAuth()
			if err != nil {
				handleAPIError(err)
				return nil
			}

			res, err := client.DeleteService(id)
			if err != nil {
				handleAPIError(err)
				return nil
			}

			reportAction(res, fmt.Sprintf("Server #%d deletion started.", id))
			maybeWait(client, id, waitDeleted(), wait, timeout)
			return nil
		},
	}

	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "skip confirmation prompt")
	addWaitFlags(cmd, &wait, &timeout)
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
			id, err := parseServerID(args[0])
			if err != nil {
				return err
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
			if jsonMode() {
				p.PrintJSON(status)
				return nil
			}

			p.Table(
				[]string{"Field", "Value"},
				[][]string{
					{"Status", printer.StatusColor(status.Status)},
					{"Provider Status", strings.ToLower(ptrOrDash(status.InstanceStatus))},
				},
			)
			return nil
		},
	}
}

func newServiceActionsCmd() *cobra.Command {
	var limit int

	cmd := &cobra.Command{
		Use:   "actions <id>",
		Short: "Show the operation history of a server",
		Long: `Show the operations performed on a server (create, restart, reinstall, ...),
newest first, with their outcome.`,
		Args: cobra.ExactArgs(1),
		Example: `  arianet server actions 42
  arianet server actions 42 --limit 50 --output json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := parseServerID(args[0])
			if err != nil {
				return err
			}

			client, err := requireAuth()
			if err != nil {
				handleAPIError(err)
				return nil
			}

			history, err := client.ListServiceActions(id, limit)
			if err != nil {
				handleAPIError(err)
				return nil
			}

			p := printer.New(cfg.Output)
			if jsonMode() {
				p.PrintJSON(history)
				return nil
			}

			if len(history.Actions) == 0 {
				printer.Info("No operations recorded for this server.")
				return nil
			}

			rows := make([][]string, len(history.Actions))
			for i, a := range history.Actions {
				rows[i] = []string{a.Type, a.Status, ptrOrDash(a.StartedAt), ptrOrDash(a.FinishedAt), a.ID.String()}
			}
			p.Table([]string{"Operation", "Status", "Started", "Finished", "ID"}, rows)
			return nil
		},
	}

	cmd.Flags().IntVarP(&limit, "limit", "l", 20, "number of operations to show")
	return cmd
}

func newServiceRestartCmd() *cobra.Command {
	var (
		yes     bool
		wait    bool
		timeout time.Duration
	)

	cmd := &cobra.Command{
		Use:   "restart <id>",
		Short: "Restart a server",
		Args:  cobra.ExactArgs(1),
		Example: `  arianet server restart 42
  arianet server restart 42 --yes --wait`,
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := parseServerID(args[0])
			if err != nil {
				return err
			}

			if !yes {
				if !confirm(fmt.Sprintf("Restart server #%d?", id)) {
					printer.Info("Cancelled.")
					return nil
				}
			}

			client, err := requireAuth()
			if err != nil {
				handleAPIError(err)
				return nil
			}

			res, err := client.RestartService(id)
			if err != nil {
				handleAPIError(err)
				return nil
			}

			reportAction(res, fmt.Sprintf("Server #%d is restarting.", id))
			maybeWait(client, id, waitRestarted(), wait, timeout)
			return nil
		},
	}

	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "skip confirmation prompt")
	addWaitFlags(cmd, &wait, &timeout)
	return cmd
}

func newServicePowerOnCmd() *cobra.Command {
	var (
		wait    bool
		timeout time.Duration
	)

	cmd := &cobra.Command{
		Use:     "power-on <id>",
		Short:   "Power on a server",
		Args:    cobra.ExactArgs(1),
		Example: `  arianet server power-on 42 --wait`,
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := parseServerID(args[0])
			if err != nil {
				return err
			}

			client, err := requireAuth()
			if err != nil {
				handleAPIError(err)
				return nil
			}

			res, err := client.PowerOnService(id)
			if err != nil {
				handleAPIError(err)
				return nil
			}

			reportAction(res, fmt.Sprintf("Server #%d is powering on.", id))
			maybeWait(client, id, waitPoweredOn(), wait, timeout)
			return nil
		},
	}

	addWaitFlags(cmd, &wait, &timeout)
	return cmd
}

func newServicePowerOffCmd() *cobra.Command {
	var (
		yes     bool
		wait    bool
		timeout time.Duration
	)

	cmd := &cobra.Command{
		Use:     "power-off <id>",
		Short:   "Power off a server",
		Args:    cobra.ExactArgs(1),
		Example: `  arianet server power-off 42 --yes --wait`,
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := parseServerID(args[0])
			if err != nil {
				return err
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

			res, err := client.PowerOffService(id)
			if err != nil {
				handleAPIError(err)
				return nil
			}

			reportAction(res, fmt.Sprintf("Server #%d is powering off.", id))
			maybeWait(client, id, waitPoweredOff(), wait, timeout)
			return nil
		},
	}

	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "skip confirmation prompt")
	addWaitFlags(cmd, &wait, &timeout)
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
			id, err := parseServerID(args[0])
			if err != nil {
				return err
			}
			if n := len([]rune(name)); n < 1 || n > 30 {
				return fmt.Errorf("--name must be 1-30 characters")
			}

			client, err := requireAuth()
			if err != nil {
				handleAPIError(err)
				return nil
			}

			res, err := client.RenameService(id, name)
			if err != nil {
				handleAPIError(err)
				return nil
			}

			reportAction(res, fmt.Sprintf("Server #%d renamed to %q.", id, name))
			return nil
		},
	}

	cmd.Flags().StringVar(&name, "name", "", "new name (1-30 characters)")
	_ = cmd.MarkFlagRequired("name")
	return cmd
}

func newServiceReinstallCmd() *cobra.Command {
	var (
		osID    int
		yes     bool
		wait    bool
		timeout time.Duration
	)

	cmd := &cobra.Command{
		Use:   "reinstall <id>",
		Short: "Reinstall the OS on a server",
		Args:  cobra.ExactArgs(1),
		Example: `  arianet server reinstall 42 --os 3
  arianet server reinstall 42 --os 3 --yes --wait`,
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := parseServerID(args[0])
			if err != nil {
				return err
			}

			if !yes {
				printer.Warn("Reinstalling erases all data on the server.")
				if !confirm(fmt.Sprintf("Reinstall the OS on server #%d?", id)) {
					printer.Info("Cancelled.")
					return nil
				}
			}

			client, err := requireAuth()
			if err != nil {
				handleAPIError(err)
				return nil
			}

			res, err := client.ReinstallService(id, osID)
			if err != nil {
				handleAPIError(err)
				return nil
			}

			reportAction(res, fmt.Sprintf("Server #%d reinstall started.", id))
			maybeWait(client, id, waitReinstalled(), wait, timeout)
			return nil
		},
	}

	cmd.Flags().IntVar(&osID, "os", 0, "OS template ID (see: arianet os list)")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "skip confirmation prompt")
	addWaitFlags(cmd, &wait, &timeout)
	_ = cmd.MarkFlagRequired("os")
	return cmd
}

func newServiceProtectCmd() *cobra.Command {
	var enable, disable bool

	cmd := &cobra.Command{
		Use:   "toggle-protection <id>",
		Short: "Turn deletion protection on or off",
		Long: `Turn deletion protection on or off.

With --enable or --disable the state is set explicitly (safe to repeat in
scripts). With neither, the current state is flipped.`,
		Args: cobra.ExactArgs(1),
		Example: `  arianet server toggle-protection 42 --enable
  arianet server toggle-protection 42 --disable
  arianet server toggle-protection 42`,
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := parseServerID(args[0])
			if err != nil {
				return err
			}
			if enable && disable {
				return fmt.Errorf("use either --enable or --disable, not both")
			}

			client, err := requireAuth()
			if err != nil {
				handleAPIError(err)
				return nil
			}

			var res *api.ActionResult
			switch {
			case enable:
				res, err = client.SetServiceProtection(id, true)
			case disable:
				res, err = client.SetServiceProtection(id, false)
			default:
				res, err = client.ToggleServiceProtection(id)
			}
			if err != nil {
				handleAPIError(err)
				return nil
			}

			state := "toggled"
			if res.Protected != nil {
				state = map[bool]string{true: "enabled", false: "disabled"}[*res.Protected]
			}
			reportAction(res, fmt.Sprintf("Server #%d deletion protection %s.", id, state))
			return nil
		},
	}

	cmd.Flags().BoolVar(&enable, "enable", false, "turn protection on")
	cmd.Flags().BoolVar(&disable, "disable", false, "turn protection off")
	return cmd
}
