package commands

import (
	"net/http"

	"github.com/ariaservice/arianet-cli/internal/api"
	"github.com/spf13/cobra"
)

// The sms command group: the SMS product through the gateway's /sms/v1
// (arianet-infra-tasks#592). The wallet, top-ups and tickets are not here:
// they belong to the account, not to the SMS API.

var pageQuery = []queryFlag{{"page", "page", "page number"}, {"per-page", "per_page", "items per page (at most 200)"}}

var messageCols = []col{{header: "ID", key: "id"}, {header: "Recipient", key: "recipient"}, {header: "Sender", key: "sender"},
	{header: "Status", key: "status"}, {header: "Parts", key: "parts"}, {header: "Cost (rial)", key: "cost_rial"}, {header: "Sent", key: "sent_at"}}

var batchCols = []col{{header: "ID", key: "id"}, {header: "Status", key: "status"}, {header: "Recipients", key: "total_recipients"},
	{header: "Delivered", key: "delivered_count"}, {header: "Failed", key: "failed_count"}, {header: "Cost (rial)", key: "total_cost_rial"},
	{header: "Reference", key: "client_reference"}}

func newSMSCmd() *cobra.Command {
	p := &product{name: "arianet sms", prefix: api.SMSPrefix,
		scopes: "sms:messages:read, sms:messages:write, sms:lines:read, sms:templates:read, sms:templates:write"}
	cmd := p.group("sms", "Send SMS and manage lines, templates and blocked recipients",
		`Send SMS through your sender lines and read what was sent and received.`)

	send := p.command(endpoint{use: "send", short: "Send a message to up to 100 recipients, now or at a set time", method: http.MethodPost,
		path: "/messages", rawBody: true, done: "Message accepted.",
		example: `  arianet sms send --from 3 --to 09121234567 --to 09351234567 --body "Your code is 1234"
  arianet sms send --from 3 --to 09121234567,09351234567 --template 7 --reference order-981
  arianet sms send --from 3 --to 09121234567 --body "Reminder" --at 2026-12-01T09:00:00+03:30`,
		body: []bodyFlag{
			{"from", "from", "int", "sender line ID (arianet sms lines list)", true},
			{"to", "recipients", "strings", "recipient mobile number, at most 100", true},
			{"body", "body", "string", "message text (or --template)", false},
			{"template", "template_id", "int", "template ID to send instead of --body (arianet sms templates list)", false},
			{"at", "scheduled_at", "string", "send later, at this ISO 8601 time; the send is then listed by arianet sms scheduled list", false},
			{"reference", "client_reference", "string", "your own reference; a send repeated with the same reference is not sent twice", false},
		}})

	messages := subgroup("messages", "Read sent messages", []string{"message", "msg"},
		p.command(endpoint{use: "list", aliases: []string{"ls"}, short: "List sent messages", method: http.MethodGet, path: "/messages",
			query: append([]queryFlag{
				{"batch", "batch_id", "only the messages of this batch"},
				{"status", "status", "only messages in this status, e.g. delivered or failed"},
				{"to", "to", "only messages to this number"},
			}, pageQuery...), cols: messageCols}),
		p.command(endpoint{use: "get <message-id>", aliases: []string{"show"}, short: "Show one message and its delivery", method: http.MethodGet,
			path: "/messages/{0}", nargs: 1}),
	)

	batches := subgroup("batches", "Read send batches", []string{"batch"},
		p.command(endpoint{use: "list", aliases: []string{"ls"}, short: "List batches", method: http.MethodGet, path: "/batches", query: pageQuery,
			cols: batchCols}),
		p.command(endpoint{use: "get <batch-id>", aliases: []string{"show"}, short: "Show one batch", method: http.MethodGet,
			path: "/batches/{0}", nargs: 1}),
	)

	scheduled := subgroup("scheduled", "List and cancel scheduled sends", []string{"schedule"},
		p.command(endpoint{use: "list", aliases: []string{"ls"}, short: "List scheduled sends", method: http.MethodGet, path: "/scheduled", query: pageQuery}),
		p.command(endpoint{use: "cancel <scheduled-id>", aliases: []string{"delete", "rm"}, short: "Cancel a pending scheduled send", method: http.MethodDelete,
			path: "/scheduled/{0}", nargs: 1, confirm: "Cancel scheduled send #{0}?", done: "Scheduled send #{0} cancelled."}),
	)

	lines := subgroup("lines", "List your sender lines or ask for a dedicated one", []string{"line"},
		p.command(endpoint{use: "list", aliases: []string{"ls"}, short: "List the sender lines you can send from", method: http.MethodGet, path: "/lines",
			query: pageQuery}),
		p.command(endpoint{use: "request", short: "Ask for a dedicated sender line (opens a request with support)", method: http.MethodPost, path: "/lines",
			done: "Line requested.", body: []bodyFlag{
				{"number", "number", "string", "the number you would like (optional)", false},
				{"note", "note", "string", "anything support should know (optional)", false},
			}}),
	)

	templates := subgroup("templates", "Manage message templates", []string{"template"},
		p.command(endpoint{use: "list", aliases: []string{"ls"}, short: "List templates", method: http.MethodGet, path: "/templates", query: pageQuery}),
		p.command(endpoint{use: "get <template-id>", aliases: []string{"show"}, short: "Show one template", method: http.MethodGet,
			path: "/templates/{0}", nargs: 1}),
		p.command(endpoint{use: "create", short: "Create a template", method: http.MethodPost, path: "/templates", done: "Template created.",
			body: []bodyFlag{
				{"name", "name", "string", "template name, unique in your account", true},
				{"body", "body", "string", "template text", true},
			}}),
		p.command(endpoint{use: "update <template-id>", short: "Replace a template's name and text", method: http.MethodPut, path: "/templates/{0}", nargs: 1,
			done: "Template #{0} updated.", body: []bodyFlag{
				{"name", "name", "string", "template name", true},
				{"body", "body", "string", "template text", true},
			}}),
		p.command(endpoint{use: "delete <template-id>", aliases: []string{"rm"}, short: "Delete a template", method: http.MethodDelete,
			path: "/templates/{0}", nargs: 1, confirm: "Delete template #{0}?", done: "Template #{0} deleted."}),
	)

	blocked := subgroup("blocked", "Manage numbers you never send to", []string{"blocked-recipients", "blocklist"},
		p.command(endpoint{use: "list", aliases: []string{"ls"}, short: "List blocked numbers", method: http.MethodGet, path: "/blocked-recipients",
			query: append([]queryFlag{{"phone", "phone", "only numbers containing this"}}, pageQuery...)}),
		p.command(endpoint{use: "add", short: "Block a number", method: http.MethodPost, path: "/blocked-recipients", done: "Number blocked.",
			body: []bodyFlag{
				{"phone", "phone", "string", "mobile number", true},
				{"note", "note", "string", "why (optional)", false},
			}}),
		p.command(endpoint{use: "delete <id>", aliases: []string{"rm", "unblock"}, short: "Unblock a number", method: http.MethodDelete,
			path: "/blocked-recipients/{0}", nargs: 1, done: "Number unblocked."}),
	)

	webhook := subgroup("webhook", "Manage the delivery-report webhook", nil,
		p.command(endpoint{use: "get", aliases: []string{"show"}, short: "Show the webhook URL and its signing secret", method: http.MethodGet, path: "/webhook"}),
		p.command(endpoint{use: "set", short: "Set the webhook URL (an empty --url turns it off)", method: http.MethodPut, path: "/webhook", done: "Webhook updated.",
			body: []bodyFlag{{"url", "url", "string", "HTTPS URL that receives delivery reports", true}}}),
		p.command(endpoint{use: "rotate-secret", short: "Replace the webhook signing secret", method: http.MethodPost, path: "/webhook/secret",
			confirm: "Replace the webhook signing secret? Your receiver must switch to the new one.", done: "Webhook secret replaced."}),
	)

	cmd.AddCommand(
		send, messages, batches, scheduled, lines, templates, blocked, webhook,
		p.command(endpoint{use: "inbound", short: "List messages received on your lines", method: http.MethodGet, path: "/inbound", query: pageQuery}),
		p.command(endpoint{use: "report", short: "Show the sending summary (sent, delivered, failed, spend)", method: http.MethodGet, path: "/reports/summary",
			query: []queryFlag{{"from", "from", "first day, e.g. 2026-11-01"}, {"to", "to", "last day, e.g. 2026-11-30"}}}),
		p.command(endpoint{use: "prices", short: "Show the price per message and its volume tiers", method: http.MethodGet, path: "/prices"}),
		p.apiCmd(),
	)
	return cmd
}
