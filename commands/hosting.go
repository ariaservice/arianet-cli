package commands

import (
	"net/http"

	"github.com/ariaservice/arianet-cli/internal/api"
	"github.com/spf13/cobra"
)

// The hosting command group: shared hosting (cPanel/DirectAdmin) through
// the gateway's /hosting/v1 (arianet-infra-tasks#587).

var hostingAccountCols = []col{
	{"ID", "id"}, {"Domain", "domain"}, {"State", "state"}, {"Kind", "kind"}, {"Plan", "plan_id"},
	{"Datacenter", "datacenter"}, {"Region", "region"}, {"IP", "ip_address"}, {"Renews", "renews_at"},
}

func newHostingCmd() *cobra.Command {
	p := &product{name: "arianet hosting", prefix: api.HostingPrefix,
		scopes: "hosting:accounts:read (reads), hosting:accounts:write (changes)", actions: true}
	cmd := p.group("hosting", "Manage shared hosting accounts (cPanel / DirectAdmin)",
		`Manage shared hosting: order and manage accounts, DNS records, mailboxes,
databases, SSH keys, and follow operations.`)

	accounts := subgroup("accounts", "Order and manage hosting accounts", []string{"account"},
		p.command(endpoint{use: "list", aliases: []string{"ls"}, short: "List your hosting accounts", method: http.MethodGet,
			path: "/accounts", cols: hostingAccountCols}),
		p.command(endpoint{use: "get <account-id>", aliases: []string{"show"}, short: "Show one hosting account", method: http.MethodGet,
			path: "/accounts/{0}", nargs: 1}),
		p.command(endpoint{use: "create", short: "Order a hosting account (charged to your wallet)", method: http.MethodPost,
			path: "/accounts", rawBody: true, done: "Hosting account ordered.",
			example: "  arianet hosting accounts create --datacenter ir-thr-1 --plan-id 3 --term monthly --domain example.com --password 'S3cret!pass'",
			body: []bodyFlag{
				{"datacenter", "datacenter", "string", "datacenter code (arianet hosting datacenters)", true},
				{"plan-id", "plan_id", "int", "plan ID (arianet hosting plans)", true},
				{"term", "term", "string", "billing term, e.g. monthly or yearly", true},
				{"domain", "domain", "string", "the account's main domain", true},
				{"password", "password", "string", "the control panel password", true},
				{"contact-email", "contact_email", "string", "contact email of the account", false},
				{"project-id", "project_id", "int", "project to put it in", false},
			}}),
		p.command(endpoint{use: "delete <account-id>", aliases: []string{"rm"}, short: "Terminate an account and delete its data", method: http.MethodDelete,
			path: "/accounts/{0}", nargs: 1, confirm: "Terminate hosting account #{0} and delete all its data?", done: "Termination of hosting account #{0} accepted."}),
		p.command(endpoint{use: "auto-renew <account-id>", short: "Turn auto-renew on or off", method: http.MethodPatch,
			path: "/accounts/{0}", nargs: 1, done: "Auto-renew of hosting account #{0} updated.",
			body: []bodyFlag{{"enabled", "auto_renew", "bool", "renew automatically (--enabled=false turns it off)", true}}}),
		p.command(endpoint{use: "change-plan <account-id>", short: "Move the account to another plan", method: http.MethodPost,
			path: "/accounts/{0}/actions/change_plan", nargs: 1, done: "Plan change of hosting account #{0} accepted.",
			body: []bodyFlag{{"plan-id", "plan_id", "int", "the new plan ID", true}}}),
		p.command(endpoint{use: "renew <account-id>", short: "Renew a lapsed term", method: http.MethodPost,
			path: "/accounts/{0}/actions/renew", nargs: 1, done: "Renewal of hosting account #{0} accepted."}),
		p.command(endpoint{use: "password <account-id>", short: "Change the account's control panel / FTP / SSH password", method: http.MethodPost,
			path: "/accounts/{0}/password", nargs: 1, done: "Password of hosting account #{0} changed.",
			body: []bodyFlag{{"password", "password", "string", "the new password", true}}}),
		p.command(endpoint{use: "login <account-id>", short: "Get a one-time login link into the control panel", method: http.MethodPost,
			path: "/accounts/{0}/login", nargs: 1, done: "One-time login link (open it at once):",
			body: []bodyFlag{{"target", "target", "string", "where to land, e.g. reseller for a reseller's panel", false}}}),
		p.command(endpoint{use: "usage <account-id>", short: "Show disk and bandwidth use", method: http.MethodGet, path: "/accounts/{0}/usage", nargs: 1}),
		p.command(endpoint{use: "nameservers <account-id>", short: "Show where the domain must point", method: http.MethodGet, path: "/accounts/{0}/nameservers", nargs: 1}),
		p.command(endpoint{use: "traffic <account-id>", short: "Show the current period's traffic against the quota", method: http.MethodGet, path: "/accounts/{0}/traffic", nargs: 1}),
		p.command(endpoint{use: "actions <account-id>", short: "List the account's operations", method: http.MethodGet, path: "/accounts/{0}/actions", nargs: 1,
			query: []queryFlag{{"limit", "limit", "at most this many"}}}),
	)

	dns := subgroup("dns", "Manage the DNS zone of an account's domain", []string{"dns-records"},
		p.command(endpoint{use: "list <account-id>", aliases: []string{"ls"}, short: "List the zone's records", method: http.MethodGet,
			path: "/accounts/{0}/dns-records", nargs: 1}),
		p.command(endpoint{use: "add <account-id>", short: "Add a record", method: http.MethodPost, path: "/accounts/{0}/dns-records", nargs: 1,
			done: "Record added.", example: "  arianet hosting dns add 12 --type A --name www --content 203.0.113.10 --ttl 300",
			body: dnsFlags()}),
		p.command(endpoint{use: "update <account-id> <record>", short: "Change a record", method: http.MethodPut, path: "/accounts/{0}/dns-records/{1}", nargs: 2,
			done: "Record {1} updated.", body: dnsFlags()}),
		p.command(endpoint{use: "delete <account-id> <record>", aliases: []string{"rm"}, short: "Delete a record", method: http.MethodDelete,
			path: "/accounts/{0}/dns-records/{1}", nargs: 2, confirm: "Delete DNS record {1}?", done: "Record {1} deleted."}),
	)

	email := subgroup("email", "Manage mailboxes on an account's domain", []string{"mailboxes"},
		p.command(endpoint{use: "list <account-id>", aliases: []string{"ls"}, short: "List mailboxes", method: http.MethodGet,
			path: "/accounts/{0}/email-accounts", nargs: 1}),
		p.command(endpoint{use: "add <account-id>", short: "Add a mailbox", method: http.MethodPost, path: "/accounts/{0}/email-accounts", nargs: 1,
			done: "Mailbox added.", body: []bodyFlag{
				{"local-part", "local_part", "string", "the part before @", true},
				{"password", "password", "string", "the mailbox password", true},
				{"quota-mb", "quota_mb", "int", "quota in MB", false},
			}}),
		p.command(endpoint{use: "update <account-id> <local-part>", short: "Change a mailbox's password and/or quota", method: http.MethodPut,
			path: "/accounts/{0}/email-accounts/{1}", nargs: 2, done: "Mailbox {1} updated.", body: []bodyFlag{
				{"password", "password", "string", "the new password", false},
				{"quota-mb", "quota_mb", "int", "the new quota in MB", false},
			}}),
		p.command(endpoint{use: "delete <account-id> <local-part>", aliases: []string{"rm"}, short: "Delete a mailbox", method: http.MethodDelete,
			path: "/accounts/{0}/email-accounts/{1}", nargs: 2, confirm: "Delete mailbox {1} and its mail?", done: "Mailbox {1} deleted."}),
	)

	databases := subgroup("databases", "Manage MySQL databases and users", []string{"db"},
		p.command(endpoint{use: "list <account-id>", aliases: []string{"ls"}, short: "List databases", method: http.MethodGet,
			path: "/accounts/{0}/databases", nargs: 1}),
		p.command(endpoint{use: "create <account-id>", short: "Create a database", method: http.MethodPost, path: "/accounts/{0}/databases", nargs: 1,
			done: "Database created.", body: []bodyFlag{{"name", "name", "string", "database name", true}}}),
		p.command(endpoint{use: "delete <account-id> <name>", aliases: []string{"rm"}, short: "Delete a database", method: http.MethodDelete,
			path: "/accounts/{0}/databases/{1}", nargs: 2, confirm: "Delete database {1} and its data?", done: "Database {1} deleted."}),
		p.command(endpoint{use: "grant <account-id> <name>", short: "Give a user all privileges on a database", method: http.MethodPost,
			path: "/accounts/{0}/databases/{1}/grant", nargs: 2, done: "Privileges on {1} granted.", body: []bodyFlag{
				{"user", "user", "string", "database user", true},
				{"password", "password", "string", "the user's password (panels that create the user here)", false},
			}}),
		p.command(endpoint{use: "users <account-id>", short: "List database users", method: http.MethodGet, path: "/accounts/{0}/database-users", nargs: 1}),
		p.command(endpoint{use: "user-create <account-id>", short: "Create a database user", method: http.MethodPost, path: "/accounts/{0}/database-users", nargs: 1,
			done: "Database user created.", body: []bodyFlag{
				{"name", "name", "string", "user name", true},
				{"password", "password", "string", "the user's password", true},
			}}),
		p.command(endpoint{use: "user-password <account-id> <user>", short: "Set a database user's password", method: http.MethodPut,
			path: "/accounts/{0}/database-users/{1}", nargs: 2, done: "Password of {1} changed.",
			body: []bodyFlag{{"password", "password", "string", "the new password", true}}}),
		p.command(endpoint{use: "user-delete <account-id> <user>", short: "Delete a database user", method: http.MethodDelete,
			path: "/accounts/{0}/database-users/{1}", nargs: 2, confirm: "Delete database user {1}?", done: "Database user {1} deleted."}),
	)

	sshKeys := subgroup("ssh-keys", "Manage SSH keys of an account", []string{"ssh-key"},
		p.command(endpoint{use: "list <account-id>", aliases: []string{"ls"}, short: "List SSH keys", method: http.MethodGet,
			path: "/accounts/{0}/ssh-keys", nargs: 1}),
		p.command(endpoint{use: "add <account-id>", short: "Add and authorize a public key", method: http.MethodPost, path: "/accounts/{0}/ssh-keys", nargs: 1,
			done: "SSH key added.", body: []bodyFlag{
				{"name", "name", "string", "key name", true},
				{"public-key", "public_key", "string", "the public key (ssh-ed25519 ...)", true},
			}}),
		p.command(endpoint{use: "delete <account-id> <name>", aliases: []string{"rm"}, short: "Deauthorize and delete a key", method: http.MethodDelete,
			path: "/accounts/{0}/ssh-keys/{1}", nargs: 2, confirm: "Delete SSH key {1}?", done: "SSH key {1} deleted."}),
	)

	cmd.AddCommand(
		p.command(endpoint{use: "datacenters", short: "List the region's hosting datacenters", method: http.MethodGet, path: "/datacenters"}),
		p.command(endpoint{use: "plans", short: "List hosting plans for sale", method: http.MethodGet, path: "/plans",
			query: []queryFlag{{"datacenter", "datacenter", "only plans of this datacenter"}, {"kind", "kind", "shared or reseller"}}}),
		accounts, dns, email, databases, sshKeys,
		p.actionsCmd(),
		p.apiCmd(),
	)
	return cmd
}

func dnsFlags() []bodyFlag {
	return []bodyFlag{
		{"type", "type", "string", "record type: A, AAAA, CNAME, MX, TXT...", true},
		{"name", "name", "string", "record name, e.g. www", true},
		{"content", "content", "string", "record value", true},
		{"ttl", "ttl", "int", "TTL in seconds", false},
		{"priority", "priority", "int", "priority (MX)", false},
	}
}
