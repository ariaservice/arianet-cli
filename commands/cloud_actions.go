package commands

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"time"

	"github.com/ariaservice/arianet-cli/internal/api"
	"github.com/ariaservice/arianet-cli/internal/printer"
	"github.com/spf13/cobra"
)

// Cloud action states (GET /actions/{id}).
const (
	actionRunning = "running"
	actionSuccess = "success"
	actionError   = "error"
)

var actionCols = []col{
	{"ID", "id"}, {"Command", "command"}, {"Status", "status"}, {"Progress", "progress"},
	{"Resource", "resource.type"}, {"Resource ID", "resource.id"}, {"Started", "started_at"},
	{"Finished", "finished_at"}, {"Error", "error.message"},
}

// waitForActions polls the actions until each finished, one failed for
// good, or the timeout passed. Progress goes to stderr. It returns the last
// state read of every action.
func waitForActions(cc *api.CloudClient, ids []int64, timeout time.Duration) ([]*api.CloudAction, waitOutcome) {
	if timeout <= 0 {
		timeout = defaultWaitTimeout
	}
	start := time.Now()
	deadline := start.Add(timeout)
	finals := make([]*api.CloudAction, len(ids))
	lastLine := map[int64]string{}
	errCount := 0

	say := func(id int64, line string) {
		if lastLine[id] == line {
			return
		}
		lastLine[id] = line
		fmt.Fprintf(os.Stderr, "  [%s] %s\n", fmtElapsed(time.Since(start)), printer.CleanLine(line))
	}

	fmt.Fprintf(os.Stderr, "\n  Waiting for %s (Ctrl+C stops waiting; the operation continues)\n", actionsLabel(ids))

	for {
		pending, failed := 0, false
		for i, id := range ids {
			if finals[i] != nil && finals[i].Status != actionRunning {
				if finals[i].Status == actionError {
					failed = true
				}
				continue
			}
			a, err := cc.GetAction(id)
			switch {
			case err != nil && !pollErrorIsTransient(err):
				fmt.Fprintf(os.Stderr, "  Stopped waiting: %s\n", printer.CleanLine(err.Error()))
				return finals, waitAborted
			case err != nil:
				errCount++
				if errCount >= maxPollErrors {
					fmt.Fprintf(os.Stderr, "  Stopped waiting: %s\n", printer.CleanLine(err.Error()))
					return finals, waitAborted
				}
				pending++
				continue
			}
			errCount = 0
			finals[i] = a
			say(id, describeAction(a))
			switch a.Status {
			case actionError:
				failed = true
			case actionSuccess:
			default:
				pending++
			}
		}
		if pending == 0 {
			if failed {
				fmt.Fprintf(os.Stderr, "\n  %s failed. Details: arianet cloud actions get <id>\n\n", capitalize(actionsLabel(ids)))
				return finals, waitFailed
			}
			fmt.Fprintf(os.Stderr, "\n  %s finished.\n\n", capitalize(actionsLabel(ids)))
			return finals, waitDone
		}
		if !time.Now().Add(pollInterval).Before(deadline) {
			fmt.Fprintf(os.Stderr, "\n  Still running after %s. Check: arianet cloud actions get %d\n\n", timeout, ids[0])
			return finals, waitTimedOut
		}
		time.Sleep(pollInterval)
	}
}

func actionsLabel(ids []int64) string {
	if len(ids) == 1 {
		return fmt.Sprintf("action #%d", ids[0])
	}
	return fmt.Sprintf("%d actions", len(ids))
}

func describeAction(a *api.CloudAction) string {
	line := fmt.Sprintf("action #%d %s: %s", a.ID, a.Command, a.Status)
	if a.Status == actionRunning && a.Progress > 0 {
		line += fmt.Sprintf(" (%d%%)", a.Progress)
	}
	if a.Error != nil && a.Error.Message != "" {
		line += " - " + a.Error.Message
	}
	return line
}

func newCloudActionsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "actions",
		Aliases: []string{"action"},
		Short:   "Follow asynchronous operations (actions)",
	}
	cmd.AddCommand(newCloudActionsListCmd(), newCloudActionsGetCmd(), newCloudActionsWaitCmd())
	return cmd
}

func newCloudActionsListCmd() *cobra.Command {
	var (
		f            listFilters
		resourceType string
		resourceID   int64
		status       string
		since        string
		limit        int
	)
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List the account's actions, newest first",
		Example: `  arianet cloud actions list
  arianet cloud actions list --resource-type server --resource-id 12
  arianet cloud actions list --status running -o json`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			q := url.Values{}
			q.Set("resource_type", resourceType)
			q.Set("status", status)
			q.Set("since", since)
			if resourceID > 0 {
				q.Set("resource_id", strconv.FormatInt(resourceID, 10))
			}
			if limit > 0 {
				q.Set("limit", strconv.Itoa(limit))
			}
			opt, err := f.options(q)
			if err != nil {
				return err
			}
			// Without a page the activity is returned whole (bounded by
			// --limit), so it is read in one request.
			if opt.Page == 0 && opt.PerPage == 0 {
				cc := requireCloud()
				var out api.CloudList
				if err := cc.Get("/actions", q, &out); err != nil {
					handleAPIError(err)
					return nil
				}
				showActions(out.Data)
				return nil
			}
			runList("/actions", opt, "actions", func(p *printer.Printer, items []map[string]any) {
				headers, rows := rowsOf(items, actionCols)
				p.Table(headers, rows)
			})
			return nil
		},
	}
	cmd.Flags().IntVar(&f.page, "page", 0, "page number")
	cmd.Flags().IntVar(&f.perPage, "per-page", 0, "items per page, at most 100")
	cmd.Flags().StringVar(&resourceType, "resource-type", "", "only actions on this kind (server, volume, firewall, network...)")
	cmd.Flags().Int64Var(&resourceID, "resource-id", 0, "only actions on this resource")
	cmd.Flags().StringVar(&status, "status", "", "running, success or error")
	cmd.Flags().StringVar(&since, "since", "", "only actions started after this RFC 3339 time")
	cmd.Flags().IntVar(&limit, "limit", 0, "at most this many actions")
	return cmd
}

func showActions(raw []json.RawMessage) {
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
		printer.Info("No actions found.")
		return
	}
	headers, rows := rowsOf(items, actionCols)
	p.Table(headers, rows)
}

func newCloudActionsGetCmd() *cobra.Command {
	res := cloudResource{plural: "actions", singular: "action", path: "/actions", detail: actionCols}
	return res.getCmd()
}

func newCloudActionsWaitCmd() *cobra.Command {
	var timeout time.Duration
	cmd := &cobra.Command{
		Use:   "wait <id> [<id>...]",
		Short: "Wait until actions finish; exits non-zero if one fails or the timeout passes",
		Example: `  arianet cloud actions wait 812
  arianet cloud actions wait 812 813 --timeout 30m`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ids, err := parseCloudIDs(args, "action")
			if err != nil {
				return err
			}
			cc := requireCloud()
			finals, outcome := waitForActions(cc, ids, timeout)
			if jsonMode() {
				printer.New(cfg.Output).PrintJSON(finals)
			}
			exitForOutcome(outcome)
			return nil
		},
	}
	cmd.Flags().DurationVar(&timeout, "timeout", defaultWaitTimeout, "how long to wait")
	return cmd
}
