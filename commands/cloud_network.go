package commands

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ariaservice/arianet-cli/internal/printer"
	"github.com/spf13/cobra"
)

// ─── Networks ─────────────────────────────────────────────────────────────────

var cloudNetwork = cloudResource{
	plural: "networks", singular: "network", path: "/networks",
	cols: []col{
		{"ID", "id"}, {"Name", "name"}, {"IP range", "ip_range"}, {"Datacenter", "datacenter"},
		{"Status", "status"}, {"Subnets", "subnets"}, {"Servers", "servers"}, {"Labels", "labels"},
	},
	detail: []col{
		{"ID", "id"}, {"Name", "name"}, {"IP range", "ip_range"}, {"Datacenter", "datacenter"},
		{"Status", "status"}, {"Router", "router.name"}, {"Router IP", "router.public_ip"},
		{"Delete protection", "protection_delete"}, {"Labels", "labels"}, {"Project", "project_id"},
		{"Created", "created_at"},
	},
	subtables: func(p *printer.Printer, obj map[string]any) {
		subtable(p, obj, "subnets", "Subnets", []col{{"ID", "id"}, {"IP range", "ip_range"}, {"Gateway", "gateway"}, {"Status", "status"}})
		subtable(p, obj, "routes", "Routes", []col{{"Destination", "destination"}, {"Gateway", "gateway"}})
		subtable(p, obj, "servers", "Servers", []col{{"Server", "server_id"}, {"Name", "name"}, {"IP", "ip"}, {"Subnet", "subnet_id"}, {"Status", "status"}})
	},
}

func newCloudNetworksCmd() *cobra.Command {
	r := cloudNetwork
	cmd := &cobra.Command{
		Use:     "networks",
		Aliases: []string{"network", "net"},
		Short:   "Manage private networks, subnets and routes",
	}
	cmd.AddCommand(
		r.listCmd("  arianet cloud networks list", nil, nil),
		r.getCmd(),
		r.createCmd(`  arianet cloud networks create --name backend --datacenter eu-central-1 --ip-range 10.0.0.0/16
  arianet cloud networks create --name backend --datacenter eu-central-1 --ip-range 10.0.0.0/16 --subnet-ip-range 10.0.1.0/24 --wait`,
			func(cmd *cobra.Command) {
				cmd.Flags().String("name", "", "network name")
				cmd.Flags().String("datacenter", "", "datacenter code")
				cmd.Flags().String("ip-range", "", "the network's range (CIDR)")
				cmd.Flags().String("subnet-ip-range", "", "first subnet's range (default: chosen for you)")
				_ = cmd.MarkFlagRequired("name")
				_ = cmd.MarkFlagRequired("datacenter")
			}, func(cmd *cobra.Command, body map[string]any) error {
				changedString(cmd, body, "name", "name")
				changedString(cmd, body, "datacenter", "datacenter")
				changedString(cmd, body, "ip-range", "ip_range")
				changedString(cmd, body, "subnet-ip-range", "subnet_ip_range")
				return nil
			}),
		r.patchCmd("  arianet cloud networks update 4 --name backend-2 --labels env=prod", func(cmd *cobra.Command) {
			cmd.Flags().String("name", "", "new name")
			cmd.Flags().Bool("protection-delete", false, "protect against deletion (true/false)")
		}, func(cmd *cobra.Command, body map[string]any) error {
			changedString(cmd, body, "name", "name")
			changedBool(cmd, body, "protection-delete", "protection_delete")
			return nil
		}),
		r.deleteCmd(),
		newCloudSubnetsCmd(),
		newCloudRoutesCmd(),
	)
	return cmd
}

func newCloudSubnetsCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "subnets", Aliases: []string{"subnet"}, Short: "Add or remove a network's subnets"}

	var (
		ipRange string
		w       waitFlags
	)
	add := &cobra.Command{
		Use:     "add <network-id>",
		Short:   "Add a subnet",
		Example: "  arianet cloud networks subnets add 4 --ip-range 10.0.2.0/24",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := parseCloudID(args[0], "network")
			if err != nil {
				return err
			}
			cc := requireCloud()
			var raw json.RawMessage
			if err := cc.Post(cloudNetwork.itemPath(id)+"/subnets", map[string]any{"ip_range": ipRange}, &raw); err != nil {
				handleAPIError(err)
				return nil
			}
			reportMutation(cc, raw, fmt.Sprintf("Subnet added to network #%d.", id), w)
			return nil
		},
	}
	add.Flags().StringVar(&ipRange, "ip-range", "", "subnet range (CIDR) inside the network's range")
	_ = add.MarkFlagRequired("ip-range")
	w.add(add)

	var dw waitFlags
	del := &cobra.Command{
		Use:     "delete <network-id> <subnet-id>",
		Aliases: []string{"rm"},
		Short:   "Delete a subnet",
		Example: "  arianet cloud networks subnets delete 4 9",
		Args:    cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := parseCloudID(args[0], "network")
			if err != nil {
				return err
			}
			subnet, err := parseCloudID(args[1], "subnet")
			if err != nil {
				return err
			}
			cc := requireCloud()
			var raw json.RawMessage
			if err := cc.Delete(fmt.Sprintf("%s/subnets/%d", cloudNetwork.itemPath(id), subnet), nil, nil, &raw); err != nil {
				handleAPIError(err)
				return nil
			}
			reportMutation(cc, raw, fmt.Sprintf("Deletion of subnet #%d accepted.", subnet), dw)
			return nil
		},
	}
	dw.add(del)
	cmd.AddCommand(add, del)
	return cmd
}

func newCloudRoutesCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "routes", Aliases: []string{"route"}, Short: "Set a network's static routes"}
	var (
		routes []string
		clear  bool
		w      waitFlags
	)
	set := &cobra.Command{
		Use:   "set <network-id>",
		Short: "Replace every route of a network",
		Example: `  arianet cloud networks routes set 4 --route 10.100.0.0/16=10.0.1.5
  arianet cloud networks routes set 4 --clear`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := parseCloudID(args[0], "network")
			if err != nil {
				return err
			}
			if clear == (len(routes) > 0) {
				return fmt.Errorf("pass --route destination=gateway (repeatable) or --clear")
			}
			list := []map[string]string{}
			for _, r := range routes {
				dest, gw, ok := strings.Cut(r, "=")
				if !ok || strings.TrimSpace(dest) == "" || strings.TrimSpace(gw) == "" {
					return fmt.Errorf("invalid route %q: use destination=gateway, e.g. 10.100.0.0/16=10.0.1.5", r)
				}
				list = append(list, map[string]string{"destination": strings.TrimSpace(dest), "gateway": strings.TrimSpace(gw)})
			}
			cc := requireCloud()
			var raw json.RawMessage
			if err := cc.Put(cloudNetwork.itemPath(id)+"/routes", map[string]any{"routes": list}, &raw); err != nil {
				handleAPIError(err)
				return nil
			}
			reportMutation(cc, raw, fmt.Sprintf("Routes of network #%d replaced.", id), w)
			return nil
		},
	}
	set.Flags().StringArrayVar(&routes, "route", nil, "destination=gateway (repeatable)")
	set.Flags().BoolVar(&clear, "clear", false, "remove every route")
	w.add(set)
	cmd.AddCommand(set)
	return cmd
}

// ─── Firewalls ────────────────────────────────────────────────────────────────

var cloudFirewall = cloudResource{
	plural: "firewalls", singular: "firewall", path: "/firewalls",
	cols: []col{
		{"ID", "id"}, {"Name", "name"}, {"Rules", "rules"}, {"Servers", "server_ids"}, {"Labels", "labels"},
	},
	detail: []col{
		{"ID", "id"}, {"Name", "name"}, {"Servers", "server_ids"}, {"Via selectors", "selector_server_ids"},
		{"Delete protection", "protection_delete"}, {"Labels", "labels"}, {"Project", "project_id"},
		{"Created", "created_at"},
	},
	subtables: func(p *printer.Printer, obj map[string]any) {
		subtable(p, obj, "rules", "Rules", []col{
			{"Direction", "direction"}, {"Protocol", "protocol"}, {"Port", "port"},
			{"Sources", "source_ips"}, {"Destinations", "destination_ips"}, {"Description", "description"},
		})
		subtable(p, obj, "applied_to", "Applied to", []col{{"Type", "type"}, {"Server", "server.id"}, {"Label selector", "label_selector.selector"}})
	},
}

// parseRule reads "direction=in,protocol=tcp,port=22,source=0.0.0.0/0,source=::/0,description=ssh".
func parseRule(spec string) (map[string]any, error) {
	rule := map[string]any{}
	var sources, dests []string
	for _, part := range strings.Split(spec, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		k, v, ok := strings.Cut(part, "=")
		if !ok {
			return nil, fmt.Errorf("invalid rule part %q: use key=value", part)
		}
		v = strings.TrimSpace(v)
		switch strings.TrimSpace(k) {
		case "direction":
			rule["direction"] = v
		case "protocol", "proto":
			rule["protocol"] = v
		case "port":
			rule["port"] = v
		case "source", "source_ips", "src":
			sources = append(sources, v)
		case "destination", "destination_ips", "dst":
			dests = append(dests, v)
		case "description":
			rule["description"] = v
		default:
			return nil, fmt.Errorf("unknown rule key %q (direction, protocol, port, source, destination, description)", k)
		}
	}
	if rule["direction"] == nil || rule["protocol"] == nil {
		return nil, fmt.Errorf("rule %q needs direction (in|out) and protocol", spec)
	}
	if len(sources) > 0 {
		rule["source_ips"] = sources
	}
	if len(dests) > 0 {
		rule["destination_ips"] = dests
	}
	return rule, nil
}

// ruleFlags reads rules from --rule specs or a --rules-file JSON array.
type ruleFlags struct {
	specs []string
	file  string
}

func (f *ruleFlags) add(cmd *cobra.Command) {
	cmd.Flags().StringArrayVar(&f.specs, "rule", nil, `rule "direction=in,protocol=tcp,port=22,source=0.0.0.0/0[,source=::/0][,description=ssh]" (repeatable)`)
	cmd.Flags().StringVar(&f.file, "rules-file", "", "JSON file with an array of rules (as the API takes them)")
}

func (f *ruleFlags) given() bool { return len(f.specs) > 0 || f.file != "" }

func (f *ruleFlags) rules() ([]any, error) {
	if len(f.specs) > 0 && f.file != "" {
		return nil, fmt.Errorf("use --rule or --rules-file, not both")
	}
	out := []any{}
	if f.file != "" {
		if err := readJSONFile(f.file, &out); err != nil {
			return nil, err
		}
		return out, nil
	}
	for _, s := range f.specs {
		r, err := parseRule(s)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, nil
}

func newCloudFirewallsCmd() *cobra.Command {
	r := cloudFirewall
	cmd := &cobra.Command{
		Use:     "firewalls",
		Aliases: []string{"firewall", "fw"},
		Short:   "Manage firewalls, their rules and what they apply to",
	}

	var createRules ruleFlags
	create := r.createCmd(`  arianet cloud firewalls create --name web \
      --rule direction=in,protocol=tcp,port=443,source=0.0.0.0/0,source=::/0 \
      --rule direction=in,protocol=tcp,port=22,source=203.0.113.4/32 --server 12
  arianet cloud firewalls create --name web --rules-file rules.json`,
		func(cmd *cobra.Command) {
			cmd.Flags().String("name", "", "firewall name")
			cmd.Flags().StringArray("server", nil, "server ID to apply it to (repeatable)")
			createRules.add(cmd)
			_ = cmd.MarkFlagRequired("name")
		}, func(cmd *cobra.Command, body map[string]any) error {
			changedString(cmd, body, "name", "name")
			if err := changedIDs(cmd, body, "server", "server_ids", "server"); err != nil {
				return err
			}
			if createRules.given() {
				rules, err := createRules.rules()
				if err != nil {
					return err
				}
				body["rules"] = rules
			}
			return nil
		})

	cmd.AddCommand(
		r.listCmd("  arianet cloud firewalls list\n  arianet cloud firewalls list --label-selector env=prod", nil, nil),
		r.getCmd(),
		create,
		r.patchCmd("  arianet cloud firewalls update 2 --name web-2 --labels env=prod", func(cmd *cobra.Command) {
			cmd.Flags().String("name", "", "new name")
			cmd.Flags().String("description", "", "description")
			cmd.Flags().Bool("protection-delete", false, "protect against deletion (true/false)")
		}, func(cmd *cobra.Command, body map[string]any) error {
			changedString(cmd, body, "name", "name")
			changedString(cmd, body, "description", "description")
			changedBool(cmd, body, "protection-delete", "protection_delete")
			return nil
		}),
		r.deleteCmd(),
		newCloudFirewallRulesCmd(),
		newCloudFirewallTargetsCmd("apply-to", "apply_to_resources", "apply_to", "Apply a firewall to servers or label selectors"),
		newCloudFirewallTargetsCmd("remove-from", "remove_from_resources", "remove_from", "Remove a firewall from servers or label selectors"),
		newCloudFirewallSetServersCmd(),
	)
	return cmd
}

func newCloudFirewallRulesCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "rules", Aliases: []string{"rule"}, Short: "Replace a firewall's rules"}
	var (
		rf    ruleFlags
		clear bool
		w     waitFlags
	)
	set := &cobra.Command{
		Use:   "set <firewall-id>",
		Short: "Replace every rule of a firewall",
		Example: `  arianet cloud firewalls rules set 2 --rule direction=in,protocol=tcp,port=80,source=0.0.0.0/0
  arianet cloud firewalls rules set 2 --rules-file rules.json --wait
  arianet cloud firewalls rules set 2 --clear`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := parseCloudID(args[0], "firewall")
			if err != nil {
				return err
			}
			if clear == rf.given() {
				return fmt.Errorf("pass --rule, --rules-file or --clear")
			}
			rules := []any{}
			if !clear {
				if rules, err = rf.rules(); err != nil {
					return err
				}
			}
			cc := requireCloud()
			// PUT replaces name and rules together, so the current name is
			// read first and sent back unchanged.
			var current struct {
				Data struct {
					Name string `json:"name"`
				} `json:"data"`
			}
			if err := cc.Get(cloudFirewall.itemPath(id), nil, &current); err != nil {
				handleAPIError(err)
				return nil
			}
			var raw json.RawMessage
			if err := cc.Put(cloudFirewall.itemPath(id), map[string]any{"name": current.Data.Name, "rules": rules}, &raw); err != nil {
				handleAPIError(err)
				return nil
			}
			reportMutation(cc, raw, fmt.Sprintf("Rules of firewall #%d replaced (%d rules).", id, len(rules)), w)
			return nil
		},
	}
	rf.add(set)
	set.Flags().BoolVar(&clear, "clear", false, "remove every rule")
	w.add(set)
	cmd.AddCommand(set)
	return cmd
}

func newCloudFirewallTargetsCmd(use, op, key, short string) *cobra.Command {
	var (
		servers   []string
		selectors []string
		w         waitFlags
	)
	cmd := &cobra.Command{
		Use:   use + " <firewall-id>",
		Short: short,
		Long: short + `.

A label selector target follows the labels: servers that gain matching labels
later get the firewall too, servers that lose them lose it.`,
		Example: fmt.Sprintf(`  arianet cloud firewalls %[1]s 2 --server 12 --server 13
  arianet cloud firewalls %[1]s 2 --label-selector env=prod
  arianet cloud firewalls %[1]s 2 --label-selector "env=prod,tier!=db" --wait`, use),
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := parseCloudID(args[0], "firewall")
			if err != nil {
				return err
			}
			ids, err := parseCloudIDs(servers, "server")
			if err != nil {
				return err
			}
			if len(ids) == 0 && len(selectors) == 0 {
				return fmt.Errorf("pass at least one --server or --label-selector")
			}
			targets := []map[string]any{}
			for _, sid := range ids {
				targets = append(targets, map[string]any{"type": "server", "server": map[string]any{"id": sid}})
			}
			for _, sel := range selectors {
				if strings.TrimSpace(sel) == "" {
					return fmt.Errorf("--label-selector cannot be empty")
				}
				targets = append(targets, map[string]any{"type": "label_selector", "label_selector": map[string]any{"selector": sel}})
			}
			cc := requireCloud()
			var raw json.RawMessage
			if err := cc.Post(fmt.Sprintf("%s/actions/%s", cloudFirewall.itemPath(id), op), map[string]any{key: targets}, &raw); err != nil {
				handleAPIError(err)
				return nil
			}
			reportMutation(cc, raw, fmt.Sprintf("Firewall #%d: %s accepted.", id, strings.ReplaceAll(use, "-", " ")), w)
			return nil
		},
	}
	cmd.Flags().StringArrayVar(&servers, "server", nil, "server ID (repeatable, or comma separated)")
	cmd.Flags().StringArrayVar(&selectors, "label-selector", nil, `label selector, e.g. "env=prod" (repeatable)`)
	w.add(cmd)
	return cmd
}

func newCloudFirewallSetServersCmd() *cobra.Command {
	var (
		servers []string
		none    bool
		w       waitFlags
	)
	cmd := &cobra.Command{
		Use:   "set-servers <firewall-id>",
		Short: "Replace the servers a firewall is applied to directly",
		Example: `  arianet cloud firewalls set-servers 2 --server 12,13
  arianet cloud firewalls set-servers 2 --none`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := parseCloudID(args[0], "firewall")
			if err != nil {
				return err
			}
			ids, err := parseCloudIDs(servers, "server")
			if err != nil {
				return err
			}
			if none == (len(ids) > 0) {
				return fmt.Errorf("pass --server or --none")
			}
			if ids == nil {
				ids = []int64{}
			}
			cc := requireCloud()
			var raw json.RawMessage
			if err := cc.Put(cloudFirewall.itemPath(id)+"/servers", map[string]any{"server_ids": ids}, &raw); err != nil {
				handleAPIError(err)
				return nil
			}
			reportMutation(cc, raw, fmt.Sprintf("Servers of firewall #%d replaced.", id), w)
			return nil
		},
	}
	cmd.Flags().StringArrayVar(&servers, "server", nil, "server ID (repeatable, or comma separated)")
	cmd.Flags().BoolVar(&none, "none", false, "apply it to no server directly")
	w.add(cmd)
	return cmd
}

// ─── Floating IPs ─────────────────────────────────────────────────────────────

var cloudFloatingIP = cloudResource{
	plural: "floating-ips", singular: "floating IP", path: "/floating-ips",
	cols: []col{
		{"ID", "id"}, {"Name", "name"}, {"Type", "type"}, {"IP", "ip"}, {"Datacenter", "datacenter"},
		{"Server", "server_id"}, {"DNS PTR", "dns_ptr"}, {"Status", "status"}, {"Labels", "labels"},
	},
	detail: []col{
		{"ID", "id"}, {"Name", "name"}, {"Description", "description"}, {"Type", "type"}, {"IP", "ip"},
		{"Datacenter", "datacenter"}, {"Server", "server_id"}, {"DNS PTR", "dns_ptr"}, {"Status", "status"},
		{"Delete protection", "protection_delete"}, {"Labels", "labels"}, {"Project", "project_id"},
		{"Created", "created_at"},
	},
}

func newCloudFloatingIPsCmd() *cobra.Command {
	r := cloudFloatingIP
	cmd := &cobra.Command{
		Use:     "floating-ips",
		Aliases: []string{"floating-ip", "fip"},
		Short:   "Manage floating IPs",
	}
	cmd.AddCommand(
		r.listCmd("  arianet cloud floating-ips list", nil, nil),
		r.getCmd(),
		r.createCmd(`  arianet cloud floating-ips create --type ipv4 --datacenter eu-central-1 --name vip
  arianet cloud floating-ips create --type ipv4 --server 12 --wait`,
			func(cmd *cobra.Command) {
				cmd.Flags().String("type", "ipv4", "ipv4 or ipv6")
				cmd.Flags().String("datacenter", "", "datacenter code (not needed with --server)")
				cmd.Flags().String("name", "", "name")
				cmd.Flags().String("description", "", "description")
				cmd.Flags().Int64("server", 0, "assign it to this server")
			}, func(cmd *cobra.Command, body map[string]any) error {
				typ, _ := cmd.Flags().GetString("type")
				body["type"] = typ
				changedString(cmd, body, "datacenter", "datacenter")
				changedString(cmd, body, "name", "name")
				changedString(cmd, body, "description", "description")
				changedInt64(cmd, body, "server", "server_id")
				return nil
			}),
		r.patchCmd("  arianet cloud floating-ips update 5 --name vip --description \"web VIP\"", func(cmd *cobra.Command) {
			cmd.Flags().String("name", "", "new name")
			cmd.Flags().String("description", "", "description")
			cmd.Flags().Bool("protection-delete", false, "protect against deletion (true/false)")
		}, func(cmd *cobra.Command, body map[string]any) error {
			changedString(cmd, body, "name", "name")
			changedString(cmd, body, "description", "description")
			changedBool(cmd, body, "protection-delete", "protection_delete")
			return nil
		}),
		r.deleteCmd(),
		r.opCmd("assign", "assign", "Assign a floating IP to a server", "  arianet cloud floating-ips assign 5 --server 12 --wait",
			func(cmd *cobra.Command) {
				cmd.Flags().Int64("server", 0, "server ID")
				_ = cmd.MarkFlagRequired("server")
			}, func(cmd *cobra.Command) (any, error) {
				id, _ := cmd.Flags().GetInt64("server")
				return map[string]any{"server_id": id}, nil
			}),
		r.opCmd("unassign", "unassign", "Unassign a floating IP from its server", "  arianet cloud floating-ips unassign 5", nil, nil),
		r.opCmd("dns-ptr", "change_dns_ptr", "Set or reset a floating IP's reverse DNS",
			"  arianet cloud floating-ips dns-ptr 5 --ptr web.example.com\n  arianet cloud floating-ips dns-ptr 5 --reset",
			func(cmd *cobra.Command) {
				cmd.Flags().String("ptr", "", "hostname for the reverse DNS record")
				cmd.Flags().Bool("reset", false, "reset to the default record")
			}, func(cmd *cobra.Command) (any, error) {
				ptr, _ := cmd.Flags().GetString("ptr")
				reset, _ := cmd.Flags().GetBool("reset")
				if reset == (ptr != "") {
					return nil, fmt.Errorf("pass --ptr <hostname> or --reset")
				}
				if reset {
					return map[string]any{"dns_ptr": nil}, nil
				}
				return map[string]any{"dns_ptr": ptr}, nil
			}),
	)
	return cmd
}
