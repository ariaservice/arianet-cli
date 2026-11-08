package commands

import (
	"bytes"
	"encoding/json"
	"fmt"
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

// The cloud command group talks to the cloud API at <api_url>/cloud/v1 with
// the same token as every other command. Reads need the token scope
// cloud:servers:read, changes cloud:servers:write.

const cloudScopesHint = "The cloud commands need a token with the scopes cloud:servers:read (reads) and cloud:servers:write (changes)."

func newCloudCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cloud",
		Short: "Manage Arianet Cloud resources (servers, volumes, networks, firewalls...)",
		Long: `Manage Arianet Cloud: servers, volumes, private networks, firewalls,
floating IPs, load balancers, images and actions.

The commands call the cloud API at <api-url>/cloud/v1 with your API token.
` + cloudScopesHint + `

Lists accept --label-selector (Kubernetes style, e.g. "env=prod,tier!=db")
and --page/--per-page; without a page every item is fetched.
Commands that start an operation print its action; --wait follows it to the end.`,
	}
	cmd.AddCommand(
		newCloudServersCmd(),
		newCloudVolumesCmd(),
		newCloudNetworksCmd(),
		newCloudFirewallsCmd(),
		newCloudFloatingIPsCmd(),
		newCloudLoadBalancersCmd(),
		newCloudSSHKeysCmd(),
		newCloudImagesCmd(),
		newCloudActionsCmd(),
		newCloudPricingCmd(),
		newCloudPlansCmd(),
		newCloudDatacentersCmd(),
		newCloudLimitsCmd(),
	)
	return cmd
}

// requireCloud returns the cloud API client, or reports why there is none.
func requireCloud() *api.CloudClient {
	client, err := requireAuth()
	if err != nil {
		handleAPIError(err)
		return nil
	}
	return client.Cloud()
}

// ─── Resource descriptions ────────────────────────────────────────────────────

// col is one table column: a header and a dotted path into the JSON object.
type col struct {
	header string
	key    string
}

// cloudResource is one kind of cloud resource and how it is shown.
type cloudResource struct {
	plural   string // command name: "servers"
	singular string // "server"
	path     string // API path: "/servers"
	cols     []col  // list columns
	detail   []col  // get fields
	// subtables prints nested lists (rules, subnets...) under the details.
	subtables func(p *printer.Printer, obj map[string]any)
}

func (r cloudResource) itemPath(id int64) string {
	return r.path + "/" + strconv.FormatInt(id, 10)
}

func (r cloudResource) title() string {
	return capitalize(r.singular)
}

func capitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// ─── JSON helpers ─────────────────────────────────────────────────────────────

// decodeObject turns raw JSON into a generic object, keeping numbers exact.
func decodeObject(raw []byte) (map[string]any, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var out map[string]any
	if err := dec.Decode(&out); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return out, nil
}

func decodeAny(raw []byte) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var out any
	if err := dec.Decode(&out); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return out, nil
}

// lookup follows a dotted path ("resource.type") into obj.
func lookup(obj map[string]any, path string) any {
	var cur any = obj
	for _, part := range strings.Split(path, ".") {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil
		}
		cur = m[part]
	}
	return cur
}

// cell renders a JSON value for one table cell.
func cell(v any) string {
	switch t := v.(type) {
	case nil:
		return "-"
	case string:
		if t == "" {
			return "-"
		}
		return t
	case bool:
		if t {
			return "yes"
		}
		return "no"
	case json.Number:
		return t.String()
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	case map[string]any:
		if len(t) == 0 {
			return "-"
		}
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		parts := make([]string, 0, len(keys))
		for _, k := range keys {
			if s, ok := t[k].(string); ok {
				parts = append(parts, k+"="+s)
			} else {
				parts = append(parts, k+"="+cell(t[k]))
			}
		}
		return strings.Join(parts, ",")
	case []any:
		if len(t) == 0 {
			return "-"
		}
		parts := make([]string, 0, len(t))
		for _, item := range t {
			if _, isObj := item.(map[string]any); isObj {
				return fmt.Sprintf("%d items", len(t))
			}
			parts = append(parts, cell(item))
		}
		return strings.Join(parts, ",")
	}
	return fmt.Sprintf("%v", v)
}

func rowsOf(items []map[string]any, cols []col) ([]string, [][]string) {
	headers := make([]string, len(cols))
	for i, c := range cols {
		headers[i] = c.header
	}
	rows := make([][]string, len(items))
	for i, it := range items {
		row := make([]string, len(cols))
		for j, c := range cols {
			row[j] = cell(lookup(it, c.key))
		}
		rows[i] = row
	}
	return headers, rows
}

// genericTable prints objects with a column for every scalar key they have,
// for responses whose shape this CLI does not pin down (pricing, plans).
func genericTable(p *printer.Printer, items []map[string]any, preferred []string) {
	seen := map[string]bool{}
	var keys []string
	for _, k := range preferred {
		for _, it := range items {
			if _, ok := it[k]; ok && !seen[k] {
				seen[k] = true
				keys = append(keys, k)
			}
		}
	}
	var rest []string
	for _, it := range items {
		for k, v := range it {
			if seen[k] {
				continue
			}
			switch v.(type) {
			case map[string]any, []any:
				continue
			}
			seen[k] = true
			rest = append(rest, k)
		}
	}
	sort.Strings(rest)
	keys = append(keys, rest...)
	cols := make([]col, len(keys))
	for i, k := range keys {
		cols[i] = col{header: strings.ReplaceAll(k, "_", " "), key: k}
	}
	headers, rows := rowsOf(items, cols)
	p.Table(headers, rows)
}

func objects(raw []json.RawMessage) ([]map[string]any, error) {
	out := make([]map[string]any, 0, len(raw))
	for _, r := range raw {
		obj, err := decodeObject(r)
		if err != nil {
			return nil, err
		}
		out = append(out, obj)
	}
	return out, nil
}

// printRawJSON writes a response as indented JSON.
func printRawJSON(raw []byte) {
	v, err := decodeAny(raw)
	if err != nil {
		handleAPIError(err)
		return
	}
	printer.New(cfg.Output).PrintJSON(v)
}

// ─── Flags ────────────────────────────────────────────────────────────────────

// parseLabels turns repeated key=value flags into a map.
func parseLabels(values []string) (map[string]string, error) {
	out := map[string]string{}
	for _, v := range values {
		for _, part := range strings.Split(v, ",") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			k, val, ok := strings.Cut(part, "=")
			k = strings.TrimSpace(k)
			if !ok || k == "" {
				return nil, fmt.Errorf("invalid label %q: use key=value", part)
			}
			out[k] = strings.TrimSpace(val)
		}
	}
	return out, nil
}

func parseCloudID(arg, what string) (int64, error) {
	id, err := strconv.ParseInt(strings.TrimSpace(arg), 10, 64)
	if err != nil || id <= 0 {
		return 0, fmt.Errorf("invalid %s ID: %s", what, arg)
	}
	return id, nil
}

func parseCloudIDs(values []string, what string) ([]int64, error) {
	var ids []int64
	for _, v := range values {
		for _, part := range strings.Split(v, ",") {
			if part = strings.TrimSpace(part); part == "" {
				continue
			}
			id, err := parseCloudID(part, what)
			if err != nil {
				return nil, err
			}
			ids = append(ids, id)
		}
	}
	return ids, nil
}

// labelFlags adds --labels and, for updates, --clear-labels.
type labelFlags struct {
	values []string
	clear  bool
}

func (l *labelFlags) add(cmd *cobra.Command, withClear bool) {
	cmd.Flags().StringArrayVar(&l.values, "labels", nil, "label key=value (repeatable, or comma separated)")
	if withClear {
		cmd.Flags().BoolVar(&l.clear, "clear-labels", false, "remove every label")
	}
}

// set puts labels into body when the user gave any. On an update the given
// labels replace the existing set.
func (l *labelFlags) set(body map[string]any) error {
	if l.clear {
		if len(l.values) > 0 {
			return fmt.Errorf("--labels and --clear-labels cannot be combined")
		}
		body["labels"] = map[string]string{}
		return nil
	}
	if len(l.values) == 0 {
		return nil
	}
	labels, err := parseLabels(l.values)
	if err != nil {
		return err
	}
	body["labels"] = labels
	return nil
}

// projectFlag sets project_id when --project-id was given.
func projectFlag(cmd *cobra.Command, body map[string]any, projectID int64) {
	if cmd.Flags().Changed("project-id") {
		body["project_id"] = projectID
	}
}

func readJSONFile(path string, into any) error {
	f, err := os.Open(expandHome(path))
	if err != nil {
		return err
	}
	defer f.Close()
	dec := json.NewDecoder(f)
	if err := dec.Decode(into); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	return nil
}

// ─── Generic commands ─────────────────────────────────────────────────────────

type listFilters struct {
	page, perPage int
	selector      string
	projectID     int64
}

func (f *listFilters) add(cmd *cobra.Command, withProject bool) {
	cmd.Flags().IntVar(&f.page, "page", 0, "page number (default: fetch every page)")
	cmd.Flags().IntVar(&f.perPage, "per-page", 0, "items per page, at most 100")
	cmd.Flags().StringVar(&f.selector, "label-selector", "", `filter by labels, e.g. "env=prod,tier!=db"`)
	if withProject {
		cmd.Flags().Int64Var(&f.projectID, "project-id", 0, "only resources in this project")
	}
}

func (f *listFilters) options(extra url.Values) (api.ListOptions, error) {
	if f.perPage > 100 || f.perPage < 0 || f.page < 0 {
		return api.ListOptions{}, fmt.Errorf("--page must be positive and --per-page at most 100")
	}
	if extra == nil {
		extra = url.Values{}
	}
	if f.projectID > 0 {
		extra.Set("project_id", strconv.FormatInt(f.projectID, 10))
	}
	return api.ListOptions{Page: f.page, PerPage: f.perPage, LabelSelector: f.selector, Extra: extra}, nil
}

// runList fetches a list and prints it as a table or a JSON array.
func runList(path string, opt api.ListOptions, noun string, show func(p *printer.Printer, items []map[string]any)) {
	cc := requireCloud()
	raw, page, err := cc.List(path, opt)
	if err != nil {
		handleAPIError(err)
		return
	}
	p := printer.New(cfg.Output)
	if jsonMode() {
		items := make([]any, 0, len(raw))
		for _, r := range raw {
			v, err := decodeAny(r)
			if err != nil {
				handleAPIError(err)
				return
			}
			items = append(items, v)
		}
		p.PrintJSON(items)
		return
	}
	items, err := objects(raw)
	if err != nil {
		handleAPIError(err)
		return
	}
	if len(items) == 0 {
		printer.Info("No " + noun + " found.")
		return
	}
	show(p, items)
	if page != nil && opt.Page > 0 {
		printer.PaginationFooter(page.Page, page.LastPage, page.TotalEntries, noun)
	}
}

func (r cloudResource) listCmd(example string, extraFlags func(cmd *cobra.Command), extra func() url.Values) *cobra.Command {
	var f listFilters
	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List " + r.plural,
		Example: example,
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			var q url.Values
			if extra != nil {
				q = extra()
			}
			opt, err := f.options(q)
			if err != nil {
				return err
			}
			runList(r.path, opt, strings.ReplaceAll(r.plural, "-", " "), func(p *printer.Printer, items []map[string]any) {
				headers, rows := rowsOf(items, r.cols)
				p.Table(headers, rows)
			})
			return nil
		},
	}
	f.add(cmd, true)
	if extraFlags != nil {
		extraFlags(cmd)
	}
	return cmd
}

func (r cloudResource) getCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "get <id>",
		Aliases: []string{"describe", "show"},
		Short:   "Show one " + r.singular,
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := parseCloudID(args[0], r.singular)
			if err != nil {
				return err
			}
			cc := requireCloud()
			var raw json.RawMessage
			if err := cc.Get(r.itemPath(id), nil, &raw); err != nil {
				handleAPIError(err)
				return nil
			}
			r.printItem(raw)
			return nil
		},
	}
}

// printItem prints {"data": {...}}: the object as JSON, or its details.
func (r cloudResource) printItem(raw []byte) {
	resp, err := decodeObject(raw)
	if err != nil {
		handleAPIError(err)
		return
	}
	obj, _ := resp["data"].(map[string]any)
	p := printer.New(cfg.Output)
	if jsonMode() {
		if obj == nil {
			p.PrintJSON(resp)
			return
		}
		p.PrintJSON(obj)
		return
	}
	if obj == nil {
		printRawJSON(raw)
		return
	}
	r.printDetail(p, obj)
}

func (r cloudResource) printDetail(p *printer.Printer, obj map[string]any) {
	fields := r.detail
	if len(fields) == 0 {
		fields = r.cols
	}
	rows := make([][]string, 0, len(fields))
	for _, f := range fields {
		rows = append(rows, []string{f.header, cell(lookup(obj, f.key))})
	}
	p.Table([]string{"Field", "Value"}, rows)
	if r.subtables != nil {
		r.subtables(p, obj)
	}
}

// subtable prints obj[key] (a list of objects) under a title.
func subtable(p *printer.Printer, obj map[string]any, key, title string, cols []col) {
	list, _ := obj[key].([]any)
	if len(list) == 0 {
		return
	}
	items := make([]map[string]any, 0, len(list))
	for _, it := range list {
		if m, ok := it.(map[string]any); ok {
			items = append(items, m)
		}
	}
	headers, rows := rowsOf(items, cols)
	p.TableAdvanced(title, headers, rows)
}

// deleteCmd is DELETE <path>/{id}, after a confirmation.
func (r cloudResource) deleteCmd() *cobra.Command {
	var (
		yes bool
		w   waitFlags
	)
	cmd := &cobra.Command{
		Use:     "delete <id>",
		Aliases: []string{"rm"},
		Short:   "Delete a " + r.singular,
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := parseCloudID(args[0], r.singular)
			if err != nil {
				return err
			}
			if !yes && !confirm(fmt.Sprintf("Delete %s #%d?", r.singular, id)) {
				printer.Info("Cancelled.")
				return nil
			}
			cc := requireCloud()
			var raw json.RawMessage
			if err := cc.Delete(r.itemPath(id), nil, nil, &raw); err != nil {
				handleAPIError(err)
				return nil
			}
			reportMutation(cc, raw, fmt.Sprintf("Deletion of %s #%d accepted.", r.singular, id), w)
			return nil
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "skip the confirmation prompt")
	w.add(cmd)
	return cmd
}

// patchCmd is PATCH <path>/{id} with the fields build puts in the body.
func (r cloudResource) patchCmd(example string, flags func(cmd *cobra.Command), build func(cmd *cobra.Command, body map[string]any) error) *cobra.Command {
	var (
		labels    labelFlags
		projectID int64
	)
	cmd := &cobra.Command{
		Use:     "update <id>",
		Short:   "Change a " + r.singular + "'s name, labels, protection or project",
		Example: example,
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := parseCloudID(args[0], r.singular)
			if err != nil {
				return err
			}
			body := map[string]any{}
			if err := labels.set(body); err != nil {
				return err
			}
			projectFlag(cmd, body, projectID)
			if build != nil {
				if err := build(cmd, body); err != nil {
					return err
				}
			}
			if len(body) == 0 {
				return fmt.Errorf("nothing to change: pass at least one flag (see --help)")
			}
			cc := requireCloud()
			var raw json.RawMessage
			if err := cc.Patch(r.itemPath(id), body, &raw); err != nil {
				handleAPIError(err)
				return nil
			}
			if jsonMode() {
				r.printItem(raw)
				return nil
			}
			printer.Success(fmt.Sprintf("%s #%d updated.", r.title(), id))
			return nil
		},
	}
	labels.add(cmd, true)
	cmd.Flags().Int64Var(&projectID, "project-id", 0, "move it to this project")
	if flags != nil {
		flags(cmd)
	}
	return cmd
}

// opCmd is POST <path>/{id}/actions/<op> with the body build returns.
func (r cloudResource) opCmd(use, op, short, example string, flags func(cmd *cobra.Command), build func(cmd *cobra.Command) (any, error)) *cobra.Command {
	var w waitFlags
	cmd := &cobra.Command{
		Use:     use + " <id>",
		Short:   short,
		Example: example,
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := parseCloudID(args[0], r.singular)
			if err != nil {
				return err
			}
			var body any
			if build != nil {
				if body, err = build(cmd); err != nil {
					return err
				}
			}
			cc := requireCloud()
			var raw json.RawMessage
			if err := cc.Post(r.itemPath(id)+"/actions/"+op, body, &raw); err != nil {
				handleAPIError(err)
				return nil
			}
			reportMutation(cc, raw, fmt.Sprintf("%s accepted for %s #%d.", capitalize(strings.ReplaceAll(use, "-", " ")), r.singular, id), w)
			return nil
		},
	}
	w.add(cmd)
	if flags != nil {
		flags(cmd)
	}
	return cmd
}

// createCmd is POST <path> with the body build returns.
func (r cloudResource) createCmd(example string, flags func(cmd *cobra.Command), build func(cmd *cobra.Command, body map[string]any) error) *cobra.Command {
	var (
		labels    labelFlags
		projectID int64
		w         waitFlags
	)
	cmd := &cobra.Command{
		Use:     "create",
		Short:   "Create a " + r.singular,
		Example: example,
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			body := map[string]any{}
			if err := labels.set(body); err != nil {
				return err
			}
			projectFlag(cmd, body, projectID)
			if err := build(cmd, body); err != nil {
				return err
			}
			cc := requireCloud()
			var raw json.RawMessage
			if err := cc.Post(r.path, body, &raw); err != nil {
				handleAPIError(err)
				return nil
			}
			reportMutation(cc, raw, r.title()+" creation accepted.", w)
			return nil
		},
	}
	labels.add(cmd, false)
	cmd.Flags().Int64Var(&projectID, "project-id", 0, "project to create it in (default: the account's default project)")
	w.add(cmd)
	if flags != nil {
		flags(cmd)
	}
	return cmd
}

// ─── Mutations and actions ────────────────────────────────────────────────────

type waitFlags struct {
	wait    bool
	timeout time.Duration
}

func (w *waitFlags) add(cmd *cobra.Command) {
	cmd.Flags().BoolVar(&w.wait, "wait", false, "wait until the operation finishes")
	cmd.Flags().DurationVar(&w.timeout, "timeout", defaultWaitTimeout, "how long --wait waits")
}

// actionIDs collects action_id and servers[].action_id from a response.
func actionIDs(resp map[string]any) []int64 {
	var ids []int64
	seen := map[int64]bool{}
	addID := func(v any) {
		n, ok := v.(json.Number)
		if !ok {
			return
		}
		id, err := n.Int64()
		if err != nil || id <= 0 || seen[id] {
			return
		}
		seen[id] = true
		ids = append(ids, id)
	}
	addID(resp["action_id"])
	if a, ok := resp["action"].(map[string]any); ok {
		addID(a["id"])
	}
	if servers, ok := resp["servers"].([]any); ok {
		for _, s := range servers {
			if m, ok := s.(map[string]any); ok {
				addID(m["action_id"])
			}
		}
	}
	return ids
}

// reportMutation prints what a change returned and, with --wait, follows
// its actions. In JSON mode the response is printed as is, with the final
// actions under "actions" after a wait.
func reportMutation(cc *api.CloudClient, raw []byte, done string, w waitFlags) {
	resp := map[string]any{}
	if len(bytes.TrimSpace(raw)) > 0 {
		var err error
		if resp, err = decodeObject(raw); err != nil {
			handleAPIError(err)
			return
		}
	}
	ids := actionIDs(resp)

	outcome := waitDone
	if w.wait && len(ids) > 0 {
		var finals []*api.CloudAction
		finals, outcome = waitForActions(cc, ids, w.timeout)
		if jsonMode() {
			resp["actions"] = finals
		}
	}

	if jsonMode() {
		printer.New(cfg.Output).PrintJSON(resp)
	} else {
		printMutation(resp, ids, done, w.wait)
	}
	exitForOutcome(outcome)
}

func printMutation(resp map[string]any, ids []int64, done string, waited bool) {
	msg := done
	if obj, ok := resp["data"].(map[string]any); ok {
		if id := cell(obj["id"]); id != "-" {
			msg += " ID: " + id
			if name := cell(obj["name"]); name != "-" {
				msg += fmt.Sprintf(" (%s)", name)
			}
		}
	}
	printer.Success(msg)
	if pw, ok := resp["root_password"].(string); ok && pw != "" {
		fmt.Printf("  Root password: %s\n", printer.CleanLine(pw))
		fmt.Println("  It is shown only this once - save it now.")
		fmt.Println()
	}
	if waited || len(ids) == 0 {
		return
	}
	for _, id := range ids {
		printer.Info(fmt.Sprintf("Action #%d is running. Follow it with: arianet cloud actions wait %d", id, id))
	}
	fmt.Println()
}

// exitForOutcome ends the process non-zero when a wait did not succeed.
func exitForOutcome(outcome waitOutcome) {
	if outcome != waitDone {
		os.Exit(1)
	}
}
