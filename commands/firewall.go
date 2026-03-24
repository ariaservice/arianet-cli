package commands

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/arianet/arianet-cli/internal/api"
	"github.com/arianet/arianet-cli/internal/printer"
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
		newFirewallDeleteCmd(),
		newFirewallRuleCmd(),
	)
	return cmd
}

func newFirewallListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List your firewalls",
		Example: `  arianet firewall list
  arianet firewall list --output json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := requireAuth()
			if err != nil {
				handleAPIError(err)
				return nil
			}

			firewalls, err := client.ListFirewalls()
			if err != nil {
				handleAPIError(err)
				return nil
			}

			p := printer.New(cfg.Output)
			if cfg.Output == printer.FormatJSON {
				p.PrintJSON(firewalls)
				return nil
			}

			if len(firewalls) == 0 {
				printer.Info("No firewalls found.")
				return nil
			}

			rows := make([][]string, len(firewalls))
			for i, fw := range firewalls {
				rows[i] = []string{
					strconv.Itoa(fw.ID),
					fw.Name,
					strconv.Itoa(fw.DatacenterID),
					strconv.Itoa(len(fw.Rules)),
					fw.CreatedAt,
				}
			}
			p.Table([]string{"ID", "Name", "Region ID", "Rules", "Created"}, rows)
			return nil
		},
	}
}

func newFirewallGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get <id>",
		Short: "Get details and rules of a firewall",
		Args:  cobra.ExactArgs(1),
		Example: `  arianet firewall get 7
  arianet firewall get 7 --output json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := strconv.Atoi(args[0])
			if err != nil {
				return fmt.Errorf("invalid firewall ID: %s", args[0])
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
			if cfg.Output == printer.FormatJSON {
				p.PrintJSON(fw)
				return nil
			}

			fmt.Printf("%s %s\n", printer.Bold("Firewall:"), printer.Cyan(fw.Name))
			fmt.Printf("  ID: %d  |  Region ID: %d  |  Created: %s\n\n", fw.ID, fw.DatacenterID, fw.CreatedAt)

			if len(fw.Rules) == 0 {
				printer.Info("No rules configured.")
				printer.Info("Add a rule with: arianet firewall rule add " + strconv.Itoa(fw.ID) + " --direction inbound --proto tcp --port 22 --source 0.0.0.0/0 --action allow")
				return nil
			}

			rows := make([][]string, len(fw.Rules))
			for i, r := range fw.Rules {
				action := r.Action
				if strings.ToLower(r.Action) == "allow" {
					action = printer.BoolCheck(true) + " allow"
				} else {
					action = printer.Dim("deny")
				}
				rows[i] = []string{
					strconv.Itoa(i),
					r.Direction,
					r.Protocol,
					r.PortRange,
					r.Source,
					action,
				}
			}
			p.Table([]string{"#", "Direction", "Protocol", "Port", "Source", "Action"}, rows)
			return nil
		},
	}
}

func newFirewallCreateCmd() *cobra.Command {
	var (
		name         string
		datacenterID int
	)

	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a new firewall",
		Example: `  arianet firewall create --name web-fw --region 1`,
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := requireAuth()
			if err != nil {
				handleAPIError(err)
				return nil
			}

			fw, err := client.CreateFirewall(api.CreateFirewallRequest{
				Name:         name,
				DatacenterID: datacenterID,
			})
			if err != nil {
				handleAPIError(err)
				return nil
			}

			printer.Success(fmt.Sprintf("Firewall %q created (ID: %d)", fw.Name, fw.ID))
			printer.Info(fmt.Sprintf("Add rules with: arianet firewall rule add %d --direction inbound --proto tcp --port 80 --source 0.0.0.0/0 --action allow", fw.ID))
			return nil
		},
	}

	cmd.Flags().StringVar(&name, "name", "", "firewall name")
	cmd.Flags().IntVar(&datacenterID, "region", 0, "datacenter/region ID (see: arianet region list)")
	_ = cmd.MarkFlagRequired("name")
	_ = cmd.MarkFlagRequired("region")
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
			id, err := strconv.Atoi(args[0])
			if err != nil {
				return fmt.Errorf("invalid firewall ID: %s", args[0])
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

			printer.Success(fmt.Sprintf("Firewall #%d deleted.", id))
			return nil
		},
	}

	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "skip confirmation prompt")
	return cmd
}

func newFirewallRuleCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "rule",
		Short: "Manage firewall rules",
	}

	// add rule
	var (
		direction string
		protocol  string
		portRange string
		source    string
		action    string
	)

	addCmd := &cobra.Command{
		Use:   "add <firewall-id>",
		Short: "Add a rule to a firewall",
		Args:  cobra.ExactArgs(1),
		Example: `  arianet firewall rule add 7 --direction inbound --proto tcp --port 80 --source 0.0.0.0/0 --action allow
  arianet firewall rule add 7 --direction inbound --proto tcp --port 22 --source 192.168.1.0/24 --action allow
  arianet firewall rule add 7 --direction outbound --proto any --port any --source 0.0.0.0/0 --action allow`,
		RunE: func(cmd *cobra.Command, args []string) error {
			fwID, err := strconv.Atoi(args[0])
			if err != nil {
				return fmt.Errorf("invalid firewall ID: %s", args[0])
			}

			client, err := requireAuth()
			if err != nil {
				handleAPIError(err)
				return nil
			}

			rule := api.FirewallRule{
				Direction: direction,
				Protocol:  protocol,
				PortRange: portRange,
				Source:    source,
				Action:    action,
			}

			if err := client.AddFirewallRule(fwID, rule); err != nil {
				handleAPIError(err)
				return nil
			}

			printer.Success(fmt.Sprintf("Rule added to firewall #%d.", fwID))
			return nil
		},
	}

	addCmd.Flags().StringVar(&direction, "direction", "inbound", "traffic direction: inbound|outbound")
	addCmd.Flags().StringVar(&protocol, "proto", "tcp", "protocol: tcp|udp|icmp|any")
	addCmd.Flags().StringVar(&portRange, "port", "", "port or range (e.g. 80, 443, 8000-9000, any)")
	addCmd.Flags().StringVar(&source, "source", "0.0.0.0/0", "source CIDR (e.g. 0.0.0.0/0, 192.168.1.0/24)")
	addCmd.Flags().StringVar(&action, "action", "allow", "action: allow|deny")
	_ = addCmd.MarkFlagRequired("port")

	// remove rule
	removeCmd := &cobra.Command{
		Use:   "remove <firewall-id> <rule-index>",
		Short: "Remove a rule from a firewall by its index",
		Args:  cobra.ExactArgs(2),
		Example: `  arianet firewall rule remove 7 0
  arianet firewall rule remove 7 2`,
		RunE: func(cmd *cobra.Command, args []string) error {
			fwID, err := strconv.Atoi(args[0])
			if err != nil {
				return fmt.Errorf("invalid firewall ID: %s", args[0])
			}
			ruleIndex, err := strconv.Atoi(args[1])
			if err != nil {
				return fmt.Errorf("invalid rule index: %s", args[1])
			}

			yes, _ := cmd.Flags().GetBool("yes")
			if !yes {
				if !confirm(fmt.Sprintf("Remove rule #%d from firewall #%d?", ruleIndex, fwID)) {
					printer.Info("Cancelled.")
					return nil
				}
			}

			client, err := requireAuth()
			if err != nil {
				handleAPIError(err)
				return nil
			}

			if err := client.DeleteFirewallRule(fwID, ruleIndex); err != nil {
				handleAPIError(err)
				return nil
			}

			printer.Success(fmt.Sprintf("Rule #%d removed from firewall #%d.", ruleIndex, fwID))
			return nil
		},
	}
	removeCmd.Flags().BoolP("yes", "y", false, "skip confirmation prompt")

	cmd.AddCommand(addCmd, removeCmd)
	return cmd
}
