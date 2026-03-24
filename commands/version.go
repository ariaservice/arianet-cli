package commands

import (
	"fmt"

	"github.com/arianet/arianet-cli/pkg/version"
	"github.com/spf13/cobra"
)

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the CLI version",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("arianet %s\n", version.Version)
			fmt.Printf("  commit:     %s\n", version.Commit)
			fmt.Printf("  built:      %s\n", version.BuildDate)
			fmt.Printf("  source:     https://github.com/arianet/arianet-cli\n")
		},
	}
}
