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

// derefStr safely dereferences a *string, returning "" if nil.
func derefStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// derefBool safely dereferences a *bool, returning false if nil.
func derefBool(b *bool) bool {
	if b == nil {
		return false
	}
	return *b
}

// optIntPtr returns a *int pointer if v > 0, otherwise nil.
func optIntPtr(v int) *int {
	if v == 0 {
		return nil
	}
	return &v
}

// optStrPtr returns a *string pointer if s != "", otherwise nil.
func optStrPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
