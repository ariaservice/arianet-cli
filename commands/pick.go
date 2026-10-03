package commands

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/ariaservice/arianet-cli/internal/api"
	"github.com/ariaservice/arianet-cli/internal/printer"
)

// datacenterTable prints the orderable locations.
func datacenterTable(p *printer.Printer, list []api.DatacenterEntry) {
	rows := make([][]string, len(list))
	for i, d := range list {
		rows[i] = []string{strconv.Itoa(d.ID), d.Name, d.Region, d.Country, d.Status,
			supportLabel(d.Supports, "ssh_keys"), supportLabel(d.Supports, "firewalls")}
	}
	p.Table([]string{"ID", "Name", "Region", "Country", "Status", "SSH keys", "Firewalls"}, rows)
}

// supportLabel renders one capability flag; "-" means the API did not say.
func supportLabel(supports map[string]bool, name string) string {
	ok, known := supports[name]
	switch {
	case !known:
		return "-"
	case ok:
		return "yes"
	default:
		return "no"
	}
}

func flattenForDisplay(regions []api.Region) []api.DatacenterEntry {
	return api.FlattenDatacenters(regions)
}

// chooseDatacenter lists the locations and asks which one to use.
func chooseDatacenter(client *api.Client, prompt string) (int, bool, error) {
	list, err := client.ListDatacenters()
	if err != nil {
		return 0, false, err
	}
	if len(list) == 0 {
		printer.Info("No regions available.")
		return 0, false, nil
	}

	datacenterTable(printer.New(cfg.Output), list)

	reader := bufio.NewReader(os.Stdin)
	for {
		fmt.Printf("\n%s: ", prompt)
		line, err := reader.ReadString('\n')
		input := strings.TrimSpace(line)
		if err != nil && input == "" {
			return 0, false, fmt.Errorf("no region selected; pass --region <id>")
		}
		id, convErr := strconv.Atoi(input)
		if convErr != nil || id <= 0 {
			printer.Warn("Invalid input. Please enter a valid region ID.")
			continue
		}
		for _, d := range list {
			if d.ID == id {
				return id, true, nil
			}
		}
		printer.Warn(fmt.Sprintf("Region ID %d not found. Please try again.", id))
	}
}
