package commands

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/ariaservice/arianet-cli/internal/api"
	"github.com/ariaservice/arianet-cli/internal/config"
	"github.com/ariaservice/arianet-cli/internal/printer"
	"github.com/spf13/cobra"
)

var cloudVolume = cloudResource{
	plural: "volumes", singular: "volume", path: "/volumes",
	cols: []col{
		{"ID", "id"}, {"Name", "name"}, {"Size (GB)", "size_gb"}, {"Status", "status"},
		{"Datacenter", "datacenter"}, {"Server", "server_id"}, {"Labels", "labels"},
	},
	detail: []col{
		{"ID", "id"}, {"Name", "name"}, {"Size (GB)", "size_gb"}, {"Status", "status"},
		{"Datacenter", "datacenter"}, {"Server", "server_id"}, {"Device", "device"},
		{"Filesystem", "filesystem"}, {"Disk bus", "disk_bus"}, {"Delete protection", "protection_delete"},
		{"Labels", "labels"}, {"Project", "project_id"}, {"Created", "created_at"},
	},
}

func newCloudVolumesCmd() *cobra.Command {
	r := cloudVolume
	cmd := &cobra.Command{
		Use:     "volumes",
		Aliases: []string{"volume"},
		Short:   "Manage block storage volumes",
	}
	cmd.AddCommand(
		r.listCmd(`  arianet cloud volumes list
  arianet cloud volumes list --label-selector env=prod -o json`, nil, nil),
		r.getCmd(),
		r.createCmd(`  arianet cloud volumes create --name data --size 50 --datacenter eu-central-1
  arianet cloud volumes create --name data --size 50 --server 12 --filesystem ext4 --wait`,
			func(cmd *cobra.Command) {
				cmd.Flags().String("name", "", "volume name")
				cmd.Flags().Int("size", 0, "size in GB")
				cmd.Flags().String("datacenter", "", "datacenter code (not needed with --server)")
				cmd.Flags().Int64("server", 0, "attach to this server once created")
				cmd.Flags().String("filesystem", "", "ext4 or xfs (what the volume is meant for)")
				_ = cmd.MarkFlagRequired("name")
				_ = cmd.MarkFlagRequired("size")
			}, func(cmd *cobra.Command, body map[string]any) error {
				changedString(cmd, body, "name", "name")
				changedInt(cmd, body, "size", "size_gb")
				changedString(cmd, body, "datacenter", "datacenter")
				changedInt64(cmd, body, "server", "server_id")
				changedString(cmd, body, "filesystem", "filesystem")
				return nil
			}),
		r.patchCmd(`  arianet cloud volumes update 7 --name logs --labels env=prod`, func(cmd *cobra.Command) {
			cmd.Flags().String("name", "", "new name")
			cmd.Flags().Bool("protection-delete", false, "protect against deletion (true/false)")
			cmd.Flags().String("filesystem", "", "ext4, xfs or \"\"")
		}, func(cmd *cobra.Command, body map[string]any) error {
			changedString(cmd, body, "name", "name")
			changedBool(cmd, body, "protection-delete", "protection_delete")
			changedString(cmd, body, "filesystem", "filesystem")
			return nil
		}),
		r.deleteCmd(),
		r.opCmd("attach", "attach", "Attach a volume to a server", "  arianet cloud volumes attach 7 --server 12 --wait",
			func(cmd *cobra.Command) {
				cmd.Flags().Int64("server", 0, "server ID")
				_ = cmd.MarkFlagRequired("server")
			}, func(cmd *cobra.Command) (any, error) {
				id, _ := cmd.Flags().GetInt64("server")
				return map[string]any{"server_id": id}, nil
			}),
		r.opCmd("detach", "detach", "Detach a volume from its server", "  arianet cloud volumes detach 7", nil, nil),
		r.opCmd("resize", "resize", "Grow a volume", "  arianet cloud volumes resize 7 --size 100 --wait",
			func(cmd *cobra.Command) {
				cmd.Flags().Int("size", 0, "new size in GB (larger than now)")
				_ = cmd.MarkFlagRequired("size")
			}, func(cmd *cobra.Command) (any, error) {
				size, _ := cmd.Flags().GetInt("size")
				return map[string]any{"size_gb": size}, nil
			}),
	)
	return cmd
}

var imageCols = []col{
	{"ID", "id"}, {"Image", "image"}, {"Type", "type"}, {"Name", "name"}, {"Datacenters", "datacenters"},
	{"Min disk (GB)", "min_disk_gb"}, {"Status", "status"}, {"Labels", "labels"},
}

func newCloudImagesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "images",
		Aliases: []string{"image"},
		Short:   "List installable images, snapshots and backups",
	}

	var typ, datacenter string
	imageFilters := func() url.Values {
		q := url.Values{}
		q.Set("type", typ)
		q.Set("datacenter", datacenter)
		return q
	}
	r := cloudResource{plural: "images", singular: "image", path: "/images", cols: imageCols}
	list := r.listCmd(`  arianet cloud images list
  arianet cloud images list --type system --datacenter eu-central-1
  arianet cloud images list --type snapshot --label-selector env=prod`, func(cmd *cobra.Command) {
		cmd.Flags().StringVar(&typ, "type", "", "system, app, snapshot or backup")
		cmd.Flags().StringVar(&datacenter, "datacenter", "", "only images available in this datacenter")
		cmd.Flags().StringVar(&datacenter, "location", "", "alias of --datacenter")
	}, imageFilters)

	get := &cobra.Command{
		Use:   "get <id-or-image>",
		Short: "Show one image, by its ID or its image name",
		Long: `Show one image. The cloud API has no single-image endpoint, so this reads the
image list and picks the entry whose id or image field matches.`,
		Example: `  arianet cloud images get ubuntu-24.04
  arianet cloud images get 41`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cc := requireCloud()
			raw, _, err := cc.List("/images", api.ListOptions{})
			if err != nil {
				handleAPIError(err)
				return nil
			}
			items, err := objects(raw)
			if err != nil {
				handleAPIError(err)
				return nil
			}
			want := strings.TrimSpace(args[0])
			for i, it := range items {
				if cell(it["id"]) == want || cell(it["image"]) == want {
					if jsonMode() {
						var v any
						v, _ = decodeAny(raw[i])
						printer.New(cfg.Output).PrintJSON(v)
						return nil
					}
					r.printDetail(printer.New(cfg.Output), it)
					return nil
				}
			}
			return fmt.Errorf("no image %q (see: arianet cloud images list)", want)
		},
	}
	cmd.AddCommand(list, get)
	return cmd
}

// ─── Catalog: pricing, plans, datacenters, limits ─────────────────────────────

// dataList reads {"data": [...]} from path.
func dataList(path string, q url.Values) ([]json.RawMessage, bool) {
	cc := requireCloud()
	var out api.CloudList
	if err := cc.Get(path, q, &out); err != nil {
		handleAPIError(err)
		return nil, false
	}
	return out.Data, true
}

// catalogCmd prints a {"data": [...]} read as a generic table, or as JSON
// passed through, so a new field in the response needs no CLI change.
func catalogCmd(use, short, path, example string, preferred []string, aliases ...string) *cobra.Command {
	var datacenter string
	cmd := &cobra.Command{
		Use:     use,
		Aliases: aliases,
		Short:   short,
		Example: example,
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			q := url.Values{}
			if datacenter != "" {
				q.Set("datacenter", datacenter)
			}
			raw, ok := dataList(path, q)
			if !ok {
				return nil
			}
			showGeneric(raw, preferred)
			return nil
		},
	}
	cmd.Flags().StringVar(&datacenter, "datacenter", "", "only this datacenter")
	return cmd
}

func showGeneric(raw []json.RawMessage, preferred []string) {
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
		printer.Info("Nothing found.")
		return
	}
	genericTable(p, items, preferred)
}

// newCloudPricingCmd prints the public price list, GET /public/pricing,
// which needs no token. --account shows the account's own price rows
// (GET /pricing, token required).
func newCloudPricingCmd() *cobra.Command {
	var (
		datacenter string
		account    bool
	)
	cmd := &cobra.Command{
		Use:     "pricing",
		Aliases: []string{"prices"},
		Short:   "Show the public price list (no token needed)",
		Long: `Show every server type per location and every other price on sale (IPs,
volumes, snapshots, backups, load balancers, NAT gateways), VAT excluded.
No API token is needed. --account shows your account's own price rows instead.`,
		Example: `  arianet cloud pricing
  arianet cloud pricing --datacenter ovh
  arianet cloud pricing -o json
  arianet cloud pricing --account`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if account {
				q := url.Values{}
				if datacenter != "" {
					q.Set("datacenter", datacenter)
				}
				raw, ok := dataList("/pricing", q)
				if !ok {
					return nil
				}
				showGeneric(raw, []string{"datacenter", "sku", "unit", "hourly_price", "monthly_price", "percent", "currency"})
				return nil
			}
			cc, err := publicCloud()
			if err != nil {
				handleAPIError(err)
				return nil
			}
			var raw json.RawMessage
			if err := cc.Get("/public/pricing", nil, &raw); err != nil {
				handleAPIError(err)
				return nil
			}
			if jsonMode() {
				printRawJSON(raw)
				return nil
			}
			resp, err := decodeObject(raw)
			if err != nil {
				handleAPIError(err)
				return nil
			}
			pricing, _ := resp["pricing"].(map[string]any)
			if pricing == nil {
				printRawJSON(raw)
				return nil
			}
			printPublicPricing(printer.New(cfg.Output), pricing, datacenter)
			return nil
		},
	}
	cmd.Flags().StringVar(&datacenter, "datacenter", "", "only this datacenter (location code)")
	cmd.Flags().StringVar(&datacenter, "location", "", "alias of --datacenter")
	cmd.Flags().BoolVar(&account, "account", false, "your account's price rows (GET /pricing; needs a token)")
	return cmd
}

// publicCloud is a cloud client that sends no credential, for public
// endpoints. It refuses the same API URLs requireAuth refuses.
func publicCloud() (*api.CloudClient, error) {
	apiURL := config.DefaultAPIURL
	if cfg != nil && cfg.APIURL != "" {
		apiURL = cfg.APIURL
	}
	if err := config.ValidateAPIURL(apiURL); err != nil {
		return nil, err
	}
	return api.New(apiURL, "").Cloud(), nil
}

func printPublicPricing(p *printer.Printer, pricing map[string]any, location string) {
	var serverRows [][]string
	types, _ := pricing["server_types"].([]any)
	for _, t := range types {
		st, ok := t.(map[string]any)
		if !ok {
			continue
		}
		prices, _ := st["prices"].([]any)
		for _, pr := range prices {
			lp, ok := pr.(map[string]any)
			if !ok || (location != "" && cell(lp["location"]) != location) {
				continue
			}
			serverRows = append(serverRows, []string{
				cell(st["id"]), cell(st["name"]), cell(lp["location"]), cell(st["cores"]), cell(st["memory_mb"]),
				cell(st["disk_gb"]), cell(st["included_traffic_gb"]), cell(lookup(lp, "price_hourly.net")),
				cell(lookup(lp, "price_monthly.net")), cell(lookup(lp, "price_monthly.currency")),
			})
		}
	}
	if len(serverRows) > 0 {
		p.TableAdvanced("Server types", []string{"ID", "Name", "Location", "Cores", "Memory (MB)", "Disk (GB)", "Traffic (GB)", "Hourly", "Monthly", "Currency"}, serverRows)
	}

	var resourceRows [][]string
	resources, _ := pricing["resources"].([]any)
	for _, r := range resources {
		rp, ok := r.(map[string]any)
		if !ok || (location != "" && cell(rp["location"]) != location) {
			continue
		}
		resourceRows = append(resourceRows, []string{
			cell(rp["sku"]), cell(rp["location"]), cell(rp["unit"]), cell(lookup(rp, "price_hourly.net")),
			cell(lookup(rp, "price_monthly.net")), cell(rp["percent"]), cell(lookup(rp, "price_monthly.currency")),
		})
	}
	if len(resourceRows) > 0 {
		p.TableAdvanced("Other resources", []string{"SKU", "Location", "Unit", "Hourly", "Monthly", "Percent", "Currency"}, resourceRows)
	}
	if len(serverRows) == 0 && len(resourceRows) == 0 {
		printer.Info("No prices found.")
		return
	}
	note := "Prices exclude VAT."
	if h := cell(pricing["hours_per_month"]); h != "-" {
		note += " Monthly prices are quoted over " + h + " hours."
	}
	printer.Info(note)
}

func newCloudPlansCmd() *cobra.Command {
	return catalogCmd("plans", "List server plans (server types)", "/plans",
		"  arianet cloud plans --datacenter eu-central-1",
		[]string{"id", "datacenter", "name", "vcpu", "ram_mb", "disk_gb", "traffic_gb", "hourly_price", "monthly_price", "currency"},
		"server-types", "plan")
}

func newCloudDatacentersCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "datacenters",
		Aliases: []string{"locations", "datacenter", "regions"},
		Short:   "List datacenters (locations) you can create resources in",
		Example: "  arianet cloud datacenters",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			raw, ok := dataList("/datacenters", nil)
			if !ok {
				return nil
			}
			if jsonMode() {
				showGeneric(raw, nil)
				return nil
			}
			items, err := objects(raw)
			if err != nil {
				handleAPIError(err)
				return nil
			}
			headers, rows := rowsOf(items, []col{
				{"Code", "code"}, {"Name", "name"}, {"Country", "country"}, {"City", "city"},
				{"Status", "status"}, {"Orderable", "orderable"}, {"Features", "features"},
			})
			printer.New(cfg.Output).Table(headers, rows)
			return nil
		},
	}
	return cmd
}

func newCloudLimitsCmd() *cobra.Command {
	var datacenter string
	cmd := &cobra.Command{
		Use:     "limits",
		Aliases: []string{"quotas"},
		Short:   "Show the account's limits and usage per datacenter",
		Example: "  arianet cloud limits\n  arianet cloud limits --datacenter eu-central-1",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			q := url.Values{}
			if datacenter != "" {
				q.Set("datacenter", datacenter)
			}
			raw, ok := dataList("/limits", q)
			if !ok {
				return nil
			}
			if jsonMode() {
				showGeneric(raw, nil)
				return nil
			}
			items, err := objects(raw)
			if err != nil {
				handleAPIError(err)
				return nil
			}
			// One row per datacenter and resource.
			var rows [][]string
			for _, dc := range items {
				limits, _ := dc["limits"].([]any)
				for _, l := range limits {
					m, ok := l.(map[string]any)
					if !ok {
						continue
					}
					rows = append(rows, []string{cell(dc["datacenter"]), cell(m["resource"]), cell(m["used"]), cell(m["limit"]), cell(m["source"])})
				}
			}
			if len(rows) == 0 {
				printer.Info("No limits found.")
				return nil
			}
			printer.New(cfg.Output).Table([]string{"Datacenter", "Resource", "Used", "Limit", "Source"}, rows)
			return nil
		},
	}
	cmd.Flags().StringVar(&datacenter, "datacenter", "", "only this datacenter")
	return cmd
}

// newCloudSSHKeysCmd is the account's SSH keys: they live in core and are
// served by /api/v1/ssh-keys, so this is the ssh command under cloud.
func newCloudSSHKeysCmd() *cobra.Command {
	cmd := newSSHCmd()
	cmd.Use = "ssh-keys"
	cmd.Aliases = []string{"ssh-key", "ssh"}
	cmd.Short = "Manage the account's SSH keys (same as: arianet ssh)"
	for _, sub := range cmd.Commands() {
		if sub.Name() == "add" {
			sub.Aliases = append(sub.Aliases, "create")
		}
	}
	return cmd
}
