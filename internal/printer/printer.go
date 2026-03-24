package printer

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/mattn/go-runewidth"
	"gopkg.in/yaml.v3"
)

// strWidth returns the visible display width of a string,
// correctly handling multi-byte Unicode characters (Persian, Arabic, CJK, etc.).
func strWidth(s string) int {
	return runewidth.StringWidth(s)
}

// Format constants.
const (
	FormatTable   = "table"
	FormatJSON    = "json"
	FormatYAML    = "yaml"
	FormatCompact = "compact"
)

// OutputMode controls how responses are displayed.
type OutputMode string

const (
	ModeNormal  OutputMode = "normal"
	ModeCompact OutputMode = "compact"
	ModeVerbose OutputMode = "verbose"
	ModeTree    OutputMode = "tree"
)

// ─── Plain-text style helpers ─────────────────────────────────────────────────
// These are intentionally no-ops — no ANSI codes, no color.

// StatusColor returns the status string as-is.
func StatusColor(status string) string { return status }

// StatusBadge returns the status string as-is.
func StatusBadge(status string) string { return status }

// BoolCheck returns "Yes" or "No".
func BoolCheck(v bool) string {
	if v {
		return "Yes"
	}
	return "No"
}

// Bold returns the string unchanged.
func Bold(s string) string { return s }

// Cyan returns the string unchanged.
func Cyan(s string) string { return s }

// Dim returns the string unchanged.
func Dim(s string) string { return s }

// FormatValue returns a plain string representation of a value.
func FormatValue(v interface{}) string {
	if v == nil {
		return "null"
	}
	return fmt.Sprintf("%v", v)
}

// ─── Standalone output functions ──────────────────────────────────────────────

// Success prints a success message to stdout.
func Success(msg string) {
	fmt.Fprintf(os.Stdout, "\nSuccess: %s\n\n", msg)
}

// Error prints an error message to stderr.
func Error(msg string) {
	fmt.Fprintf(os.Stderr, "\nError: %s\n\n", msg)
}

// Info prints an informational line to stdout.
func Info(msg string) {
	fmt.Fprintf(os.Stdout, "  %s\n", msg)
}

// Warn prints a warning line to stdout.
func Warn(msg string) {
	fmt.Fprintf(os.Stdout, "Warning: %s\n", msg)
}

// Hint prints a hint line to stdout.
func Hint(msg string) {
	fmt.Fprintf(os.Stdout, "  Hint: %s\n", msg)
}

// EmptyState prints an empty-state message with an optional hint.
func EmptyState(message, hint string) {
	fmt.Fprintln(os.Stdout)
	fmt.Fprintf(os.Stdout, "  %s\n", message)
	if hint != "" {
		fmt.Fprintf(os.Stdout, "  %s\n", hint)
	}
	fmt.Fprintln(os.Stdout)
}

// SectionHeader prints a section title.
func SectionHeader(title, subtitle string) {
	fmt.Fprintln(os.Stdout)
	if subtitle != "" {
		fmt.Fprintf(os.Stdout, "%s  %s\n", title, subtitle)
	} else {
		fmt.Fprintf(os.Stdout, "%s\n", title)
	}
}

// PaginationFooter prints a simple pagination summary.
func PaginationFooter(current, last, total int, resource string) {
	if total <= 0 || last <= 1 {
		return
	}
	fmt.Fprintf(os.Stdout, "\nPage %d of %d -- %d %s total\n\n", current, last, total, resource)
}

// ErrorWithSuggestion prints an error with optional suggestions.
func ErrorWithSuggestion(title string, details string, suggestions ...string) {
	fmt.Fprintln(os.Stdout)
	fmt.Fprintf(os.Stdout, "Error: %s\n", title)
	if details != "" {
		fmt.Fprintf(os.Stdout, "       %s\n", details)
	}
	if len(suggestions) > 0 {
		fmt.Fprintf(os.Stdout, "\nSuggestions:\n")
		for _, sug := range suggestions {
			fmt.Fprintf(os.Stdout, "  - %s\n", sug)
		}
	}
	fmt.Fprintln(os.Stdout)
}

// ProgressBar returns a plain ASCII progress bar string.
func ProgressBar(current, total int, width int) string {
	if width <= 0 {
		width = 20
	}
	if total <= 0 {
		total = 100
	}
	if current < 0 {
		current = 0
	}
	if current > total {
		current = total
	}
	filled := (current * width) / total
	empty := width - filled
	bar := strings.Repeat("#", filled) + strings.Repeat("-", empty)
	percent := (current * 100) / total
	return fmt.Sprintf("[%s] %d%%", bar, percent)
}

// LoadingSpinner returns a plain character for the given frame.
func LoadingSpinner(frame int) string {
	spinners := []string{"-", "\\", "|", "/"}
	return spinners[frame%len(spinners)]
}

// ─── Printer ──────────────────────────────────────────────────────────────────

// Printer handles formatted output.
type Printer struct {
	Out    io.Writer
	Format string
	Mode   OutputMode
}

// New returns a Printer writing to stdout with the given format.
func New(format string) *Printer {
	if format == "" {
		format = FormatTable
	}
	return &Printer{
		Out:    os.Stdout,
		Format: format,
		Mode:   ModeNormal,
	}
}

// WithMode sets the output mode.
func (p *Printer) WithMode(mode OutputMode) *Printer {
	p.Mode = mode
	return p
}

// PrintJSON marshals v to indented JSON and writes it.
func (p *Printer) PrintJSON(v interface{}) {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "json error: %v\n", err)
		return
	}
	fmt.Fprintln(p.Out, string(b))
}

// PrintYAML marshals v to YAML and writes it.
func (p *Printer) PrintYAML(v interface{}) {
	b, err := yaml.Marshal(v)
	if err != nil {
		fmt.Fprintf(os.Stderr, "yaml error: %v\n", err)
		return
	}
	fmt.Fprintln(p.Out, string(b))
}

// PrintCompact outputs data as compact JSON (one line).
func (p *Printer) PrintCompact(v interface{}) {
	data, err := json.Marshal(v)
	if err != nil {
		fmt.Fprintf(os.Stderr, "compact error: %v\n", err)
		return
	}
	fmt.Fprintln(p.Out, string(data))
}

// Print outputs data in the configured format.
func (p *Printer) Print(v interface{}) {
	switch p.Format {
	case FormatJSON:
		p.PrintJSON(v)
	case FormatYAML:
		p.PrintYAML(v)
	case FormatCompact:
		p.PrintCompact(v)
	default:
		p.PrintJSON(v)
	}
}

// ─── Table rendering ──────────────────────────────────────────────────────────

// Table renders an AWS CLI-style box table.
func (p *Printer) Table(headers []string, rows [][]string) {
	p.renderTable("", headers, rows)
}

// TableAdvanced renders an optionally titled AWS CLI-style box table.
func (p *Printer) TableAdvanced(title string, headers []string, rows [][]string) {
	p.renderTable(title, headers, rows)
}

func (p *Printer) renderTable(title string, headers []string, rows [][]string) {
	if len(headers) == 0 {
		return
	}

	// Calculate column widths using visible character width (handles Persian/Arabic/CJK).
	widths := make([]int, len(headers))
	for i, h := range headers {
		widths[i] = strWidth(h)
	}
	for _, row := range rows {
		for i, cell := range row {
			if i < len(widths) {
				if w := strWidth(cell); w > widths[i] {
					widths[i] = w
				}
			}
		}
	}

	sep := buildSeparator(widths)

	fmt.Fprintln(p.Out)
	if title != "" {
		fmt.Fprintf(p.Out, "%s\n", title)
	}

	// Top border
	fmt.Fprintln(p.Out, sep)

	// Header row (uppercase column names)
	var sb strings.Builder
	sb.WriteString("|")
	for i, h := range headers {
		label := strings.ToUpper(h)
		sb.WriteString(" ")
		sb.WriteString(label)
		sb.WriteString(strings.Repeat(" ", widths[i]-strWidth(h)))
		sb.WriteString(" |")
	}
	fmt.Fprintln(p.Out, sb.String())

	// Header-body separator
	fmt.Fprintln(p.Out, sep)

	// Data rows
	for _, row := range rows {
		sb.Reset()
		sb.WriteString("|")
		for i := range headers {
			cell := ""
			if i < len(row) {
				cell = row[i]
			}
			pad := widths[i] - strWidth(cell)
			if pad < 0 {
				pad = 0
			}
			sb.WriteString(" ")
			sb.WriteString(cell)
			sb.WriteString(strings.Repeat(" ", pad))
			sb.WriteString(" |")
		}
		fmt.Fprintln(p.Out, sb.String())
	}

	// Bottom border
	fmt.Fprintln(p.Out, sep)
	fmt.Fprintln(p.Out)
}

func buildSeparator(widths []int) string {
	var sb strings.Builder
	sb.WriteString("+")
	for _, w := range widths {
		sb.WriteString(strings.Repeat("-", w+2))
		sb.WriteString("+")
	}
	return sb.String()
}

// ─── Detail rendering ─────────────────────────────────────────────────────────

// Detail renders a titled key-value panel.
func (p *Printer) Detail(title string, rows [][]string) {
	maxKey := 0
	for _, row := range rows {
		if len(row) >= 1 {
			if w := strWidth(row[0]); w > maxKey {
				maxKey = w
			}
		}
	}

	fmt.Fprintln(p.Out)
	if title != "" {
		fmt.Fprintf(p.Out, "%s\n", title)
		fmt.Fprintln(p.Out, strings.Repeat("-", maxKey+30))
	}

	for _, row := range rows {
		if len(row) < 2 {
			continue
		}
		pad := strings.Repeat(" ", maxKey-strWidth(row[0]))
		fmt.Fprintf(p.Out, "  %s%s  %s\n", row[0], pad, row[1])
	}
	fmt.Fprintln(p.Out)
}

// DetailExpandable renders a key-value panel (expandable markers are ignored, plain text).
func (p *Printer) DetailExpandable(title string, rows [][]string, expandableKeys map[string]bool) {
	p.Detail(title, rows)
}
