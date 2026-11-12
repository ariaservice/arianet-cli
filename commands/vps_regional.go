package commands

import (
	"net/http"

	"github.com/ariaservice/arianet-cli/internal/api"
	"github.com/spf13/cobra"
)

// `arianet vps regional`: the regional VPS product (vps-service, iran-1)
// through the gateway's /vps/v1 (arianet-infra-tasks#588).
//
// It lives under the existing `arianet vps` group rather than replacing it:
// `arianet vps list|create|...` keeps calling the original /api/v1/servers
// for every script that uses it, and a top-level name of its own would be a
// second "VPS" command for the same product in the panel. `regional`
// collides with none of the existing subcommands.

var regionalServerCols = []col{
	{"ID", "id"}, {"Hostname", "hostname"}, {"State", "state"}, {"Power", "power_state"},
	{"Region", "region"}, {"Datacenter", "datacenter"}, {"Plan", "plan_id"}, {"IPv4", "ipv4"}, {"Billing", "billing_mode"},
}

func newVPSRegionalCmd() *cobra.Command {
	p := &product{name: "arianet vps regional", prefix: api.VPSPrefix,
		scopes: "vps:servers:read (reads), vps:servers:write (changes)", actions: true}
	cmd := p.group("regional", "Manage regional VPS servers (e.g. iran-1) through /vps/v1",
		`Manage VPS servers of the regional VPS product (iran-1): order, power,
reinstall, resize, console and traffic.`, "region-vps")

	op := func(use, op, short string) *cobra.Command {
		return p.command(endpoint{use: use + " <server-id>", short: short, method: http.MethodPost,
			path: "/servers/{0}/actions/" + op, nargs: 1, done: capitalize(use) + " accepted for server #{0}."})
	}

	servers := subgroup("servers", "Order and manage servers", []string{"server"},
		p.command(endpoint{use: "list", aliases: []string{"ls"}, short: "List your servers", method: http.MethodGet,
			path: "/servers", cols: regionalServerCols}),
		p.command(endpoint{use: "get <server-id>", aliases: []string{"show"}, short: "Show one server", method: http.MethodGet,
			path: "/servers/{0}", nargs: 1}),
		p.command(endpoint{use: "create", short: "Order a server (charged to your wallet)", method: http.MethodPost,
			path: "/servers", rawBody: true, done: "Server ordered.",
			example: "  arianet vps regional servers create --datacenter thr-1 --plan-id 4 --image ubuntu-24.04 --hostname web-1 --billing-mode hourly --wait",
			body: []bodyFlag{
				{"datacenter", "datacenter", "string", "datacenter code (arianet vps regional datacenters)", true},
				{"plan-id", "plan_id", "int", "plan ID (arianet vps regional plans)", true},
				{"image", "image", "string", "image (arianet vps regional images)", true},
				{"hostname", "hostname", "string", "server hostname", true},
				{"billing-mode", "billing_mode", "string", "hourly or monthly", true},
				{"password", "password", "string", "root password (default: generated and shown once)", false},
				{"project-id", "project_id", "int", "project to put it in", false},
			}}),
		p.command(endpoint{use: "delete <server-id>", aliases: []string{"rm"}, short: "Delete a server and its disk", method: http.MethodDelete,
			path: "/servers/{0}", nargs: 1, confirm: "Delete server #{0} and its disk?", done: "Deletion of server #{0} accepted.",
			body: []bodyFlag{{"reason", "reason", "string", "why (optional)", false}}}),
		op("start", "start", "Power a server on"),
		op("stop", "stop", "Shut a server down gracefully"),
		op("restart", "restart", "Restart a server"),
		p.command(endpoint{use: "kill <server-id>", short: "Hard power-off (like pulling the plug)", method: http.MethodPost,
			path: "/servers/{0}/actions/kill", nargs: 1, confirm: "Hard power-off server #{0}? Unsaved data is lost.", done: "Power-off accepted for server #{0}."}),
		p.command(endpoint{use: "reinstall <server-id>", short: "Reinstall from an image (the disk is wiped)", method: http.MethodPost,
			path: "/servers/{0}/reinstall", nargs: 1, confirm: "Reinstall server #{0}? Its disk is wiped.", done: "Reinstall of server #{0} accepted.",
			body: []bodyFlag{
				{"image", "image", "string", "the image", true},
				{"password", "password", "string", "root password (default: generated and shown once)", false},
			}}),
		p.command(endpoint{use: "resize <server-id>", short: "Move to another plan (disks only grow)", method: http.MethodPost,
			path: "/servers/{0}/resize", nargs: 1, done: "Resize of server #{0} accepted.",
			body: []bodyFlag{{"plan-id", "plan_id", "int", "the new plan ID", true}}}),
		p.command(endpoint{use: "reset-password <server-id>", short: "Set the root password (generated when not given)", method: http.MethodPost,
			path: "/servers/{0}/reset-password", nargs: 1, done: "Password reset accepted for server #{0}.",
			body: []bodyFlag{{"password", "password", "string", "the new root password", false}}}),
		p.command(endpoint{use: "console <server-id>", short: "Get a one-time browser console URL", method: http.MethodPost,
			path: "/servers/{0}/console", nargs: 1, done: "Console URL (works once, open it at once):"}),
		p.command(endpoint{use: "traffic <server-id>", short: "Show network traffic and the period's quota", method: http.MethodGet,
			path: "/servers/{0}/traffic", nargs: 1, query: []queryFlag{
				{"from", "from", "start (RFC 3339)"}, {"to", "to", "end (RFC 3339)"}, {"granularity", "granularity", "hour or day"},
			}}),
		p.command(endpoint{use: "actions <server-id>", short: "List the server's operations", method: http.MethodGet,
			path: "/servers/{0}/actions", nargs: 1, query: []queryFlag{{"limit", "limit", "at most this many"}}}),
	)

	cmd.AddCommand(
		p.command(endpoint{use: "datacenters", short: "List the region's datacenters", method: http.MethodGet, path: "/datacenters"}),
		p.command(endpoint{use: "plans", short: "List plans for sale", method: http.MethodGet, path: "/plans",
			query: []queryFlag{{"datacenter", "datacenter", "only plans of this datacenter"}}}),
		p.command(endpoint{use: "images", short: "List operating system images", method: http.MethodGet, path: "/images",
			query: []queryFlag{{"datacenter", "datacenter", "only images of this datacenter"}}}),
		servers,
		p.actionsCmd(),
		p.apiCmd(),
	)
	return cmd
}
