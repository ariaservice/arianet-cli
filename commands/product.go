package commands

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ariaservice/arianet-cli/internal/api"
	"github.com/ariaservice/arianet-cli/internal/printer"
	"github.com/spf13/cobra"
)

// A product is one of the product APIs the gateway relays like the cloud
// (/hosting/v1, /vps/v1, /baremetal/v1, /sms/v1). Its commands are built
// from endpoint descriptions: each is one request, its flags become query
// parameters or JSON body fields, and its answer ({"data": ...}) is printed
// as a table or, with -o json, as the service returned it.
type product struct {
	name   string // command path for hints, e.g. "arianet hosting"
	prefix string // gateway path, e.g. /hosting/v1
	scopes string // the token scopes, for help texts
	// actions is true when the product answers changes with an action
	// ({"action": {...}}) that GET /actions/{id} follows.
	actions bool
	region  string // --region
}

func (p *product) client() *api.CloudClient {
	client, err := requireAuth()
	if err != nil {
		handleAPIError(err)
		return nil
	}
	return client.Product(p.prefix, p.region)
}

// group is the product's top command with --region on every subcommand.
func (p *product) group(use, short, long string, aliases ...string) *cobra.Command {
	cmd := &cobra.Command{
		Use:     use,
		Aliases: aliases,
		Short:   short,
		Long: long + `

The commands call ` + p.prefix + ` on the API host with your API token.
Token scopes: ` + p.scopes + ` (a token without scopes may do everything).
--region picks a regional install (default: the product's default region).
Add -o json to print the service's answer as is.`,
	}
	cmd.PersistentFlags().StringVar(&p.region, "region", "", "regional install to call, e.g. iran-1 (default: the product's default region)")
	return cmd
}

// bodyFlag is a flag that sets one JSON body field when given.
type bodyFlag struct {
	flag     string
	field    string
	kind     string // string | int | bool | strings | ints | texts (repeatable, never split)
	usage    string
	required bool
}

// queryFlag is a flag that sets one query parameter when given.
type queryFlag struct {
	flag  string
	param string
	usage string
}

// endpoint is one leaf command.
type endpoint struct {
	use     string // e.g. "get <account-id>"
	aliases []string
	short   string
	example string
	method  string
	// path names positional arguments as {0}, {1}...
	path    string
	nargs   int
	query   []queryFlag
	body    []bodyFlag
	rawBody bool   // --data / --file for the whole JSON body
	confirm string // a prompt, with {0}... for the arguments; --yes skips it
	done    string // what a successful change prints, with {0}...
	cols    []col  // table columns; nil shows every scalar field
}

func fill(template string, args []string) string {
	for i, a := range args {
		template = strings.ReplaceAll(template, "{"+strconv.Itoa(i)+"}", a)
	}
	return template
}

func (p *product) command(e endpoint) *cobra.Command {
	var (
		yes      bool
		dataJSON string
		dataFile string
		w        waitFlags
	)
	strs := map[string]*string{}
	ints := map[string]*int64{}
	bools := map[string]*bool{}
	lists := map[string]*[]string{}

	cmd := &cobra.Command{
		Use:     e.use,
		Aliases: e.aliases,
		Short:   e.short,
		Example: e.example,
		Args:    cobra.ExactArgs(e.nargs),
		RunE: func(cmd *cobra.Command, args []string) error {
			escaped := make([]string, len(args))
			for i, a := range args {
				a = strings.TrimSpace(a)
				if a == "" || strings.Contains(a, "/") || a == "." || a == ".." {
					return fmt.Errorf("invalid argument %q", args[i])
				}
				escaped[i] = url.PathEscape(a)
			}
			path := fill(e.path, escaped)

			q := url.Values{}
			for _, f := range e.query {
				if v := *strs["q:"+f.flag]; v != "" {
					q.Set(f.param, v)
				}
			}

			body, err := buildBody(cmd, e, dataJSON, dataFile, strs, ints, bools, lists)
			if err != nil {
				return err
			}

			if e.confirm != "" && !yes && !confirm(fill(e.confirm, args)) {
				printer.Info("Cancelled.")
				return nil
			}

			cc := p.client()
			if cc == nil {
				return nil
			}
			var raw json.RawMessage
			switch e.method {
			case http.MethodGet:
				err = cc.Get(path, q, &raw)
			case http.MethodPost:
				err = cc.Post(withQuery(path, q), bodyOrNil(body), &raw)
			case http.MethodPut:
				err = cc.Put(withQuery(path, q), bodyOrNil(body), &raw)
			case http.MethodPatch:
				err = cc.Patch(withQuery(path, q), bodyOrNil(body), &raw)
			case http.MethodDelete:
				err = cc.Delete(path, q, bodyOrNil(body), &raw)
			}
			if err != nil {
				handleAPIError(err)
				return nil
			}
			if e.method == http.MethodGet {
				printAnswer(raw, e.cols)
				return nil
			}
			p.reportChange(cc, raw, fill(e.done, args), w)
			return nil
		},
	}
	for _, f := range e.query {
		strs["q:"+f.flag] = cmd.Flags().String(f.flag, "", f.usage)
	}
	for _, f := range e.body {
		usage := f.usage
		if f.required {
			usage += " (required)"
		}
		switch f.kind {
		case "int":
			ints[f.flag] = cmd.Flags().Int64(f.flag, 0, usage)
		case "bool":
			bools[f.flag] = cmd.Flags().Bool(f.flag, false, usage)
		case "strings", "ints":
			lists[f.flag] = cmd.Flags().StringArray(f.flag, nil, usage+" (repeatable, or comma separated)")
		case "texts":
			lists[f.flag] = cmd.Flags().StringArray(f.flag, nil, usage+" (repeatable)")
		default:
			strs[f.flag] = cmd.Flags().String(f.flag, "", usage)
		}
	}
	if e.rawBody {
		cmd.Flags().StringVar(&dataJSON, "data", "", "the JSON request body; flags given as well override its fields")
		cmd.Flags().StringVar(&dataFile, "file", "", "read the JSON request body from this file")
	}
	if e.confirm != "" {
		cmd.Flags().BoolVarP(&yes, "yes", "y", false, "skip the confirmation prompt")
	}
	if p.actions && e.method != http.MethodGet {
		w.add(cmd)
	}
	return cmd
}

func withQuery(path string, q url.Values) string {
	if len(q) == 0 {
		return path
	}
	return path + "?" + q.Encode()
}

func bodyOrNil(body map[string]any) any {
	if body == nil {
		return nil
	}
	return body
}

// buildBody starts from --data/--file and sets the fields of every flag given.
func buildBody(cmd *cobra.Command, e endpoint, dataJSON, dataFile string,
	strs map[string]*string, ints map[string]*int64, bools map[string]*bool, lists map[string]*[]string) (map[string]any, error) {
	var body map[string]any
	switch {
	case dataJSON != "" && dataFile != "":
		return nil, fmt.Errorf("--data and --file cannot be combined")
	case dataJSON != "":
		if err := json.Unmarshal([]byte(dataJSON), &body); err != nil {
			return nil, fmt.Errorf("--data is not a JSON object: %w", err)
		}
	case dataFile != "":
		if err := readJSONFile(dataFile, &body); err != nil {
			return nil, err
		}
	}
	for _, f := range e.body {
		if !cmd.Flags().Changed(f.flag) {
			if f.required && body[f.field] == nil {
				return nil, fmt.Errorf("--%s is required", f.flag)
			}
			continue
		}
		if body == nil {
			body = map[string]any{}
		}
		switch f.kind {
		case "int":
			body[f.field] = *ints[f.flag]
		case "bool":
			body[f.field] = *bools[f.flag]
		case "strings":
			body[f.field] = splitList(*lists[f.flag])
		case "texts":
			body[f.field] = *lists[f.flag]
		case "ints":
			var out []int64
			for _, v := range splitList(*lists[f.flag]) {
				n, err := strconv.ParseInt(v, 10, 64)
				if err != nil {
					return nil, fmt.Errorf("--%s: %q is not a number", f.flag, v)
				}
				out = append(out, n)
			}
			body[f.field] = out
		default:
			body[f.field] = *strs[f.flag]
		}
	}
	return body, nil
}

func splitList(values []string) []string {
	out := []string{}
	for _, v := range values {
		for _, part := range strings.Split(v, ",") {
			if part = strings.TrimSpace(part); part != "" {
				out = append(out, part)
			}
		}
	}
	return out
}

// printAnswer prints {"data": [...]} as a table, {"data": {...}} as fields,
// and anything else (or -o json) as JSON.
func printAnswer(raw []byte, cols []col) {
	if jsonMode() {
		printRawJSON(raw)
		return
	}
	resp, err := decodeObject(raw)
	if err != nil {
		printRawJSON(raw)
		return
	}
	p := printer.New(cfg.Output)
	switch data := resp["data"].(type) {
	case []any:
		items := make([]map[string]any, 0, len(data))
		for _, it := range data {
			if m, ok := it.(map[string]any); ok {
				items = append(items, m)
			}
		}
		if len(items) == 0 {
			printer.Info("Nothing found.")
			return
		}
		if cols == nil {
			genericTable(p, items, []string{"id", "name"})
		} else {
			headers, rows := rowsOf(items, cols)
			p.Table(headers, rows)
		}
		if meta, ok := resp["meta"].(map[string]any); ok {
			printPageFooter(meta)
		}
	case map[string]any:
		printFields(p, data, cols)
	default:
		printRawJSON(raw)
	}
}

// printPageFooter prints Laravel-style or cloud-style pagination, if any.
func printPageFooter(meta map[string]any) {
	pg, ok := meta["pagination"].(map[string]any)
	if !ok {
		pg = meta
	}
	page, _ := strconv.Atoi(cell(firstOf(pg, "page", "current_page")))
	last, _ := strconv.Atoi(cell(pg["last_page"]))
	total, _ := strconv.Atoi(cell(firstOf(pg, "total_entries", "total")))
	if page > 0 && last > 1 {
		printer.PaginationFooter(page, last, total, "items")
	}
}

func firstOf(m map[string]any, keys ...string) any {
	for _, k := range keys {
		if v, ok := m[k]; ok {
			return v
		}
	}
	return nil
}

// printFields prints one object as Field/Value rows: cols when given, else
// every field in name order (nested lists as a count).
func printFields(p *printer.Printer, obj map[string]any, cols []col) {
	if cols == nil {
		keys := make([]string, 0, len(obj))
		for k := range obj {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			cols = append(cols, col{header: strings.ReplaceAll(k, "_", " "), key: k})
		}
	}
	rows := make([][]string, 0, len(cols))
	for _, c := range cols {
		rows = append(rows, []string{c.header, cell(lookup(obj, c.key))})
	}
	p.Table([]string{"Field", "Value"}, rows)
}

// oneTimeFields are answer fields shown once by the service: printed in
// full with a warning.
var oneTimeFields = []string{"password", "root_password"}

// reportChange prints what a change returned and, with --wait, follows its
// action until it ends.
func (p *product) reportChange(cc *api.CloudClient, raw []byte, done string, w waitFlags) {
	resp := map[string]any{}
	if len(bytes.TrimSpace(raw)) > 0 {
		var err error
		if resp, err = decodeObject(raw); err != nil {
			handleAPIError(err)
			return
		}
	}
	ids := []int64{}
	if p.actions {
		ids = actionIDs(resp)
	}
	outcome := waitDone
	if w.wait && len(ids) > 0 {
		var finals []map[string]any
		finals, outcome = p.waitActions(cc, ids, w.timeout)
		if jsonMode() {
			resp["actions"] = finals
		}
	}
	if jsonMode() {
		printer.New(cfg.Output).PrintJSON(resp)
		exitForOutcome(outcome)
		return
	}
	printer.Success(done)
	if data, ok := resp["data"].(map[string]any); ok && len(data) > 0 {
		printFields(printer.New(cfg.Output), data, nil)
	}
	for _, k := range oneTimeFields {
		if pw, ok := resp[k].(string); ok && pw != "" {
			fmt.Printf("  %s: %s\n", capitalize(strings.ReplaceAll(k, "_", " ")), printer.CleanLine(pw))
			fmt.Println("  It is shown only this once - save it now.")
		}
	}
	if !w.wait {
		for _, id := range ids {
			printer.Info(fmt.Sprintf("Action #%d is running. Follow it with: %s actions wait %d", id, p.name, id))
		}
	}
	exitForOutcome(outcome)
}

// actionState is an action's state ("state" at the product services,
// "status" at the cloud).
func actionState(a map[string]any) string {
	if s, ok := a["state"].(string); ok && s != "" {
		return s
	}
	s, _ := a["status"].(string)
	return s
}

// waitActions polls GET /actions/{id} until every action left running.
func (p *product) waitActions(cc *api.CloudClient, ids []int64, timeout time.Duration) ([]map[string]any, waitOutcome) {
	if timeout <= 0 {
		timeout = defaultWaitTimeout
	}
	start := time.Now()
	deadline := start.Add(timeout)
	finals := make([]map[string]any, len(ids))
	errCount := 0
	fmt.Fprintf(os.Stderr, "\n  Waiting for %s (Ctrl+C stops waiting; the operation continues)\n", actionsLabel(ids))
	for {
		pending, failed := 0, false
		for i, id := range ids {
			if finals[i] != nil && actionState(finals[i]) != "running" && actionState(finals[i]) != "pending" {
				failed = failed || actionState(finals[i]) == "error"
				continue
			}
			var raw json.RawMessage
			err := cc.Get("/actions/"+strconv.FormatInt(id, 10), nil, &raw)
			if err == nil {
				var resp map[string]any
				if resp, err = decodeObject(raw); err == nil {
					finals[i], _ = resp["data"].(map[string]any)
				}
			}
			switch {
			case err != nil && !pollErrorIsTransient(err):
				fmt.Fprintf(os.Stderr, "  Stopped waiting: %s\n", printer.CleanLine(err.Error()))
				return finals, waitAborted
			case err != nil:
				if errCount++; errCount >= maxPollErrors {
					fmt.Fprintf(os.Stderr, "  Stopped waiting: %s\n", printer.CleanLine(err.Error()))
					return finals, waitAborted
				}
				pending++
				continue
			}
			errCount = 0
			state := actionState(finals[i])
			fmt.Fprintf(os.Stderr, "  [%s] action #%d: %s\n", fmtElapsed(time.Since(start)), id, printer.CleanLine(state))
			switch state {
			case "error":
				failed = true
			case "success":
			default:
				pending++
			}
		}
		if pending == 0 {
			if failed {
				fmt.Fprintf(os.Stderr, "\n  %s failed. Details: %s actions get <id>\n\n", capitalize(actionsLabel(ids)), p.name)
				return finals, waitFailed
			}
			fmt.Fprintf(os.Stderr, "\n  %s finished.\n\n", capitalize(actionsLabel(ids)))
			return finals, waitDone
		}
		if !time.Now().Add(pollInterval).Before(deadline) {
			fmt.Fprintf(os.Stderr, "\n  Still running after %s. Check: %s actions get %d\n\n", timeout, p.name, ids[0])
			return finals, waitTimedOut
		}
		time.Sleep(pollInterval)
	}
}

// actionsCmd is list / get / wait for products that have actions.
func (p *product) actionsCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "actions", Aliases: []string{"action"}, Short: "Follow asynchronous operations (actions)"}
	cmd.AddCommand(
		p.command(endpoint{use: "list", aliases: []string{"ls"}, short: "List your latest operations", method: http.MethodGet,
			path: "/actions", query: []queryFlag{{"limit", "limit", "at most this many"}}}),
		p.command(endpoint{use: "get <action-id>", short: "Show one operation", method: http.MethodGet, path: "/actions/{0}", nargs: 1}),
	)
	var timeout time.Duration
	wait := &cobra.Command{
		Use:   "wait <action-id> [<action-id>...]",
		Short: "Wait until operations finish; exits non-zero if one fails or the timeout passes",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ids, err := parseCloudIDs(args, "action")
			if err != nil {
				return err
			}
			cc := p.client()
			if cc == nil {
				return nil
			}
			finals, outcome := p.waitActions(cc, ids, timeout)
			if jsonMode() {
				printer.New(cfg.Output).PrintJSON(finals)
			}
			exitForOutcome(outcome)
			return nil
		},
	}
	wait.Flags().DurationVar(&timeout, "timeout", defaultWaitTimeout, "how long to wait")
	cmd.AddCommand(wait)
	return cmd
}

// apiCmd sends any request to the product API, for what no command covers.
func (p *product) apiCmd() *cobra.Command {
	var dataJSON, dataFile string
	cmd := &cobra.Command{
		Use:   "api <method> <path>",
		Short: "Send any request to the " + p.prefix + " API and print the answer as JSON",
		Example: fmt.Sprintf(`  %[1]s api GET /openapi.json
  %[1]s api POST /some/path --data '{"key":"value"}'`, p.name),
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			method := strings.ToUpper(args[0])
			path := args[1]
			if !strings.HasPrefix(path, "/") || strings.Contains(path, "..") {
				return fmt.Errorf("the path must start with / and stay inside %s", p.prefix)
			}
			var body any
			if dataJSON != "" || dataFile != "" {
				m, err := buildBody(cmd, endpoint{}, dataJSON, dataFile, nil, nil, nil, nil)
				if err != nil {
					return err
				}
				body = m
			}
			cc := p.client()
			if cc == nil {
				return nil
			}
			pathOnly, rawQuery, _ := strings.Cut(path, "?")
			q, err := url.ParseQuery(rawQuery)
			if err != nil {
				return fmt.Errorf("invalid query: %w", err)
			}
			var raw json.RawMessage
			switch method {
			case http.MethodGet:
				err = cc.Get(pathOnly, q, &raw)
			case http.MethodPost:
				err = cc.Post(path, body, &raw)
			case http.MethodPut:
				err = cc.Put(path, body, &raw)
			case http.MethodPatch:
				err = cc.Patch(path, body, &raw)
			case http.MethodDelete:
				err = cc.Delete(pathOnly, q, body, &raw)
			default:
				return fmt.Errorf("unsupported method %s", method)
			}
			if err != nil {
				handleAPIError(err)
				return nil
			}
			if len(bytes.TrimSpace(raw)) == 0 {
				printer.Success("Done.")
				return nil
			}
			v, err := decodeAny(raw)
			if err != nil {
				handleAPIError(err)
				return nil
			}
			printer.New("json").PrintJSON(v)
			return nil
		},
	}
	cmd.Flags().StringVar(&dataJSON, "data", "", "the JSON request body")
	cmd.Flags().StringVar(&dataFile, "file", "", "read the JSON request body from this file")
	return cmd
}

// subgroup is a command holding other commands.
func subgroup(use, short string, aliases []string, children ...*cobra.Command) *cobra.Command {
	cmd := &cobra.Command{Use: use, Short: short, Aliases: aliases}
	cmd.AddCommand(children...)
	return cmd
}
