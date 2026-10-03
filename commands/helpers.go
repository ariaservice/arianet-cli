package commands

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"math/big"
	"os"
	"strconv"
	"strings"

	"github.com/arianet/arianet-cli/internal/config"
	"github.com/arianet/arianet-cli/internal/printer"
)

// confirm prompts the user for a yes/no answer. Returns true if they confirm.
// The prompt goes to stderr so it never mixes with --output json on stdout.
func confirm(prompt string) bool {
	fmt.Fprintf(os.Stderr, "%s (y/N): ", prompt)
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

// newIdempotencyKey returns a random key accepted by the API (8-128 chars of
// letters, digits and _-:.).
func newIdempotencyKey() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(fmt.Sprintf("crypto/rand unavailable: %v", err))
	}
	return "cli-" + hex.EncodeToString(b[:])
}

// generatePassword builds a root password with upper case, lower case, digits
// and one symbol (the API requires a symbol). The symbols are a conservative
// set that providers and shells handle safely.
func generatePassword(length int) (string, error) {
	const (
		upper = "ABCDEFGHJKLMNPQRSTUVWXYZ"
		lower = "abcdefghijkmnopqrstuvwxyz"
		digit = "23456789"
		sym   = "@#%+="
		all   = upper + lower + digit
	)
	pick := func(set string) (byte, error) {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(set))))
		if err != nil {
			return 0, err
		}
		return set[n.Int64()], nil
	}

	out := make([]byte, 0, length)
	for _, set := range []string{upper, lower, digit, sym} {
		c, err := pick(set)
		if err != nil {
			return "", err
		}
		out = append(out, c)
	}
	for len(out) < length {
		c, err := pick(all)
		if err != nil {
			return "", err
		}
		out = append(out, c)
	}
	// Shuffle so the guaranteed characters are not always first.
	for i := len(out) - 1; i > 0; i-- {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(i+1)))
		if err != nil {
			return "", err
		}
		j := int(n.Int64())
		out[i], out[j] = out[j], out[i]
	}
	return string(out), nil
}

// parseIDList turns "1,2,3" (or repeated values) into ints.
func parseIDList(values []string) ([]int, error) {
	var ids []int
	for _, v := range values {
		for _, part := range strings.Split(v, ",") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			id, err := strconv.Atoi(part)
			if err != nil || id <= 0 {
				return nil, fmt.Errorf("invalid server ID: %q", part)
			}
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return nil, fmt.Errorf("at least one server ID is required")
	}
	return ids, nil
}

// ptrOrDash renders an optional string for tables.
func ptrOrDash(s *string) string {
	if s == nil || *s == "" {
		return "-"
	}
	return *s
}

// reportDeleted confirms a removal, as JSON when --output json is used.
func reportDeleted(kind, id string) {
	if jsonMode() {
		printer.New(cfg.Output).PrintJSON(map[string]any{"deleted": true, "type": kind, "id": id})
		return
	}
	printer.Success(fmt.Sprintf("%s %s deleted.", kind, id))
}
