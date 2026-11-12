package commands

import (
	"net/http"

	"github.com/ariaservice/arianet-cli/internal/api"
	"github.com/spf13/cobra"
)

// The baremetal command group: dedicated servers through the gateway's
// /baremetal/v1 (arianet-infra-tasks#590).

var baremetalServerCols = []col{
	{"ID", "id"}, {"Name", "name"}, {"State", "state"}, {"Plan", "plan.name"},
	{"Datacenter", "datacenter.code"}, {"IP", "ip"}, {"Expires", "expires_at"},
}

var baremetalOrderCols = []col{
	{"ID", "id"}, {"Stage", "stage"}, {"Plan", "plan_name"}, {"Datacenter", "datacenter"},
	{"Monthly", "monthly"}, {"Currency", "currency"}, {"Server", "server_id"}, {"Created", "created_at"},
}

func newBaremetalCmd() *cobra.Command {
	p := &product{name: "arianet baremetal", prefix: api.BaremetalPrefix,
		scopes: "baremetal:servers:read (reads), baremetal:servers:write (orders, renewals, reinstalls)"}
	cmd := p.group("baremetal", "Order and manage dedicated (baremetal) servers",
		`Order and manage dedicated servers: browse the catalog, price a
configuration, order, renew, reveal credentials and reinstall.`, "dedicated")

	configFlags := func(withRules bool) []bodyFlag {
		f := []bodyFlag{
			{"catalog", "catalog", "string", "catalog of the plan (arianet baremetal plans)", true},
			{"plan-code", "plan_code", "string", "plan code", true},
			{"datacenter", "datacenter", "string", "datacenter (arianet baremetal options)", true},
			{"memory", "memory", "string", "memory option", true},
			{"storage", "storage", "string", "storage option", true},
			{"bandwidth", "bandwidth", "string", "bandwidth option", true},
			{"hostname", "hostname", "string", "hostname", false},
		}
		if withRules {
			f = append(f,
				bodyFlag{"rules-version", "rules_version", "string", "the rules version you accept (arianet baremetal rules)", true},
				bodyFlag{"accept-rules", "accept_rules", "bool", "accept the dedicated server rules", true},
			)
		}
		return f
	}

	orders := subgroup("orders", "Order servers and follow orders", []string{"order"},
		p.command(endpoint{use: "list", aliases: []string{"ls"}, short: "List your orders", method: http.MethodGet,
			path: "/orders", cols: baremetalOrderCols}),
		p.command(endpoint{use: "get <order-id>", aliases: []string{"show"}, short: "Show one order with its stages", method: http.MethodGet,
			path: "/orders/{0}", nargs: 1}),
		p.command(endpoint{use: "create", short: "Order a server (held on your wallet)", method: http.MethodPost,
			path: "/orders", rawBody: true, body: configFlags(true), done: "Server ordered.",
			confirm: "Order this dedicated server? The first payment is held on your wallet.",
			example: "  arianet baremetal orders create --catalog eco --plan-code 24rise01 --datacenter fra --memory ram-32g --storage softraid-2x512nvme --bandwidth 1gbps --rules-version 2026-10 --accept-rules"}),
		p.command(endpoint{use: "cancel <order-id>", short: "Cancel an order before procurement started", method: http.MethodPost,
			path: "/orders/{0}/cancel", nargs: 1, confirm: "Cancel order #{0}?", done: "Order #{0} cancelled."}),
	)

	servers := subgroup("servers", "Manage delivered servers", []string{"server"},
		p.command(endpoint{use: "list", aliases: []string{"ls"}, short: "List your servers", method: http.MethodGet,
			path: "/servers", cols: baremetalServerCols}),
		p.command(endpoint{use: "get <server-id>", aliases: []string{"show"}, short: "Show one server", method: http.MethodGet,
			path: "/servers/{0}", nargs: 1}),
		p.command(endpoint{use: "credentials <server-id>", short: "Reveal the login details (shown once)", method: http.MethodPost,
			path: "/servers/{0}/credentials/reveal", nargs: 1, confirm: "Reveal the login details of server #{0}? They are shown only once.",
			done: "Login details of server #{0} (shown only once - save them now):"}),
		p.command(endpoint{use: "renewals <server-id>", short: "List the server's renewals", method: http.MethodGet,
			path: "/servers/{0}/renewals", nargs: 1}),
		p.command(endpoint{use: "renew <server-id>", short: "Pay the open renewal (charged at once)", method: http.MethodPost,
			path: "/servers/{0}/renew", nargs: 1, confirm: "Pay the next month of server #{0} now?", done: "Server #{0} renewed."}),
		p.command(endpoint{use: "os-templates <server-id>", short: "List operating systems the server can be installed with", method: http.MethodGet,
			path: "/servers/{0}/os-templates", nargs: 1}),
		p.command(endpoint{use: "reinstall <server-id>", short: "Install an operating system (the disks are wiped)", method: http.MethodPost,
			path: "/servers/{0}/reinstall", nargs: 1, confirm: "Reinstall server #{0}? Its disks are wiped.", done: "Installation on server #{0} started.",
			body: []bodyFlag{
				{"template", "template", "string", "OS template (os-templates)", true},
				{"hostname", "hostname", "string", "hostname", false},
				{"ssh-key", "ssh_key", "string", "an SSH public key to install", false},
			}}),
		p.command(endpoint{use: "reinstall-status <server-id>", short: "Show the last installation's state", method: http.MethodGet,
			path: "/servers/{0}/reinstall", nargs: 1}),
	)

	cmd.AddCommand(
		p.command(endpoint{use: "suppliers", short: "List the companies servers are bought from", method: http.MethodGet, path: "/suppliers"}),
		p.command(endpoint{use: "ranges", short: "List server families with their cheapest price", method: http.MethodGet, path: "/ranges",
			query: []queryFlag{{"supplier", "supplier", "only this supplier"}}}),
		p.command(endpoint{use: "plans", short: "List server models", method: http.MethodGet, path: "/plans",
			query: []queryFlag{{"range", "range", "only this family"}, {"supplier", "supplier", "only this supplier"}}}),
		p.command(endpoint{use: "options <catalog> <plan-code>", short: "Show a model's datacenters, memory, storage and bandwidth with prices",
			method: http.MethodGet, path: "/plans/{0}/{1}/options", nargs: 2,
			query: []queryFlag{{"datacenter", "datacenter", "only this datacenter"}, {"memory", "memory", "price with this memory"}}}),
		p.command(endpoint{use: "rules", short: "Show the rules an order must accept", method: http.MethodGet, path: "/rules"}),
		p.command(endpoint{use: "quote", short: "Price a configuration (nothing is held)", method: http.MethodPost, path: "/quote",
			rawBody: true, body: configFlags(false), done: "Quote:"}),
		orders, servers,
		p.apiCmd(),
	)
	return cmd
}
