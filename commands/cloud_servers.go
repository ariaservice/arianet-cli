package commands

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"

	"github.com/ariaservice/arianet-cli/internal/printer"
	"github.com/spf13/cobra"
)

var cloudServer = cloudResource{
	plural: "servers", singular: "server", path: "/servers",
	cols: []col{
		{"ID", "id"}, {"Name", "name"}, {"Status", "status"}, {"Datacenter", "datacenter"},
		{"Plan", "plan_id"}, {"Image", "image"}, {"IPv4", "ipv4"}, {"IPv6", "ipv6"}, {"Labels", "labels"},
	},
	detail: []col{
		{"ID", "id"}, {"Name", "name"}, {"Status", "status"}, {"Datacenter", "datacenter"},
		{"Plan ID", "plan_id"}, {"Disk (GB)", "disk_gb"}, {"Image", "image"}, {"IPv4", "ipv4"}, {"IPv6", "ipv6"},
		{"Rescue", "rescue_enabled"}, {"Locked", "locked"}, {"Locked reason", "locked_reason"},
		{"Delete protection", "protection_delete"}, {"Rebuild protection", "protection_rebuild"},
		{"Labels", "labels"}, {"Project", "project_id"}, {"Created", "created_at"},
	},
}

func newCloudServersCmd() *cobra.Command {
	r := cloudServer
	cmd := &cobra.Command{
		Use:     "servers",
		Aliases: []string{"server"},
		Short:   "Manage cloud servers",
	}
	cmd.AddCommand(
		r.listCmd(`  arianet cloud servers list
  arianet cloud servers list --label-selector env=prod
  arianet cloud servers list --page 1 --per-page 50 -o json`, nil, nil),
		r.getCmd(),
		newCloudServerCreateCmd(),
		r.patchCmd(`  arianet cloud servers update 12 --name web-2
  arianet cloud servers update 12 --labels env=prod --labels tier=web
  arianet cloud servers update 12 --protection-delete=true`, func(cmd *cobra.Command) {
			cmd.Flags().String("name", "", "new name")
			cmd.Flags().Bool("protection-delete", false, "protect against deletion (true/false)")
			cmd.Flags().Bool("protection-rebuild", false, "protect against rebuild (true/false)")
		}, func(cmd *cobra.Command, body map[string]any) error {
			changedString(cmd, body, "name", "name")
			changedBool(cmd, body, "protection-delete", "protection_delete")
			changedBool(cmd, body, "protection-rebuild", "protection_rebuild")
			return nil
		}),
		newCloudServerDeleteCmd(),
		r.opCmd("power-on", "poweron", "Power a server on", "  arianet cloud servers power-on 12 --wait", nil, nil),
		r.opCmd("power-off", "poweroff", "Cut a server's power (hard stop)", "  arianet cloud servers power-off 12", nil, nil),
		r.opCmd("shutdown", "shutdown", "Shut a server down gracefully (ACPI)", "  arianet cloud servers shutdown 12 --wait", nil, nil),
		r.opCmd("reboot", "reboot", "Reboot a server gracefully", "  arianet cloud servers reboot 12", nil, nil),
		r.opCmd("reset", "reset", "Hard-reset a server", "  arianet cloud servers reset 12", nil, nil),
		r.opCmd("rebuild", "rebuild", "Reinstall a server from an image (erases its disk)",
			"  arianet cloud servers rebuild 12 --image ubuntu-24.04 --wait",
			func(cmd *cobra.Command) {
				cmd.Flags().String("image", "", "image to install (see: arianet cloud images list)")
				_ = cmd.MarkFlagRequired("image")
			}, func(cmd *cobra.Command) (any, error) {
				image, _ := cmd.Flags().GetString("image")
				return map[string]any{"image": image}, nil
			}),
		withAliases(r.opCmd("change-type", "change_plan", "Resize a server to another plan",
			"  arianet cloud servers change-type 12 --plan 7 --wait",
			func(cmd *cobra.Command) {
				cmd.Flags().Int64("plan", 0, "plan ID (see: arianet cloud plans)")
				_ = cmd.MarkFlagRequired("plan")
			}, func(cmd *cobra.Command) (any, error) {
				plan, _ := cmd.Flags().GetInt64("plan")
				return map[string]any{"plan_id": plan}, nil
			}), "resize", "change-plan"),
		r.opCmd("enable-rescue", "enable_rescue", "Boot a server into rescue mode (prints the rescue password once)", "  arianet cloud servers enable-rescue 12", nil, nil),
		r.opCmd("disable-rescue", "disable_rescue", "Leave rescue mode", "  arianet cloud servers disable-rescue 12", nil, nil),
		r.opCmd("reset-password", "reset_password", "Set a new root password (printed once)", "  arianet cloud servers reset-password 12", nil, nil),
		r.opCmd("change-ipv4", "change_ipv4", "Give a server a new public IPv4 address",
			"  arianet cloud servers change-ipv4 12\n  arianet cloud servers change-ipv4 12 --confirm-paid",
			func(cmd *cobra.Command) {
				cmd.Flags().Bool("confirm-paid", false, "accept the fee of a paid change")
			}, func(cmd *cobra.Command) (any, error) {
				paid, _ := cmd.Flags().GetBool("confirm-paid")
				return map[string]any{"confirm_paid": paid}, nil
			}),
		r.opCmd("attach-network", "attach_to_network", "Attach a server to a private network",
			"  arianet cloud servers attach-network 12 --network 4\n  arianet cloud servers attach-network 12 --network 4 --ip 10.0.1.5",
			func(cmd *cobra.Command) {
				cmd.Flags().Int64("network", 0, "network ID")
				cmd.Flags().Int64("subnet", 0, "subnet ID (default: the network's first)")
				cmd.Flags().String("ip", "", "private IP to use (default: the next free one)")
				_ = cmd.MarkFlagRequired("network")
			}, func(cmd *cobra.Command) (any, error) {
				body := map[string]any{}
				network, _ := cmd.Flags().GetInt64("network")
				body["network_id"] = network
				changedInt64(cmd, body, "subnet", "subnet_id")
				changedString(cmd, body, "ip", "ip")
				return body, nil
			}),
		r.opCmd("detach-network", "detach_from_network", "Detach a server from a private network",
			"  arianet cloud servers detach-network 12 --network 4",
			func(cmd *cobra.Command) {
				cmd.Flags().Int64("network", 0, "network ID")
				_ = cmd.MarkFlagRequired("network")
			}, func(cmd *cobra.Command) (any, error) {
				network, _ := cmd.Flags().GetInt64("network")
				return map[string]any{"network_id": network}, nil
			}),
		r.opCmd("enable-backup", "enable_backup", "Turn on daily backups",
			"  arianet cloud servers enable-backup 12 --window-hour 3",
			func(cmd *cobra.Command) {
				cmd.Flags().Int("window-hour", 0, "hour of the day (UTC, 0-23) the backup runs")
			}, func(cmd *cobra.Command) (any, error) {
				body := map[string]any{}
				changedInt(cmd, body, "window-hour", "window_hour")
				return body, nil
			}),
		r.opCmd("disable-backup", "disable_backup", "Turn off daily backups", "  arianet cloud servers disable-backup 12", nil, nil),
		r.opCmd("change-backup-window", "change_backup_window", "Move the daily backup to another hour",
			"  arianet cloud servers change-backup-window 12 --window-hour 4",
			func(cmd *cobra.Command) {
				cmd.Flags().Int("window-hour", 0, "hour of the day (UTC, 0-23)")
				_ = cmd.MarkFlagRequired("window-hour")
			}, func(cmd *cobra.Command) (any, error) {
				hour, _ := cmd.Flags().GetInt("window-hour")
				return map[string]any{"window_hour": hour}, nil
			}),
		r.opCmd("create-backup", "create_backup", "Take a backup now", "  arianet cloud servers create-backup 12 --wait", nil, nil),
		r.opCmd("restore-backup", "restore_backup", "Restore a server from one of its backups (erases its disk)",
			"  arianet cloud servers restore-backup 12 --backup 33 --wait",
			func(cmd *cobra.Command) {
				cmd.Flags().Int64("backup", 0, "backup ID (see: arianet cloud images list --type backup)")
				_ = cmd.MarkFlagRequired("backup")
			}, func(cmd *cobra.Command) (any, error) {
				id, _ := cmd.Flags().GetInt64("backup")
				return map[string]any{"backup_id": id}, nil
			}),
		newCloudServerCreateImageCmd(),
		newCloudServerActionsCmd(),
	)
	return cmd
}

func withAliases(cmd *cobra.Command, aliases ...string) *cobra.Command {
	cmd.Aliases = append(cmd.Aliases, aliases...)
	return cmd
}

func changedString(cmd *cobra.Command, body map[string]any, flag, key string) {
	if cmd.Flags().Changed(flag) {
		v, _ := cmd.Flags().GetString(flag)
		body[key] = v
	}
}

func changedBool(cmd *cobra.Command, body map[string]any, flag, key string) {
	if cmd.Flags().Changed(flag) {
		v, _ := cmd.Flags().GetBool(flag)
		body[key] = v
	}
}

func changedInt(cmd *cobra.Command, body map[string]any, flag, key string) {
	if cmd.Flags().Changed(flag) {
		v, _ := cmd.Flags().GetInt(flag)
		body[key] = v
	}
}

func changedInt64(cmd *cobra.Command, body map[string]any, flag, key string) {
	if cmd.Flags().Changed(flag) {
		v, _ := cmd.Flags().GetInt64(flag)
		body[key] = v
	}
}

func changedIDs(cmd *cobra.Command, body map[string]any, flag, key, what string) error {
	if !cmd.Flags().Changed(flag) {
		return nil
	}
	values, _ := cmd.Flags().GetStringArray(flag)
	ids, err := parseCloudIDs(values, what)
	if err != nil {
		return err
	}
	body[key] = ids
	return nil
}

func newCloudServerCreateCmd() *cobra.Command {
	return cloudServer.createCmd(`  arianet cloud servers create --name web-1 --datacenter eu-central-1 --plan 3 --image ubuntu-24.04 --ssh-key-id 5 --wait
  arianet cloud servers create --name web --count 3 --datacenter eu-central-1 --plan 3 --image ubuntu-24.04 \
      --labels env=prod --firewall 2 --network 4 --no-ipv6
  arianet cloud servers create --name db-1 --datacenter eu-central-1 --plan 5 --image debian-12 \
      --user-data-file cloud-init.yaml --backups --project-id 2 -o json`,
		func(cmd *cobra.Command) {
			f := cmd.Flags()
			f.String("name", "", "server name (with --count: name, name-2, ...)")
			f.String("datacenter", "", "datacenter code (see: arianet cloud datacenters)")
			f.Int64("plan", 0, "plan ID (see: arianet cloud plans)")
			f.String("image", "", "image (see: arianet cloud images list)")
			f.StringArray("ssh-key-id", nil, "saved SSH key ID (repeatable; see: arianet cloud ssh-keys list)")
			f.StringArray("ssh-key", nil, "public key to install (repeatable)")
			f.String("user-data-file", "", "cloud-init user data file")
			f.Bool("no-start", false, "do not start the server after creating it")
			f.StringArray("firewall", nil, "firewall ID to apply (repeatable)")
			f.Int64("placement-group", 0, "placement group ID")
			f.Bool("backups", false, "turn on daily backups")
			f.StringArray("network", nil, "private network ID to attach (repeatable)")
			f.Bool("no-ipv4", false, "no public IPv4 address")
			f.Bool("no-ipv6", false, "no public IPv6 address")
			f.StringArray("volume", nil, "volume ID to attach (repeatable)")
			f.Int("count", 0, "create this many servers at once")
			_ = cmd.MarkFlagRequired("name")
			_ = cmd.MarkFlagRequired("datacenter")
			_ = cmd.MarkFlagRequired("plan")
			_ = cmd.MarkFlagRequired("image")
		},
		func(cmd *cobra.Command, body map[string]any) error {
			f := cmd.Flags()
			changedString(cmd, body, "name", "name")
			changedString(cmd, body, "datacenter", "datacenter")
			changedInt64(cmd, body, "plan", "plan_id")
			changedString(cmd, body, "image", "image")
			changedInt64(cmd, body, "placement-group", "placement_group_id")
			changedBool(cmd, body, "backups", "backups")
			changedInt(cmd, body, "count", "count")
			if err := changedIDs(cmd, body, "ssh-key-id", "ssh_key_ids", "SSH key"); err != nil {
				return err
			}
			if err := changedIDs(cmd, body, "firewall", "firewall_ids", "firewall"); err != nil {
				return err
			}
			if err := changedIDs(cmd, body, "volume", "volume_ids", "volume"); err != nil {
				return err
			}
			if keys, _ := f.GetStringArray("ssh-key"); len(keys) > 0 {
				for _, k := range keys {
					if err := checkPublicKey(k); err != nil {
						return err
					}
				}
				body["ssh_keys"] = keys
			}
			if path, _ := f.GetString("user-data-file"); path != "" {
				data, err := os.ReadFile(expandHome(path))
				if err != nil {
					return fmt.Errorf("cannot read user data: %w", err)
				}
				body["user_data"] = string(data)
			}
			if noStart, _ := f.GetBool("no-start"); noStart {
				body["start_after_create"] = false
			}
			networks := map[string]any{}
			if noV4, _ := f.GetBool("no-ipv4"); noV4 {
				networks["enable_ipv4"] = false
			}
			if noV6, _ := f.GetBool("no-ipv6"); noV6 {
				networks["enable_ipv6"] = false
			}
			if f.Changed("network") {
				values, _ := f.GetStringArray("network")
				ids, err := parseCloudIDs(values, "network")
				if err != nil {
					return err
				}
				networks["network_ids"] = ids
			}
			if len(networks) > 0 {
				body["networks"] = networks
			}
			return nil
		})
}

func newCloudServerDeleteCmd() *cobra.Command {
	var (
		yes             bool
		w               waitFlags
		reason, note    string
		keepBackups     bool
		deleteSnapshots bool
	)
	cmd := &cobra.Command{
		Use:     "delete <id>",
		Aliases: []string{"rm"},
		Short:   "Delete a server",
		Example: `  arianet cloud servers delete 12
  arianet cloud servers delete 12 --yes --wait
  arianet cloud servers delete 12 --keep-backups=false --delete-snapshots`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := parseCloudID(args[0], "server")
			if err != nil {
				return err
			}
			if !yes && !confirm(fmt.Sprintf("Delete server #%d? Its disk is erased.", id)) {
				printer.Info("Cancelled.")
				return nil
			}
			q := url.Values{}
			if reason != "" {
				q.Set("reason", reason)
			}
			if note != "" {
				q.Set("note", note)
			}
			var body map[string]any
			if cmd.Flags().Changed("keep-backups") || cmd.Flags().Changed("delete-snapshots") {
				body = map[string]any{}
				changedBool(cmd, body, "keep-backups", "keep_backups")
				changedBool(cmd, body, "delete-snapshots", "delete_snapshots")
			}
			cc := requireCloud()
			var raw json.RawMessage
			var payload any
			if body != nil {
				payload = body
			}
			if err := cc.Delete(cloudServer.itemPath(id), q, payload, &raw); err != nil {
				handleAPIError(err)
				return nil
			}
			reportMutation(cc, raw, fmt.Sprintf("Deletion of server #%d accepted.", id), w)
			return nil
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "skip the confirmation prompt")
	cmd.Flags().StringVar(&reason, "reason", "", "why it is deleted (optional feedback)")
	cmd.Flags().StringVar(&note, "note", "", "free-text note with the reason")
	cmd.Flags().BoolVar(&keepBackups, "keep-backups", false, "keep the server's backups after deletion")
	cmd.Flags().BoolVar(&deleteSnapshots, "delete-snapshots", false, "also delete the server's snapshots")
	w.add(cmd)
	return cmd
}

func newCloudServerCreateImageCmd() *cobra.Command {
	var labels labelFlags
	cmd := cloudServer.opCmd("create-image", "create_image", "Take a snapshot image of a server",
		"  arianet cloud servers create-image 12 --name before-upgrade --wait",
		func(cmd *cobra.Command) {
			cmd.Flags().String("name", "", "snapshot name")
			cmd.Flags().String("description", "", "snapshot description")
			labels.add(cmd, false)
		}, func(cmd *cobra.Command) (any, error) {
			body := map[string]any{}
			changedString(cmd, body, "name", "name")
			changedString(cmd, body, "description", "description")
			if err := labels.set(body); err != nil {
				return nil, err
			}
			return body, nil
		})
	return cmd
}

func newCloudServerActionsCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "actions <id>",
		Short:   "List a server's actions",
		Example: "  arianet cloud servers actions 12",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := parseCloudID(args[0], "server")
			if err != nil {
				return err
			}
			cc := requireCloud()
			var out struct {
				Data []json.RawMessage `json:"data"`
			}
			if err := cc.Get(cloudServer.itemPath(id)+"/actions", nil, &out); err != nil {
				handleAPIError(err)
				return nil
			}
			showActions(out.Data)
			return nil
		},
	}
}
