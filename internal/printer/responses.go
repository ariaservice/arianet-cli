package printer

import (
	"fmt"
	"time"
)

// ResponseMeta holds optional metadata about an API response.
type ResponseMeta struct {
	StatusCode int
	Duration   time.Duration
	Cached     bool
	RequestID  string
}

// ─── Common Response Rendering Patterns ─────────────────────────────────────

// RenderListResponse is a helper for displaying list API responses
// Example usage: RenderListResponse(p, services, buildServiceRows)
func RenderListResponse(
	p *Printer,
	items []map[string]interface{},
	headers []string,
	rowBuilder func(item map[string]interface{}) []string,
) {
	if len(items) == 0 {
		EmptyState("No items found", "Try adjusting your search filters")
		return
	}

	rows := make([][]string, len(items))
	for i, item := range items {
		rows[i] = rowBuilder(item)
	}

	p.Table(headers, rows)
}

// RenderDetailResponse is a helper for displaying detail/get API responses
// Example: RenderDetailResponse(p, item, [][]string{{"Name", item.Name}})
func RenderDetailResponse(p *Printer, title string, details [][]string) {
	p.Detail(title, details)
}

// RenderError displays an API error with helpful context
func RenderError(p *Printer, statusCode int, message string, requestID string) {
	suggestions := []string{}

	switch statusCode {
	case 400:
		suggestions = append(suggestions,
			"Check that all required fields are provided",
			"Verify parameter format and values",
		)
	case 401, 403:
		suggestions = append(suggestions,
			"Verify your authentication token: arianet auth tokens list",
			"Ensure you have permission for this action",
		)
	case 404:
		suggestions = append(suggestions,
			"Check that the resource ID is correct",
			"List available resources: arianet server list",
		)
	case 429:
		suggestions = append(suggestions,
			"Rate limit exceeded - please wait before retrying",
			"Consider batching your requests",
		)
	case 500, 502, 503:
		suggestions = append(suggestions,
			"The service is temporarily unavailable",
			"Try again in a few moments",
		)
	}

	ErrorWithSuggestion(
		fmt.Sprintf("API Error %d", statusCode),
		message,
		suggestions...,
	)

	if requestID != "" {
		fmt.Fprintf(p.Out, "  %s Request ID: %s\n\n",
			Dim("↳"), Dim(requestID[:16]+"..."))
	}
}

// RenderCreateResponse shows confirmation of resource creation
func RenderCreateResponse(p *Printer, resourceType string, id interface{}, details map[string]interface{}) {
	Success(fmt.Sprintf("%s created successfully", resourceType))

	detailRows := [][]string{
		{"ID", fmt.Sprintf("%v", id)},
	}
	for key, value := range details {
		detailRows = append(detailRows, []string{key, fmt.Sprintf("%v", value)})
	}

	p.Detail(resourceType+" Details", detailRows)
}

// RenderUpdateResponse shows confirmation of resource update
func RenderUpdateResponse(p *Printer, resourceType string, changes map[string]interface{}) {
	Success(fmt.Sprintf("%s updated successfully", resourceType))

	var changeRows [][]string
	for key, value := range changes {
		changeRows = append(changeRows, []string{key, fmt.Sprintf("%v", value)})
	}

	if len(changeRows) > 0 {
		p.Detail("Changes", changeRows)
	}
}

// RenderDeleteResponse shows confirmation of resource deletion
func RenderDeleteResponse(p *Printer, resourceType string, id interface{}) {
	Success(fmt.Sprintf("%s deleted", resourceType))
	Hint(fmt.Sprintf("ID: %v", id))
}

// ─── Response Metadata Builders ──────────────────────────────────────────────

// NewResponseMeta creates a ResponseMeta with timing info
func NewResponseMeta(statusCode int, duration time.Duration) ResponseMeta {
	return ResponseMeta{
		StatusCode: statusCode,
		Duration:   duration,
	}
}

// ─── Table Row Builders ──────────────────────────────────────────────────────

// ServiceRow builds a table row for a service item
func ServiceRow(id, name, status, plan, region string) []string {
	return []string{
		id,
		name,
		StatusBadge(status),
		plan,
		region,
	}
}

// TokenRow builds a table row for an auth token
func TokenRow(name, prefix, lastUsed string) []string {
	return []string{
		name,
		Dim(prefix + "..."),
		lastUsed,
	}
}

// FirewallRuleRow builds a table row for a firewall rule
func FirewallRuleRow(port, protocol, source string, enabled bool) []string {
	return []string{
		port,
		protocol,
		source,
		BoolCheck(enabled),
	}
}

// ─── Summary Helpers ────────────────────────────────────────────────────────

// RenderSummary displays a summary of operations or results.
func RenderSummary(p *Printer, title string, stats map[string]interface{}) {
	fmt.Fprintln(p.Out)
	fmt.Fprintf(p.Out, "  %s\n\n", title)
	for key, value := range stats {
		fmt.Fprintf(p.Out, "  %s %s\n", key+":", FormatValue(value))
	}
	fmt.Fprintln(p.Out)
}

// RenderStats displays key statistics.
func RenderStats(p *Printer, stats []struct {
	Label string
	Value interface{}
	Icon  string
}) {
	fmt.Fprintln(p.Out)
	for _, stat := range stats {
		icon := stat.Icon
		if icon == "" {
			icon = ">"
		}
		fmt.Fprintf(p.Out, "  %s  %s: %s\n", icon, stat.Label, FormatValue(stat.Value))
	}
	fmt.Fprintln(p.Out)
}
