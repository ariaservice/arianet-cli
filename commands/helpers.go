package commands

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/arianet/arianet-cli/internal/config"
)

// confirm prompts the user for a yes/no answer. Returns true if they confirm.
func confirm(prompt string) bool {
	fmt.Printf("%s (y/N): ", prompt)
	reader := bufio.NewReader(os.Stdin)
	response, _ := reader.ReadString('\n')
	response = strings.TrimSpace(strings.ToLower(response))
	return response == "y" || response == "yes"
}

// saveConfig persists the in-memory cfg to disk.
func saveConfig() error {
	return config.Save(cfg)
}
