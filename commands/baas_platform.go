package commands

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/ariaservice/arianet-cli/internal/printer"
	"github.com/spf13/cobra"
)

// advisors, custom domains, transfers and API settings (arianet-infra-tasks
// #1149).

var baasAdvisorKinds = []string{"security", "performance"}

// lintObject is a lint's object as "table public.todos".
func lintObject(l map[string]any) string {
	o, _ := l["object"].(map[string]any)
	if o == nil {
		return "-"
	}
	name := str(o["name"])
	if s := str(o["schema"]); s != "" && name != "" {
		name = s + "." + name
	}
	return strings.TrimSpace(str(o["type"]) + " " + name)
}

func renderAdvisors(data map[string]any, details bool) {
	p := printer.New(cfg.Output)
	if run, ok := data["run"].(map[string]any); ok {
		line := fmt.Sprintf("Last run: %s (%s, %s)", cell(run["finished_at"]), cell(run["trigger"]), cell(run["status"]))
		if code := str(run["error_code"]); code != "" {
			line += ", error " + code
		}
		printer.Info(line)
	} else {
		printer.Info("No run yet: arianet baas advisors --run")
	}
	if counts, ok := data["counts"].(map[string]any); ok {
		rows := [][]string{}
		for _, kind := range baasAdvisorKinds {
			c, ok := counts[kind].(map[string]any)
			if !ok {
				continue
			}
			rows = append(rows, []string{kind, cell(c["ERROR"]), cell(c["WARN"]), cell(c["INFO"])})
		}
		if len(rows) > 0 {
			p.Table([]string{"Category", "Errors", "Warnings", "Info"}, rows)
		}
	}
	lints, _ := data["lints"].([]any)
	if len(lints) == 0 {
		printer.Success("No findings.")
		return
	}
	rows := make([][]string, 0, len(lints))
	for _, it := range lints {
		l, _ := it.(map[string]any)
		rows = append(rows, []string{cell(l["level"]), cell(l["category"]), cell(l["name"]), lintObject(l), cell(l["detail"])})
	}
	p.Table([]string{"Level", "Category", "Lint", "Object", "Detail"}, rows)
	if !details {
		printer.Info("Add --details for how to fix each finding.")
		return
	}
	for _, it := range lints {
		l, _ := it.(map[string]any)
		fmt.Printf("\n%s %s: %s\n", printer.CleanLine(str(l["level"])), printer.CleanLine(str(l["name"])), printer.CleanLine(lintObject(l)))
		if t := str(l["title"]); t != "" {
			fmt.Printf("  %s\n", printer.CleanLine(t))
		}
		if r := str(l["remediation"]); r != "" {
			fmt.Printf("  Fix: %s\n", printer.CleanLine(r))
		}
		if s := str(l["sql"]); s != "" {
			fmt.Printf("  SQL: %s\n", printer.CleanLine(s))
		}
	}
}

func (p *product) baasAdvisorsCmd() *cobra.Command {
	var kind string
	var run, details bool
	cmd := &cobra.Command{
		Use:     "advisors [project-id]",
		Aliases: []string{"advisor", "lint"},
		Short:   "Show the security and performance findings of the database (--run checks now)",
		Long: `Show what the security and performance advisors found in the project's database: tables
without row level security, policies that allow everything, exposed security definer
functions, unindexed foreign keys, duplicate indexes and more, each with its level (ERROR,
WARN, INFO) and how to fix it. The checks run daily; --run runs them now.`,
		Example: `  arianet baas advisors
  arianet baas advisors 12 --kind security --run --details`,
		Args: projectArgsRange(0, 0),
		RunE: func(cmd *cobra.Command, args []string) error {
			args, err := withProject(args, 0, 0)
			if err != nil {
				return err
			}
			id := args[0]
			if kind != "" && kind != "security" && kind != "performance" {
				return fmt.Errorf("--kind must be security or performance")
			}
			cc := p.client()
			if cc == nil {
				return nil
			}
			q := url.Values{}
			if kind != "" {
				q.Set("kind", kind)
			}
			var raw json.RawMessage
			if run {
				err = cc.Post(withQuery("/projects/"+id+"/advisors/run", q), nil, &raw)
			} else {
				err = cc.Get("/projects/"+id+"/advisors", q, &raw)
			}
			if err != nil {
				handleAPIError(err)
				return nil
			}
			obj, derr := decodeObject(raw)
			data, _ := obj["data"].(map[string]any)
			if jsonMode() || derr != nil || data == nil {
				printRawJSON(raw)
				return nil
			}
			renderAdvisors(data, details)
			return nil
		},
	}
	cmd.Flags().StringVar(&kind, "kind", "", "only security or performance findings")
	cmd.Flags().BoolVar(&run, "run", false, "run the checks now (rate limited) instead of showing the last run")
	cmd.Flags().BoolVar(&details, "details", false, "print each finding's fix and SQL")
	return cmd
}

// ─── custom domains ──────────────────────────────────────────────────────────

var baasDomainCols = []col{{header: "ID", key: "id"}, {header: "Hostname", key: "hostname"}, {header: "State", key: "state"},
	{header: "Checked", key: "last_checked_at"}, {header: "Active since", key: "active_at"}, {header: "Error", key: "error"}}

// printDomainRecords prints the DNS records a domain needs.
func printDomainRecords(d map[string]any) {
	records, _ := d["records"].([]any)
	if len(records) == 0 || str(d["state"]) == "active" {
		return
	}
	printer.Info(fmt.Sprintf("DNS records for %s:", str(d["hostname"])))
	for _, it := range records {
		r, _ := it.(map[string]any)
		state := "missing"
		if r["ok"] == true {
			state = "ok"
		}
		printer.Info(fmt.Sprintf("  %-5s %s  ->  %s  (%s)", str(r["type"]), str(r["name"]), str(r["value"]), state))
	}
}

func renderDomains(p *printer.Printer, obj map[string]any) {
	data, _ := obj["data"].(map[string]any)
	if data == nil {
		data = obj
	}
	printFields(p, data, []col{{header: "Target", key: "target"}, {header: "Domains allowed", key: "limit"}, {header: "Plan allows", key: "plan_allows"}})
	list, _ := data["domains"].([]any)
	items := make([]map[string]any, 0, len(list))
	for _, it := range list {
		if m, ok := it.(map[string]any); ok {
			items = append(items, m)
		}
	}
	if len(items) == 0 {
		printer.Info("No custom domain yet: arianet baas domains add <hostname>")
		return
	}
	headers, rows := rowsOf(items, baasDomainCols)
	p.Table(headers, rows)
	for _, d := range items {
		printDomainRecords(d)
	}
}

// domainChange is add and check: one request answering {"data": Domain}.
func (p *product) domainChange(use, short, example, pathSuffix, done, bodyField string) *cobra.Command {
	return &cobra.Command{
		Use:     use,
		Short:   short,
		Example: example,
		Args:    projectArgsRange(1, 1),
		RunE: func(cmd *cobra.Command, args []string) error {
			args, err := withProject(args, 1, 1)
			if err != nil {
				return err
			}
			id, arg := args[0], strings.TrimSpace(args[1])
			var body any
			path := "/projects/" + id + "/domains"
			if bodyField != "" {
				body = map[string]string{bodyField: arg}
			} else {
				if !digitsOnly.MatchString(arg) {
					return fmt.Errorf("invalid domain ID %q: expected a number (arianet baas domains list)", arg)
				}
				path += "/" + arg
			}
			cc := p.client()
			if cc == nil {
				return nil
			}
			var raw json.RawMessage
			if err := cc.Post(path+pathSuffix, body, &raw); err != nil {
				handleAPIError(err)
				return nil
			}
			obj, derr := decodeObject(raw)
			d, _ := obj["data"].(map[string]any)
			if jsonMode() || derr != nil || d == nil {
				printRawJSON(raw)
				return nil
			}
			printer.Success(fmt.Sprintf(done, str(d["hostname"]), str(d["state"])))
			printFields(printer.New(cfg.Output), d, baasDomainCols)
			printDomainRecords(d)
			return nil
		},
	}
}

func (p *product) baasDomainsCmd() *cobra.Command {
	return subgroup("domains", "Serve the project at your own hostname (paid plans)", []string{"domain"},
		p.command(endpoint{use: "list <project-id>", aliases: []string{"ls"}, short: "List the custom domains and the DNS records each needs",
			method: http.MethodGet, path: "/projects/{0}/domains", nargs: 1, render: renderDomains}),
		p.domainChange("add [project-id] <hostname>", "Add a custom domain; prints the CNAME and TXT records to create",
			"  arianet baas domains add api.example.com", "", "Added %s (%s). Create these records, then run arianet baas domains check.", "hostname"),
		p.domainChange("check [project-id] <domain-id>", "Check the DNS records now; once both are right the certificate is issued",
			"  arianet baas domains check 7", "/check", "%s is %s.", ""),
		p.command(endpoint{use: "remove <project-id> <domain-id>", aliases: []string{"rm", "delete"}, short: "Remove a custom domain",
			method: http.MethodDelete, path: "/projects/{0}/domains/{1}", nargs: 2,
			confirm: "Remove custom domain #{1} of project #{0}? The project stops answering at it.", done: "Domain #{1} removed."}),
	)
}

// ─── transfer ────────────────────────────────────────────────────────────────

var baasTransferCols = []col{{header: "ID", key: "id"}, {header: "Project", key: "project_id"}, {header: "Ref", key: "project_ref"},
	{header: "Name", key: "project_name"}, {header: "Plan", key: "plan"}, {header: "From", key: "from_account_id"},
	{header: "To", key: "to_account_id"}, {header: "State", key: "state"}, {header: "Expires", key: "expires_at"}}

func (p *product) baasTransferCmd() *cobra.Command {
	cmd := p.command(endpoint{use: "transfer <project-id>", short: "Move a project to another Arianet account (it accepts within 7 days)",
		method: http.MethodPost, path: "/projects/{0}/transfer", nargs: 1, done: "Transfer of project #{0} requested; the other account accepts it with arianet baas transfer accept.",
		example: "  arianet baas transfer 12 --to 123 --confirm abcdefghij0123456789\n  arianet baas transfer list --direction incoming",
		body: []bodyFlag{
			{"to", "target_account_id", "int", "the account ID to move the project to", true},
			{"confirm", "confirm", "string", "the project's ref, typed to confirm", true},
		}})
	cmd.AddCommand(
		p.command(endpoint{use: "status <project-id>", short: "Show the project's pending transfer", method: http.MethodGet,
			path: "/projects/{0}/transfer", nargs: 1, cols: baasTransferCols}),
		p.command(endpoint{use: "cancel <project-id>", short: "Cancel the project's pending transfer", method: http.MethodDelete,
			path: "/projects/{0}/transfer", nargs: 1, confirm: "Cancel the pending transfer of project #{0}?", done: "Transfer of project #{0} cancelled."}),
		p.command(endpoint{use: "list", aliases: []string{"ls"}, short: "List pending transfers to or from your account", method: http.MethodGet,
			path: "/transfers", cols: baasTransferCols, query: []queryFlag{{"direction", "direction", "incoming or outgoing"}}}),
		p.command(endpoint{use: "accept <transfer-id>", short: "Accept a transfer to your account (billed as a new project of its plan)",
			method: http.MethodPost, path: "/transfers/{0}/accept", nargs: 1, done: "Transfer #{0} accepted; the project is now yours."}),
		p.command(endpoint{use: "decline <transfer-id>", short: "Decline a transfer to your account", method: http.MethodPost,
			path: "/transfers/{0}/decline", nargs: 1, done: "Transfer #{0} declined."}),
	)
	return cmd
}

// ─── API settings ────────────────────────────────────────────────────────────

func (p *product) baasAPISettingsCmd() *cobra.Command {
	cols := []col{{header: "Max rows", key: "max_rows"}, {header: "JWT expiry (s)", key: "jwt_expiry_seconds"},
		{header: "Max rows limit", key: "max_rows_limit"}, {header: "Default max rows", key: "defaults.max_rows"},
		{header: "Default JWT expiry (s)", key: "defaults.jwt_expiry_seconds"}}
	return subgroup("api-settings", "Show or change the REST row limit and the JWT expiry", []string{"api-setting"},
		p.command(endpoint{use: "get <project-id>", aliases: []string{"show"}, short: "Show the API settings", method: http.MethodGet,
			path: "/projects/{0}/settings/api", nargs: 1, cols: cols}),
		p.command(endpoint{use: "set <project-id>", short: "Change the API settings", method: http.MethodPut,
			path: "/projects/{0}/settings/api", nargs: 1, done: "API settings of project #{0} updated.",
			example: "  arianet baas api-settings set --max-rows 500 --jwt-expiry 7200",
			body: []bodyFlag{
				{"max-rows", "max_rows", "int", "most rows one REST request returns (1-100000)", false},
				{"jwt-expiry", "jwt_expiry_seconds", "int", "lifetime of issued access tokens in seconds (300-604800)", false},
			}}),
	)
}
