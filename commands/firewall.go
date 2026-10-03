package commands

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/ariaservice/arianet-cli/internal/api"
	"github.com/ariaservice/arianet-cli/internal/printer"
	"github.com/spf13/cobra"
)

func newFirewallCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "firewall",
		Aliases: []string{"firewalls", "fw"},
		Short:   "Manage firewalls",
	}

	cmd.AddCommand(
		newFirewallListCmd(),
		newFirewallGetCmd(),
		newFirewallCreateCmd(),
		newFirewallUpdateCmd(),
		newFirewallDeleteCmd(),
		newFirewallAttachCmd(),
		newFirewallDetachCmd(),
		newFirewallRuleCmd(),
	)
	return cmd
}

func parseFirewallID(arg string) (int, error) {
	id, err := strconv.Atoi(arg)
	if err != nil || id <= 0 {
		return 0, fmt.Errorf("invalid firewall ID: %s", arg)
	}
	return id, nil
}

func regionCell(id *int) string {
	if id == nil {
		return "-"
	}
	return strconv.Itoa(*id)
}

func newFirewallListCmd() *cobra.Command {
	var (
		page  int
		limit int
	)

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List your firewalls",
		Example: `  arianet firewall list
  arianet firewall list --page 2 --limit 20
  arianet firewall list --output json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := requireAuth()
			if err != nil {
				handleAPIError(err)
				return nil
			}

			firewalls, pagination, err := client.ListFirewalls(page, limit)
			if err != nil {
				handleAPIError(err)
				return nil
			}

			p := printer.New(cfg.Output)
			if jsonMode() {
				p.PrintJSON(firewalls)
				return nil
			}

			if len(firewalls) == 0 {
				printer.Info("No firewalls found.")
				printer.Info("Create one with: arianet firewall create --name web-fw --region 1")
				return nil
			}

			rows := make([][]string, len(firewalls))
			for i, fw := range firewalls {
				rows[i] = []string{
					strconv.Itoa(fw.ID),
					ptrOrDash(fw.Name),
					regionCell(fw.DatacenterID),
					strconv.Itoa(len(fw.Rules)),
					fw.CreatedAt,
				}
			}
			p.Table([]string{"ID", "Name", "Region", "Rules", "Created"}, rows)

			if pagination != nil {
				printer.PaginationFooter(pagination.CurrentPage, pagination.LastPage, int(pagination.Total), "firewalls")
			}
			return nil
		},
	}

	cmd.Flags().IntVarP(&page, "page", "p", 1, "page number")
	cmd.Flags().IntVarP(&limit, "limit", "l", 15, "results per page (max 100)")
	return cmd
}

func newFirewallGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get <id>",
		Short: "Get details and rules of a firewall",
		Args:  cobra.ExactArgs(1),
		Example: `  arianet firewall get 7
  arianet firewall get 7 --output json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := parseFirewallID(args[0])
			if err != nil {
				return err
			}

			client, err := requireAuth()
			if err != nil {
				handleAPIError(err)
				return nil
			}

			fw, err := client.GetFirewall(id)
			if err != nil {
				handleAPIError(err)
				return nil
			}

			p := printer.New(cfg.Output)
			if jsonMode() {
				p.PrintJSON(fw)
				return nil
			}

			fmt.Printf("%s %s\n", printer.Bold("Firewall:"), printer.Cyan(derefStr(fw.Name)))
			fmt.Printf("  ID: %d  |  Region: %s  |  Created: %s\n\n", fw.ID, regionCell(fw.DatacenterID), fw.CreatedAt)

			if len(fw.Rules) == 0 {
				printer.Info("No rules configured.")
				printer.Info(fmt.Sprintf("Add one with: arianet firewall rule add %d --direction ingress --proto tcp --port 22 --remote-ip 203.0.113.7/32", fw.ID))
				return nil
			}

			rows := make([][]string, len(fw.Rules))
			for i, r := range fw.Rules {
				rows[i] = []string{
					r.ID,
					r.Direction,
					r.Protocol,
					ptrOrDash(r.PortRange),
					ptrOrDash(r.RemoteIP),
					ptrOrDash(r.Description),
				}
			}
			p.Table([]string{"Rule ID", "Direction", "Protocol", "Port", "Remote IP", "Description"}, rows)
			return nil
		},
	}
}

func newFirewallCreateCmd() *cobra.Command {
	var (
		name         string
		description  string
		datacenterID int
	)

	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a new firewall",
		Long: `Create a new firewall in a region.

The firewall starts without rules; add them with "arianet firewall rule add"
and apply it to servers with "arianet firewall attach".`,
		Example: `  arianet firewall create --name web-fw --region 1
  arianet firewall create --name web-fw --region 1 --description "public web servers"`,
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := requireAuth()
			if err != nil {
				handleAPIError(err)
				return nil
			}

			fw, err := client.CreateFirewall(api.CreateFirewallRequest{
				Name:         name,
				DatacenterID: datacenterID,
				Description:  description,
			})
			if err != nil {
				handleAPIError(err)
				return nil
			}

			if jsonMode() {
				printer.New(cfg.Output).PrintJSON(fw)
				return nil
			}

			printer.Success(fmt.Sprintf("Firewall %q created (ID: %d)", derefStr(fw.Name), fw.ID))
			printer.Info(fmt.Sprintf("Add rules with: arianet firewall rule add %d --direction ingress --proto tcp --port 80 --remote-ip 0.0.0.0/0", fw.ID))
			return nil
		},
	}

	cmd.Flags().StringVar(&name, "name", "", "firewall name")
	cmd.Flags().IntVar(&datacenterID, "region", 0, "region ID (see: arianet region list)")
	cmd.Flags().StringVar(&description, "description", "", "optional description")
	_ = cmd.MarkFlagRequired("name")
	_ = cmd.MarkFlagRequired("region")
	return cmd
}

func newFirewallUpdateCmd() *cobra.Command {
	var name, description string

	cmd := &cobra.Command{
		Use:     "update <id>",
		Short:   "Rename a firewall or change its description",
		Args:    cobra.ExactArgs(1),
		Example: `  arianet firewall update 7 --name web-fw-2`,
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := parseFirewallID(args[0])
			if err != nil {
				return err
			}
			if !cmd.Flags().Changed("name") && !cmd.Flags().Changed("description") {
				return fmt.Errorf("nothing to update: pass --name and/or --description")
			}

			req := api.UpdateFirewallRequest{}
			if cmd.Flags().Changed("name") {
				req.Name = &name
			}
			if cmd.Flags().Changed("description") {
				req.Description = &description
			}

			client, err := requireAuth()
			if err != nil {
				handleAPIError(err)
				return nil
			}

			fw, err := client.UpdateFirewall(id, req)
			if err != nil {
				handleAPIError(err)
				return nil
			}

			if jsonMode() {
				printer.New(cfg.Output).PrintJSON(fw)
				return nil
			}
			printer.Success(fmt.Sprintf("Firewall #%d updated.", id))
			return nil
		},
	}

	cmd.Flags().StringVar(&name, "name", "", "new name")
	cmd.Flags().StringVar(&description, "description", "", "new description")
	return cmd
}

func newFirewallDeleteCmd() *cobra.Command {
	var yes bool

	cmd := &cobra.Command{
		Use:   "delete <id>",
		Short: "Delete a firewall",
		Args:  cobra.ExactArgs(1),
		Example: `  arianet firewall delete 7
  arianet firewall delete 7 --yes`,
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := parseFirewallID(args[0])
			if err != nil {
				return err
			}

			if !yes {
				if !confirm(fmt.Sprintf("Delete firewall #%d?", id)) {
					printer.Info("Cancelled.")
					return nil
				}
			}

			client, err := requireAuth()
			if err != nil {
				handleAPIError(err)
				return nil
			}

			if err := client.DeleteFirewall(id); err != nil {
				handleAPIError(err)
				return nil
			}

			reportDeleted("firewall", strconv.Itoa(id))
			return nil
		},
	}

	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "skip confirmation prompt")
	return cmd
}

func newFirewallAttachCmd() *cobra.Command {
	return newFirewallAttachDetach(true)
}

func newFirewallDetachCmd() *cobra.Command {
	return newFirewallAttachDetach(false)
}

func newFirewallAttachDetach(attach bool) *cobra.Command {
	var servers []string

	verb, past, prep := "detach", "detached", "from"
	short := "Detach a firewall from servers"
	if attach {
		verb, past, prep = "attach", "attached", "to"
		short = "Apply a firewall to servers"
	}

	cmd := &cobra.Command{
		Use:   verb + " <firewall-id>",
		Short: short,
		Args:  cobra.ExactArgs(1),
		Example: fmt.Sprintf(`  arianet firewall %s 7 --server 42
  arianet firewall %s 7 --server 42,43,44`, verb, verb),
		RunE: func(cmd *cobra.Command, args []string) error {
			fwID, err := parseFirewallID(args[0])
			if err != nil {
				return err
			}
			ids, err := parseIDList(servers)
			if err != nil {
				return err
			}
			if len(ids) > 50 {
				return fmt.Errorf("at most 50 servers per request")
			}

			client, err := requireAuth()
			if err != nil {
				handleAPIError(err)
				return nil
			}

			if attach {
				err = client.AttachFirewall(fwID, ids)
			} else {
				err = client.DetachFirewall(fwID, ids)
			}
			if err != nil {
				handleAPIError(err)
				return nil
			}

			if jsonMode() {
				printer.New(cfg.Output).PrintJSON(map[string]any{"firewall_id": fwID, "server_ids": ids, verb: true})
				return nil
			}
			printer.Success(fmt.Sprintf("Firewall #%d %s %s %d server(s).", fwID, past, prep, len(ids)))
			return nil
		},
	}

	cmd.Flags().StringSliceVar(&servers, "server", nil, "server ID (repeat or comma-separate for several)")
	_ = cmd.MarkFlagRequired("server")
	return cmd
}

func newFirewallRuleCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "rule",
		Short: "Manage firewall rules",
	}

	var (
		direction   string
		protocol    string
		portRange   string
		remoteIP    string
		description string
	)

	addCmd := &cobra.Command{
		Use:   "add <firewall-id>",
		Short: "Add a rule to a firewall",
		Long: `Add a rule to a firewall.

--remote-ip is the allowed source for ingress rules and the allowed
destination for egress rules. It is required on purpose: pass 0.0.0.0/0 to
allow everyone.`,
		Args: cobra.ExactArgs(1),
		Example: `  arianet firewall rule add 7 --direction ingress --proto tcp --port 443 --remote-ip 0.0.0.0/0
  arianet firewall rule add 7 --direction ingress --proto tcp --port 22 --remote-ip 203.0.113.7/32 --description "office"
  arianet firewall rule add 7 --direction ingress --proto icmp --remote-ip 0.0.0.0/0
  arianet firewall rule add 7 --direction egress --proto tcp --port 8000-9000 --remote-ip 10.0.0.0/8`,
		RunE: func(cmd *cobra.Command, args []string) error {
			fwID, err := parseFirewallID(args[0])
			if err != nil {
				return err
			}

			direction = strings.ToLower(direction)
			protocol = strings.ToLower(protocol)
			if direction != "ingress" && direction != "egress" {
				return fmt.Errorf("--direction must be ingress or egress")
			}
			switch protocol {
			case "tcp", "udp":
				if portRange == "" {
					return fmt.Errorf("--port is required for %s (e.g. 443 or 8000-9000)", protocol)
				}
			case "icmp", "esp", "gre":
			default:
				return fmt.Errorf("--proto must be one of tcp, udp, icmp, esp, gre")
			}

			client, err := requireAuth()
			if err != nil {
				handleAPIError(err)
				return nil
			}

			rule, err := client.AddFirewallRule(fwID, api.AddFirewallRuleRequest{
				Direction:   direction,
				Protocol:    protocol,
				PortRange:   portRange,
				RemoteIP:    remoteIP,
				Description: description,
			})
			if err != nil {
				handleAPIError(err)
				return nil
			}

			if jsonMode() {
				printer.New(cfg.Output).PrintJSON(rule)
				return nil
			}
			printer.Success(fmt.Sprintf("Rule added to firewall #%d (rule ID: %s).", fwID, rule.ID))
			printer.Info(fmt.Sprintf("Remove it with: arianet firewall rule remove %d %s", fwID, rule.ID))
			return nil
		},
	}

	addCmd.Flags().StringVar(&direction, "direction", "ingress", "traffic direction: ingress|egress")
	addCmd.Flags().StringVar(&protocol, "proto", "tcp", "protocol: tcp|udp|icmp|esp|gre")
	addCmd.Flags().StringVar(&portRange, "port", "", "port or range, e.g. 443 or 8000-9000 (tcp/udp)")
	addCmd.Flags().StringVar(&remoteIP, "remote-ip", "", "source (ingress) or destination (egress) CIDR, e.g. 203.0.113.7/32")
	addCmd.Flags().StringVar(&description, "description", "", "optional note shown in the rule list")
	_ = addCmd.MarkFlagRequired("remote-ip")

	removeCmd := &cobra.Command{
		Use:   "remove <firewall-id> <rule-id>",
		Short: "Remove a rule from a firewall",
		Long: `Remove a rule from a firewall.

The rule ID is the string shown in the "Rule ID" column of
"arianet firewall get <firewall-id>".`,
		Args: cobra.ExactArgs(2),
		Example: `  arianet firewall get 7
  arianet firewall rule remove 7 3f9c2a1e`,
		RunE: func(cmd *cobra.Command, args []string) error {
			fwID, err := parseFirewallID(args[0])
			if err != nil {
				return err
			}
			ruleID := args[1]

			yes, _ := cmd.Flags().GetBool("yes")
			if !yes {
				if !confirm(fmt.Sprintf("Remove rule %s from firewall #%d?", ruleID, fwID)) {
					printer.Info("Cancelled.")
					return nil
				}
			}

			client, err := requireAuth()
			if err != nil {
				handleAPIError(err)
				return nil
			}

			if err := client.DeleteFirewallRule(fwID, ruleID); err != nil {
				handleAPIError(err)
				return nil
			}

			reportDeleted("rule", ruleID)
			return nil
		},
	}
	removeCmd.Flags().BoolP("yes", "y", false, "skip confirmation prompt")

	cmd.AddCommand(addCmd, removeCmd)
	return cmd
}
