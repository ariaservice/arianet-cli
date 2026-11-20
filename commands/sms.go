package commands

import (
	"net/http"

	"github.com/ariaservice/arianet-cli/internal/api"
	"github.com/ariaservice/arianet-cli/internal/printer"
	"github.com/spf13/cobra"
)

// The sms command group: the SMS product through the gateway's /sms/v1
// (arianet-infra-tasks#592). The wallet, top-ups and tickets are not here:
// they belong to the account, not to the SMS API.

var pageQuery = []queryFlag{{"page", "page", "page number"}, {"per-page", "per_page", "items per page (at most 200)"}}

var messageCols = []col{{header: "ID", key: "id"}, {header: "To", key: "to"}, {header: "From", key: "from"},
	{header: "Status", key: "status"}, {header: "Parts", key: "parts"}, {header: "Cost (rial)", key: "cost_rial"}, {header: "Sent", key: "sent_at"}}

// Field names as sms-service's /v1 returns them on GET /batches.
var batchCols = []col{{header: "ID", key: "id"}, {header: "Status", key: "status"}, {header: "From", key: "from"},
	{header: "Recipients", key: "recipients"}, {header: "Delivered", key: "delivered"}, {header: "Failed", key: "failed"},
	{header: "Cost (rial)", key: "cost_rial"}, {header: "Created", key: "created_at"}}

func newSMSCmd() *cobra.Command {
	p := &product{name: "arianet sms", prefix: api.SMSPrefix,
		scopes: "sms:messages:read, sms:messages:write, sms:lines:read, sms:templates:read, sms:templates:write"}
	cmd := p.group("sms", "Send verification codes and service notices from approved templates",
		`Send verification codes and service notices to your own users through your sender lines,
and read what was sent and received. The service carries service messages only: every message
is an approved template (arianet sms templates create, reviewed within minutes) whose {placeholders}
are filled with short values (one line, at most 40 characters, no link or phone number). Advertising
is not accepted. A new account submits its activation request first (arianet sms application submit).`)

	application := subgroup("application", "Your activation request: what you send and to whom", []string{"activation"},
		p.command(endpoint{use: "get", aliases: []string{"show", "status"}, short: "Show whether the account is active and its latest request", method: http.MethodGet, path: "/application"}),
		p.command(endpoint{use: "submit", short: "Submit the activation request (reviewed before the account can send)", method: http.MethodPost,
			path: "/application", done: "Activation request submitted; you will be told the answer.",
			example: `  arianet sms application submit --brand "Acme Shop" --website acme.example --use-case otp --use-case transactional \
    --description "Login codes and order status messages to customers registered in our shop" --accept-terms`,
			body: []bodyFlag{
				{"brand", "brand_name", "string", "brand name that appears in your messages", true},
				{"website", "website", "string", "your website or app; links in templates may only point there", false},
				{"use-case", "use_cases", "strings", "otp, transactional or alerts", true},
				{"description", "description", "string", "what you send, to whom, and why they expect it (at least 20 characters)", true},
				{"sample", "sample_message", "string", "a sample message", false},
				{"accept-terms", "accept_terms", "bool", "you send service messages to your own users only, never advertising", true},
			}}),
	)

	lookup := p.command(endpoint{use: "lookup", short: "Send an approved template to one recipient (verification codes, notices)", method: http.MethodPost,
		path: "/verify/lookup", done: "Message accepted.",
		example: `  arianet sms lookup --to 09121234567 --template login --token code=48213
  arianet sms lookup --to 09121234567 --template 7 --token order=A-1001 --token date=1405/09/01 --from 3`,
		body: []bodyFlag{
			{"to", "receptor", "string", "recipient mobile number", true},
			{"template", "template", "string", "approved template name or ID", true},
			{"token", "tokens", "pairs", "value of a {placeholder} of the template", false},
			{"from", "from", "string", "sender line number or ID; the account's default line when not given", false},
			{"reference", "client_reference", "string", "your own reference; a send repeated with the same reference is not sent twice", false},
		}})

	send := p.command(endpoint{use: "send", short: "Send an approved template to up to 100 recipients, now or at a set time", method: http.MethodPost,
		path: "/messages", rawBody: true, done: "Message accepted.",
		example: `  arianet sms send --from 3 --to 09121234567,09351234567 --template 7 --token order=A-1001 --reference order-981
  arianet sms send --from 3 --to 09121234567 --template renewal --token date=1405/09/01 --at 2026-12-01T09:00:00+03:30`,
		body: []bodyFlag{
			{"from", "from", "int", "sender line ID (arianet sms lines list)", true},
			{"to", "recipients", "strings", "recipient mobile number, at most 100", true},
			{"template", "template_id", "string", "approved template ID or name (arianet sms templates list)", true},
			{"token", "tokens", "pairs", "value of a {placeholder} of the template", false},
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
		p.command(endpoint{use: "list", aliases: []string{"ls"}, short: "List templates and their review status", method: http.MethodGet, path: "/templates", query: pageQuery,
			cols: []col{{header: "ID", key: "id"}, {header: "Name", key: "name"}, {header: "Category", key: "category"}, {header: "Status", key: "status"}, {header: "Review", key: "review_reason"}}}),
		p.command(endpoint{use: "get <template-id>", aliases: []string{"show"}, short: "Show one template", method: http.MethodGet,
			path: "/templates/{0}", nargs: 1}),
		p.command(endpoint{use: "create", short: "Create a template (sent for review; usable once approved)", method: http.MethodPost, path: "/templates",
			done:    "Template created and sent for review.",
			example: `  arianet sms templates create --name login --category otp --body "Your Acme login code: {code}"`,
			body: []bodyFlag{
				{"name", "name", "string", "template name, unique in your account", true},
				{"body", "body", "string", "template text; variable parts are written {name} (lower-case letters, digits, _)", true},
				{"category", "category", "string", "otp or transactional (default transactional)", false},
			}}),
		p.command(endpoint{use: "update <template-id>", short: "Replace a template's name and text (a changed text is reviewed again)", method: http.MethodPut, path: "/templates/{0}", nargs: 1,
			done: "Template #{0} updated.", body: []bodyFlag{
				{"name", "name", "string", "template name", true},
				{"body", "body", "string", "template text", true},
				{"category", "category", "string", "otp or transactional", false},
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

	webhook := subgroup("webhook", "Manage the webhook (batch, delivery and inbound events)", nil,
		p.command(endpoint{use: "get", aliases: []string{"show"}, short: "Show the webhook URL and its signing secret", method: http.MethodGet, path: "/webhook"}),
		p.command(endpoint{use: "set", short: "Set the webhook URL (an empty --url turns it off)", method: http.MethodPut, path: "/webhook", done: "Webhook updated.",
			body: []bodyFlag{
				{"url", "url", "string", "HTTPS URL that receives the events", true},
				{"enabled", "enabled", "bool", "turn the webhook on or off", false},
				{"events", "events", "strings", "per-message events: message.delivered,message.failed,inbound.received (batch.completed is always sent)", false},
			}}),
		p.command(endpoint{use: "rotate-secret", short: "Replace the webhook signing secret", method: http.MethodPost, path: "/webhook/secret",
			confirm: "Replace the webhook signing secret? Your receiver must switch to the new one.", done: "Webhook secret replaced."}),
	)

	cmd.AddCommand(
		application, lookup, send, messages, batches, scheduled, lines, templates, blocked, webhook,
		p.command(endpoint{use: "inbound", short: "List messages received on your lines", method: http.MethodGet, path: "/inbound", query: pageQuery}),
		p.command(endpoint{use: "report", short: "Show the sending summary (sent, delivered, failed, spend)", method: http.MethodGet, path: "/reports/summary",
			query:  []queryFlag{{"from", "from", "first day, e.g. 2026-11-01"}, {"to", "to", "last day, e.g. 2026-11-30"}},
			render: renderSMSReport}),
		p.command(endpoint{use: "prices", short: "Show the price per message and its volume tiers", method: http.MethodGet, path: "/prices",
			render: renderSMSPrices}),
		p.apiCmd(),
	)
	return cmd
}

// renderSMSPrices shows GET /prices: the billing unit and base price, then the volume tiers.
func renderSMSPrices(p *printer.Printer, obj map[string]any) {
	printFields(p, obj, []col{{header: "Billed per", key: "billing_unit"}, {header: "Unit price (rial)", key: "unit_rial"}})
	tiers := mapsOf(obj["tiers"])
	if len(tiers) == 0 {
		return
	}
	headers, rows := rowsOf(tiers, []col{{header: "From messages", key: "min"}, {header: "From parts", key: "min_parts"}, {header: "Unit price (rial)", key: "unit_rial"}})
	p.TableAdvanced("Volume tiers", headers, rows)
}

// renderSMSReport shows GET /reports/summary: the period totals, then one row per day.
func renderSMSReport(p *printer.Printer, obj map[string]any) {
	printFields(p, obj, []col{{header: "From", key: "from"}, {header: "To", key: "to"}, {header: "Sent", key: "sent"},
		{header: "Delivered", key: "delivered"}, {header: "Failed", key: "failed"}, {header: "Pending", key: "pending"},
		{header: "Parts", key: "parts"}, {header: "Spend (rial)", key: "spend_rial"}})
	days := mapsOf(obj["daily"])
	if len(days) == 0 {
		return
	}
	headers, rows := rowsOf(days, []col{{header: "Date", key: "date"}, {header: "Sent", key: "sent"}, {header: "Delivered", key: "delivered"},
		{header: "Failed", key: "failed"}, {header: "Spend (rial)", key: "spend_rial"}})
	p.TableAdvanced("By day", headers, rows)
}

func mapsOf(v any) []map[string]any {
	list, _ := v.([]any)
	out := make([]map[string]any, 0, len(list))
	for _, it := range list {
		if m, ok := it.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}
