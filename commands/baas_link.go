package commands

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/ariaservice/arianet-cli/internal/printer"
	"github.com/spf13/cobra"
)

// link / unlink: a directory remembers its project in
// arianet/.baas/project.json, so every command that takes the project ID as
// its first argument can leave it out there.

// baasProjectDir is the directory of the project files (migrations, the
// link) relative to the app's directory.
const baasProjectDir = "arianet"

var baasLinkFile = filepath.Join(baasProjectDir, ".baas", "project.json")

// baasLink is what link writes.
type baasLink struct {
	ProjectID string `json:"project_id"`
	Ref       string `json:"ref"`
	Name      string `json:"name"`
	Endpoint  string `json:"endpoint"`
	Region    string `json:"region"`
}

// errNotLinked is returned when the project ID is left out outside a
// linked directory.
var errNotLinked = errors.New("no project given: pass the project ID, or run arianet baas link in this directory first")

// readBaaSLink reads the current directory's link; nil when there is none.
func readBaaSLink() (*baasLink, error) {
	b, err := os.ReadFile(baasLinkFile)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var l baasLink
	if err := json.Unmarshal(b, &l); err != nil {
		return nil, fmt.Errorf("%s is not valid JSON: %w (run arianet baas link again)", baasLinkFile, err)
	}
	if _, err := projectID(l.ProjectID); err != nil {
		return nil, fmt.Errorf("%s: %w (run arianet baas link again)", baasLinkFile, err)
	}
	return &l, nil
}

// linkedProjectID is the linked project's ID, or errNotLinked.
func linkedProjectID() (string, error) {
	l, err := readBaaSLink()
	if err != nil {
		return "", err
	}
	if l == nil {
		return "", errNotLinked
	}
	label := "#" + l.ProjectID
	if l.Name != "" {
		label = l.Name + " (#" + l.ProjectID + ")"
	}
	fmt.Fprintf(os.Stderr, "Using the linked project %s\n", printer.CleanLine(label))
	return l.ProjectID, nil
}

// splitProject separates the project ID from a command's other arguments.
// The project ID comes first and is a number; in a linked directory it may
// be left out. The command takes minRest to maxRest other arguments
// (maxRest < 0: no upper bound). A first argument that is a number is the
// project ID whenever the count allows it.
func splitProject(args []string, minRest, maxRest int) (string, []string, error) {
	fits := func(n int) bool { return n >= minRest && (maxRest < 0 || n <= maxRest) }
	if len(args) > 0 && digitsOnly.MatchString(strings.TrimSpace(args[0])) && fits(len(args)-1) {
		return strings.TrimSpace(args[0]), args[1:], nil
	}
	if fits(len(args)) {
		id, err := linkedProjectID()
		if err != nil {
			return "", nil, err
		}
		return id, args, nil
	}
	if len(args) > 0 && fits(len(args)-1) {
		_, err := projectID(args[0])
		return "", nil, err
	}
	want := fmt.Sprintf("%d", minRest)
	switch {
	case maxRest < 0:
		want = fmt.Sprintf("at least %d", minRest)
	case maxRest != minRest:
		want = fmt.Sprintf("%d to %d", minRest, maxRest)
	}
	return "", nil, fmt.Errorf("expected the project ID (optional in a linked directory) and %s more argument(s), got %d argument(s)", want, len(args))
}

// projectArgsRange is the cobra argument check of a command whose first
// argument is the project ID, which may be left out when linked.
func projectArgsRange(minRest, maxRest int) cobra.PositionalArgs {
	if maxRest < 0 {
		return cobra.MinimumNArgs(minRest)
	}
	return cobra.RangeArgs(minRest, maxRest+1)
}

// optionalProjectUse shows the project ID as optional in a usage line.
func optionalProjectUse(use string) string {
	return strings.Replace(use, "<project-id>", "[project-id]", 1)
}

func (p *product) baasLinkCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "link [project-id|ref]",
		Short: "Link this directory to a project, so the project ID can be left out here",
		Long: `Link the current directory to a project: arianet/.baas/project.json records its ID, ref,
name, endpoint and region. Afterwards every baas command run in this directory that takes the
project ID as its first argument uses the linked project when the ID is left out; an explicit
ID still wins. Without an argument, your only project is linked (or the list is shown).
The file holds no secret and can be committed.`,
		Example: `  arianet baas link 12
  arianet baas link abcdefghij0123456789
  arianet baas db push          # the linked project`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cc := p.client()
			if cc == nil {
				return nil
			}
			var project map[string]any
			arg := ""
			if len(args) == 1 {
				arg = strings.TrimSpace(args[0])
			}
			if digitsOnly.MatchString(arg) {
				var out struct {
					Data map[string]any `json:"data"`
				}
				if err := cc.Get("/projects/"+arg, nil, &out); err != nil {
					handleAPIError(err)
					return nil
				}
				project = out.Data
			} else {
				var out struct {
					Data []map[string]any `json:"data"`
				}
				if err := cc.Get("/projects", nil, &out); err != nil {
					handleAPIError(err)
					return nil
				}
				switch {
				case arg != "":
					for _, pr := range out.Data {
						if cell(pr["ref"]) == arg {
							project = pr
						}
					}
					if project == nil {
						return fmt.Errorf("no project has the ref %q (arianet baas projects list)", arg)
					}
				case len(out.Data) == 1:
					project = out.Data[0]
				case len(out.Data) == 0:
					return fmt.Errorf("you have no project yet: arianet baas projects create --name <name> --plan free")
				default:
					headers, rows := rowsOf(out.Data, baasProjectCols)
					printer.New(cfg.Output).Table(headers, rows)
					return fmt.Errorf("you have %d projects: give the ID or ref of the one to link", len(out.Data))
				}
			}
			l := baasLink{ProjectID: cell(project["id"]), Ref: str(project["ref"]), Name: str(project["name"]),
				Endpoint: str(project["endpoint"]), Region: str(project["region"])}
			if _, err := projectID(l.ProjectID); err != nil {
				return fmt.Errorf("unexpected project in the answer: %w", err)
			}
			b, _ := json.MarshalIndent(l, "", "  ")
			if err := os.MkdirAll(filepath.Dir(baasLinkFile), 0o755); err != nil {
				return err
			}
			if err := os.WriteFile(baasLinkFile, append(b, '\n'), 0o644); err != nil {
				return err
			}
			if jsonMode() {
				printer.New("json").PrintJSON(l)
				return nil
			}
			printer.Success(fmt.Sprintf("Linked this directory to project %s (#%s, ref %s) in %s.", l.Name, l.ProjectID, l.Ref, baasLinkFile))
			return nil
		},
	}
}

func (p *product) baasUnlinkCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "unlink",
		Short: "Remove this directory's project link",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			l, _ := readBaaSLink()
			if err := os.Remove(baasLinkFile); errors.Is(err, fs.ErrNotExist) {
				printer.Info("This directory is not linked to a project.")
				return nil
			} else if err != nil {
				return err
			}
			_ = os.Remove(filepath.Dir(baasLinkFile)) // only when empty
			if l != nil {
				printer.Success(fmt.Sprintf("Unlinked project %s (#%s).", l.Name, l.ProjectID))
			} else {
				printer.Success("Removed the project link.")
			}
			return nil
		},
	}
}

// str is a JSON string field, empty when missing.
func str(v any) string {
	s, _ := v.(string)
	return s
}

// withProject is args with the project ID first: as given, or the linked
// project's put in front (see splitProject).
func withProject(args []string, minRest, maxRest int) ([]string, error) {
	id, rest, err := splitProject(args, minRest, maxRest)
	if err != nil {
		return nil, err
	}
	return append([]string{id}, rest...), nil
}
