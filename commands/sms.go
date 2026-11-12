package commands

import (
	"net/http"

	"github.com/ariaservice/arianet-cli/internal/api"
	"github.com/spf13/cobra"
)

// The sms command group: the SMS product through the gateway's /sms/v1
// (arianet-infra-tasks#592). The wallet, top-ups and tickets are not here:
// they belong to the account, not to the SMS API.

var pageQuery = []queryFlag{{"page", "page", "page number"}, {"per-page", "per_page", "items per page"}}

func newSMSCmd() *cobra.Command {
	p := &product{name: "arianet sms", prefix: api.SMSPrefix,
		scopes: "sms:messages:read, sms:messages:write, sms:lines:read, sms:templates:read, sms:templates:write"}
	cmd := p.group("sms", "Send SMS and manage lines, templates and blocked recipients",
		`Send SMS through your sender lines and read what was sent and received.`)

	send := p.command(endpoint{use: "send", short: "Send a message to one or more recipients (charged per part)", method: http.MethodPost,
		path: "/messages", rawBody: true, done: "Message accepted.",
		example: `  arianet sms send --line 3 --to 09121234567 --to 09351234567 --text "Your code is 1234"
  arianet sms send --line 3 --to 09121234567,09351234567 --text "Hello" --reference order-981`,
		body: []bodyFlag{
			{"line", "sender_line_id", "int", "sender line ID (arianet sms lines list)", true},
			{"to", "recipients", "strings", "recipient mobile number", true},
			{"text", "messages", "texts", "message text (one per recipient, or one for all)", true},
			{"reference", "client_reference", "string", "your own reference for this send", false},
		}})

	messages := subgroup("messages", "Read sent messages", []string{"message", "msg"},
		p.command(endpoint{use: "list", aliases: []string{"ls"}, short: "List sent messages", method: http.MethodGet, path: "/messages",
			query: pageQuery}),
		p.command(endpoint{use: "get <message-id>", aliases: []string{"show"}, short: "Show one message and its delivery", method: http.MethodGet,
			path: "/messages/{0}", nargs: 1}),
	)

	batches := subgroup("batches", "Read send batches", []string{"batch"},
		p.command(endpoint{use: "list", aliases: []string{"ls"}, short: "List batches", method: http.MethodGet, path: "/batches", query: pageQuery}),
		p.command(endpoint{use: "get <batch-id>", aliases: []string{"show"}, short: "Show one batch", method: http.MethodGet,
			path: "/batches/{0}", nargs: 1}),
	)

	lines := subgroup("lines", "List your sender lines", []string{"line"},
		p.command(endpoint{use: "list", aliases: []string{"ls"}, short: "List the sender lines you can send from", method: http.MethodGet, path: "/lines"}),
	)

	templates := subgroup("templates", "Manage message templates", []string{"template"},
		p.command(endpoint{use: "list", aliases: []string{"ls"}, short: "List templates", method: http.MethodGet, path: "/templates"}),
		p.command(endpoint{use: "create", short: "Create a template", method: http.MethodPost, path: "/templates", done: "Template created.",
			body: []bodyFlag{
				{"name", "name", "string", "template name", true},
				{"body", "body", "string", "template text", true},
			}}),
		p.command(endpoint{use: "update <template-id>", short: "Change a template", method: http.MethodPatch, path: "/templates/{0}", nargs: 1,
			done: "Template #{0} updated.", body: []bodyFlag{
				{"name", "name", "string", "template name", true},
				{"body", "body", "string", "template text", true},
			}}),
		p.command(endpoint{use: "delete <template-id>", aliases: []string{"rm"}, short: "Delete a template", method: http.MethodDelete,
			path: "/templates/{0}", nargs: 1, confirm: "Delete template #{0}?", done: "Template #{0} deleted."}),
	)

	blocked := subgroup("blocked", "Manage numbers you never send to", []string{"blocked-recipients", "blocklist"},
		p.command(endpoint{use: "list", aliases: []string{"ls"}, short: "List blocked numbers", method: http.MethodGet, path: "/blocked-recipients", query: pageQuery}),
		p.command(endpoint{use: "add", short: "Block a number", method: http.MethodPost, path: "/blocked-recipients", done: "Number blocked.",
			body: []bodyFlag{
				{"phone", "phone", "string", "mobile number", true},
				{"note", "note", "string", "why (optional)", false},
			}}),
		p.command(endpoint{use: "delete <id>", aliases: []string{"rm", "unblock"}, short: "Unblock a number", method: http.MethodDelete,
			path: "/blocked-recipients/{0}", nargs: 1, done: "Number unblocked."}),
	)

	cmd.AddCommand(
		send, messages, batches, lines, templates, blocked,
		p.command(endpoint{use: "inbound", short: "List messages received on your lines", method: http.MethodGet, path: "/inbound", query: pageQuery}),
		p.command(endpoint{use: "scheduled", short: "List scheduled sends", method: http.MethodGet, path: "/scheduled", query: pageQuery}),
		p.command(endpoint{use: "report", short: "Show the sending summary", method: http.MethodGet, path: "/reports/summary"}),
		p.apiCmd(),
	)
	return cmd
}
