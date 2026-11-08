package commands

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/ariaservice/arianet-cli/internal/printer"
	"github.com/spf13/cobra"
)

var cloudLoadBalancer = cloudResource{
	plural: "load-balancers", singular: "load balancer", path: "/load-balancers",
	cols: []col{
		{"ID", "id"}, {"Name", "name"}, {"Type", "type"}, {"Algorithm", "algorithm"}, {"Status", "status"},
		{"Datacenter", "datacenter"}, {"IPv4", "ipv4"}, {"Healthy", "health_summary.healthy"},
		{"Unhealthy", "health_summary.unhealthy"}, {"Labels", "labels"},
	},
	detail: []col{
		{"ID", "id"}, {"Name", "name"}, {"Type", "type"}, {"Algorithm", "algorithm"}, {"Status", "status"},
		{"Datacenter", "datacenter"}, {"Public interface", "public_interface"}, {"IPv4", "ipv4"}, {"IPv6", "ipv6"},
		{"Private IP", "private_ip"}, {"Network", "network.name"}, {"Healthy", "health_summary.healthy"},
		{"Unhealthy", "health_summary.unhealthy"}, {"Unknown", "health_summary.unknown"},
		{"Delete protection", "protection_delete"}, {"Labels", "labels"}, {"Project", "project_id"},
		{"Created", "created_at"},
	},
	subtables: func(p *printer.Printer, obj map[string]any) {
		subtable(p, obj, "services", "Services", []col{
			{"Protocol", "protocol"}, {"Listen", "listen_port"}, {"Destination", "destination_port"},
			{"Proxy protocol", "proxyprotocol"}, {"Health", "health_check.protocol"}, {"Health port", "health_check.port"},
			{"Sticky", "http.sticky_sessions"},
		})
		subtable(p, obj, "targets", "Targets", []col{
			{"Target ID", "id"}, {"Type", "type"}, {"Server", "server_id"}, {"IP", "ip"},
			{"Label selector", "label_selector"}, {"Private IP", "use_private_ip"}, {"Health", "health_status"},
		})
	},
}

// serviceFlags builds one LB service from flags or a JSON file.
type serviceFlags struct {
	file                          string
	protocol                      string
	listenPort, destinationPort   int
	proxyProtocol, sticky         bool
	cookieName                    string
	healthProtocol                string
	healthPort                    int
	healthInterval, healthTimeout int
	healthRetries                 int
	healthPath, healthDomain      string
	healthStatusCodes             []string
}

func (f *serviceFlags) add(cmd *cobra.Command) {
	fl := cmd.Flags()
	fl.StringVar(&f.file, "file", "", "JSON file with the service (as the API takes it) instead of the flags")
	fl.StringVar(&f.protocol, "protocol", "tcp", "tcp, http or https (TLS passthrough)")
	fl.IntVar(&f.listenPort, "listen-port", 0, "port the load balancer listens on")
	fl.IntVar(&f.destinationPort, "destination-port", 0, "port on the targets (default: --listen-port)")
	fl.BoolVar(&f.proxyProtocol, "proxyprotocol", false, "send the PROXY protocol header")
	fl.BoolVar(&f.sticky, "sticky-sessions", false, "keep a client on one target (http, https)")
	fl.StringVar(&f.cookieName, "cookie-name", "", "http: stick on this application cookie")
	fl.StringVar(&f.healthProtocol, "health-protocol", "", "tcp or http (default: tcp)")
	fl.IntVar(&f.healthPort, "health-port", 0, "health check port (default: the destination port)")
	fl.IntVar(&f.healthInterval, "health-interval", 0, "seconds between checks (default 15)")
	fl.IntVar(&f.healthTimeout, "health-timeout", 0, "seconds a check may take (default 10)")
	fl.IntVar(&f.healthRetries, "health-retries", 0, "failed checks before a target is unhealthy (default 3)")
	fl.StringVar(&f.healthPath, "health-path", "", "http health check path, e.g. /healthz")
	fl.StringVar(&f.healthDomain, "health-domain", "", "http health check Host header")
	fl.StringArrayVar(&f.healthStatusCodes, "health-status-code", nil, `accepted status code, e.g. 200 or "2??" (repeatable)`)
}

func (f *serviceFlags) service() (map[string]any, error) {
	if f.file != "" {
		out := map[string]any{}
		if err := readJSONFile(f.file, &out); err != nil {
			return nil, err
		}
		return out, nil
	}
	if f.listenPort <= 0 {
		return nil, fmt.Errorf("--listen-port is required (or pass --file)")
	}
	dest := f.destinationPort
	if dest == 0 {
		dest = f.listenPort
	}
	health := map[string]any{"protocol": orStr(f.healthProtocol, "tcp"), "port": f.healthPort, "interval": f.healthInterval,
		"timeout": f.healthTimeout, "retries": f.healthRetries}
	if f.healthPort == 0 {
		health["port"] = dest
	}
	if f.healthPath != "" || f.healthDomain != "" || len(f.healthStatusCodes) > 0 {
		codes := f.healthStatusCodes
		if codes == nil {
			codes = []string{}
		}
		health["http"] = map[string]any{"path": orStr(f.healthPath, "/"), "domain": f.healthDomain, "status_codes": codes}
	}
	svc := map[string]any{
		"protocol": f.protocol, "listen_port": f.listenPort, "destination_port": dest,
		"proxyprotocol": f.proxyProtocol, "health_check": health,
	}
	if f.sticky || f.cookieName != "" {
		svc["http"] = map[string]any{"sticky_sessions": f.sticky || f.cookieName != "", "cookie_name": f.cookieName}
	}
	return svc, nil
}

func orStr(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}

// parseServiceShorthand reads "protocol:listen[:destination]", e.g. "tcp:80:8080".
func parseServiceShorthand(spec string) (map[string]any, error) {
	parts := strings.Split(spec, ":")
	if len(parts) < 2 || len(parts) > 3 {
		return nil, fmt.Errorf("invalid service %q: use protocol:listen-port[:destination-port], e.g. tcp:80:8080", spec)
	}
	listen, err := strconv.Atoi(parts[1])
	if err != nil || listen <= 0 {
		return nil, fmt.Errorf("invalid listen port in %q", spec)
	}
	dest := listen
	if len(parts) == 3 {
		if dest, err = strconv.Atoi(parts[2]); err != nil || dest <= 0 {
			return nil, fmt.Errorf("invalid destination port in %q", spec)
		}
	}
	return map[string]any{
		"protocol": parts[0], "listen_port": listen, "destination_port": dest,
		"health_check": map[string]any{"protocol": "tcp", "port": dest},
	}, nil
}

// targetFlags builds one LB target.
type targetFlags struct {
	server       int64
	ip, selector string
	usePrivateIP bool
}

func (f *targetFlags) add(cmd *cobra.Command) {
	cmd.Flags().Int64Var(&f.server, "server", 0, "target a server")
	cmd.Flags().StringVar(&f.ip, "ip", "", "target an IP address")
	cmd.Flags().StringVar(&f.selector, "label-selector", "", `target every server matching a label selector, e.g. "role=web"`)
	cmd.Flags().BoolVar(&f.usePrivateIP, "use-private-ip", false, "reach the target over the private network")
}

func (f *targetFlags) target() (map[string]any, error) {
	n := 0
	t := map[string]any{"use_private_ip": f.usePrivateIP}
	if f.server > 0 {
		n++
		t["type"], t["server_id"] = "server", f.server
	}
	if f.ip != "" {
		n++
		t["type"], t["ip"] = "ip", f.ip
	}
	if f.selector != "" {
		n++
		t["type"], t["label_selector"] = "label_selector", f.selector
	}
	if n != 1 {
		return nil, fmt.Errorf("pass exactly one of --server, --ip or --label-selector")
	}
	return t, nil
}

// parseTargetShorthand reads "server:12", "ip:203.0.113.5" or "label_selector:role=web".
func parseTargetShorthand(spec string) (map[string]any, error) {
	kind, value, ok := strings.Cut(spec, ":")
	if !ok || value == "" {
		return nil, fmt.Errorf("invalid target %q: use server:<id>, ip:<address> or label_selector:<selector>", spec)
	}
	switch kind {
	case "server":
		id, err := parseCloudID(value, "server")
		if err != nil {
			return nil, err
		}
		return map[string]any{"type": "server", "server_id": id}, nil
	case "ip":
		return map[string]any{"type": "ip", "ip": value}, nil
	case "label_selector", "label-selector", "selector":
		return map[string]any{"type": "label_selector", "label_selector": value}, nil
	}
	return nil, fmt.Errorf("invalid target type %q: server, ip or label_selector", kind)
}

func newCloudLoadBalancersCmd() *cobra.Command {
	r := cloudLoadBalancer
	cmd := &cobra.Command{
		Use:     "load-balancers",
		Aliases: []string{"load-balancer", "lb"},
		Short:   "Manage load balancers, their services and targets",
	}

	var (
		services, targets []string
		servicesFile      string
	)
	create := r.createCmd(`  arianet cloud load-balancers create --name web --datacenter eu-central-1 --type lb11 \
      --service tcp:80 --service tcp:443 --target label_selector:role=web --wait
  arianet cloud load-balancers create --name api --datacenter eu-central-1 --network 4 --no-public-interface \
      --services-file services.json --target server:12 --target server:13`,
		func(cmd *cobra.Command) {
			cmd.Flags().String("name", "", "load balancer name")
			cmd.Flags().String("datacenter", "", "datacenter code")
			cmd.Flags().String("type", "", "type (see: arianet cloud load-balancers types)")
			cmd.Flags().String("algorithm", "", "round_robin or least_connections")
			cmd.Flags().Int64("network", 0, "private network to attach")
			cmd.Flags().Bool("no-public-interface", false, "no public address (private network only)")
			cmd.Flags().StringArrayVar(&services, "service", nil, "service protocol:listen[:destination], e.g. tcp:80:8080 (repeatable)")
			cmd.Flags().StringVar(&servicesFile, "services-file", "", "JSON file with an array of services")
			cmd.Flags().StringArrayVar(&targets, "target", nil, "target server:<id>, ip:<address> or label_selector:<selector> (repeatable)")
			_ = cmd.MarkFlagRequired("name")
			_ = cmd.MarkFlagRequired("datacenter")
		}, func(cmd *cobra.Command, body map[string]any) error {
			changedString(cmd, body, "name", "name")
			changedString(cmd, body, "datacenter", "datacenter")
			changedString(cmd, body, "type", "type")
			changedString(cmd, body, "algorithm", "algorithm")
			changedInt64(cmd, body, "network", "network_id")
			if noPublic, _ := cmd.Flags().GetBool("no-public-interface"); noPublic {
				body["public_interface"] = false
			}
			if servicesFile != "" && len(services) > 0 {
				return fmt.Errorf("use --service or --services-file, not both")
			}
			if servicesFile != "" {
				list := []any{}
				if err := readJSONFile(servicesFile, &list); err != nil {
					return err
				}
				body["services"] = list
			}
			if len(services) > 0 {
				list := []any{}
				for _, s := range services {
					svc, err := parseServiceShorthand(s)
					if err != nil {
						return err
					}
					list = append(list, svc)
				}
				body["services"] = list
			}
			if len(targets) > 0 {
				list := []any{}
				for _, t := range targets {
					tg, err := parseTargetShorthand(t)
					if err != nil {
						return err
					}
					list = append(list, tg)
				}
				body["targets"] = list
			}
			return nil
		})

	var addSvc, updSvc serviceFlags
	var addTarget targetFlags

	cmd.AddCommand(
		r.listCmd("  arianet cloud load-balancers list", nil, nil),
		r.getCmd(),
		create,
		r.patchCmd("  arianet cloud load-balancers update 3 --name web-2 --labels env=prod", func(cmd *cobra.Command) {
			cmd.Flags().String("name", "", "new name")
			cmd.Flags().Bool("protection-delete", false, "protect against deletion (true/false)")
		}, func(cmd *cobra.Command, body map[string]any) error {
			changedString(cmd, body, "name", "name")
			changedBool(cmd, body, "protection-delete", "protection_delete")
			return nil
		}),
		r.deleteCmd(),
		newCloudLBTypesCmd(),
		newCloudLBHealthCmd(),
		r.opCmd("add-service", "add_service", "Add a service (listener)",
			`  arianet cloud load-balancers add-service 3 --protocol tcp --listen-port 443 --destination-port 8443
  arianet cloud load-balancers add-service 3 --protocol http --listen-port 80 --health-protocol http --health-path /healthz
  arianet cloud load-balancers add-service 3 --file service.json`,
			addSvc.add, func(cmd *cobra.Command) (any, error) { return addSvc.service() }),
		r.opCmd("update-service", "update_service", "Replace the service listening on a port",
			"  arianet cloud load-balancers update-service 3 --port 80 --protocol tcp --listen-port 80 --destination-port 8081",
			func(cmd *cobra.Command) {
				updSvc.add(cmd)
				cmd.Flags().Int("port", 0, "listen port of the service to change")
				_ = cmd.MarkFlagRequired("port")
			}, func(cmd *cobra.Command) (any, error) {
				svc, err := updSvc.service()
				if err != nil {
					return nil, err
				}
				port, _ := cmd.Flags().GetInt("port")
				return map[string]any{"listen_port": port, "service": svc}, nil
			}),
		r.opCmd("delete-service", "delete_service", "Delete the service listening on a port",
			"  arianet cloud load-balancers delete-service 3 --listen-port 443",
			func(cmd *cobra.Command) {
				cmd.Flags().Int("listen-port", 0, "listen port of the service")
				_ = cmd.MarkFlagRequired("listen-port")
			}, func(cmd *cobra.Command) (any, error) {
				port, _ := cmd.Flags().GetInt("listen-port")
				return map[string]any{"listen_port": port}, nil
			}),
		r.opCmd("set-services", "update_services", "Replace every service at once (from a JSON file)",
			"  arianet cloud load-balancers set-services 3 --services-file services.json --algorithm least_connections",
			func(cmd *cobra.Command) {
				cmd.Flags().String("services-file", "", "JSON file with an array of services")
				cmd.Flags().String("algorithm", "", "also change the algorithm")
				_ = cmd.MarkFlagRequired("services-file")
			}, func(cmd *cobra.Command) (any, error) {
				path, _ := cmd.Flags().GetString("services-file")
				list := []any{}
				if err := readJSONFile(path, &list); err != nil {
					return nil, err
				}
				body := map[string]any{"services": list}
				changedString(cmd, body, "algorithm", "algorithm")
				return body, nil
			}),
		r.opCmd("add-target", "add_target", "Add a target (server, IP or label selector)",
			`  arianet cloud load-balancers add-target 3 --server 12
  arianet cloud load-balancers add-target 3 --label-selector role=web --use-private-ip`,
			addTarget.add, func(cmd *cobra.Command) (any, error) { return addTarget.target() }),
		r.opCmd("remove-target", "remove_target", "Remove a target",
			"  arianet cloud load-balancers remove-target 3 --target-id 17",
			func(cmd *cobra.Command) {
				cmd.Flags().Int64("target-id", 0, "target ID (see: arianet cloud load-balancers get <id>)")
				_ = cmd.MarkFlagRequired("target-id")
			}, func(cmd *cobra.Command) (any, error) {
				id, _ := cmd.Flags().GetInt64("target-id")
				return map[string]any{"target_id": id}, nil
			}),
		r.opCmd("change-type", "change_type", "Change a load balancer's type",
			"  arianet cloud load-balancers change-type 3 --type lb21",
			func(cmd *cobra.Command) {
				cmd.Flags().String("type", "", "new type")
				_ = cmd.MarkFlagRequired("type")
			}, func(cmd *cobra.Command) (any, error) {
				typ, _ := cmd.Flags().GetString("type")
				return map[string]any{"type": typ}, nil
			}),
		r.opCmd("change-algorithm", "change_algorithm", "Change the balancing algorithm",
			"  arianet cloud load-balancers change-algorithm 3 --algorithm least_connections",
			func(cmd *cobra.Command) {
				cmd.Flags().String("algorithm", "", "round_robin or least_connections")
				_ = cmd.MarkFlagRequired("algorithm")
			}, func(cmd *cobra.Command) (any, error) {
				alg, _ := cmd.Flags().GetString("algorithm")
				return map[string]any{"algorithm": alg}, nil
			}),
		r.opCmd("attach-network", "attach_to_network", "Attach a load balancer to a private network",
			"  arianet cloud load-balancers attach-network 3 --network 4",
			func(cmd *cobra.Command) {
				cmd.Flags().Int64("network", 0, "network ID")
				_ = cmd.MarkFlagRequired("network")
			}, func(cmd *cobra.Command) (any, error) {
				id, _ := cmd.Flags().GetInt64("network")
				return map[string]any{"network_id": id}, nil
			}),
		r.opCmd("detach-network", "detach_from_network", "Detach a load balancer from its private network",
			"  arianet cloud load-balancers detach-network 3", nil, nil),
	)
	return cmd
}

func newCloudLBTypesCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "types",
		Short:   "List load balancer types",
		Example: "  arianet cloud load-balancers types",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			raw, ok := dataList("/load-balancer-types", nil)
			if !ok {
				return nil
			}
			showGeneric(raw, []string{"code", "name", "max_services", "max_targets", "max_connections"})
			return nil
		},
	}
}

func newCloudLBHealthCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "health <id>",
		Short:   "Refresh and show the health of a load balancer's targets",
		Example: "  arianet cloud load-balancers health 3",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := parseCloudID(args[0], "load balancer")
			if err != nil {
				return err
			}
			cc := requireCloud()
			var raw json.RawMessage
			if err := cc.Get(cloudLoadBalancer.itemPath(id)+"/health", nil, &raw); err != nil {
				handleAPIError(err)
				return nil
			}
			cloudLoadBalancer.printItem(raw)
			return nil
		},
	}
}
