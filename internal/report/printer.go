package report

import (
	"fmt"
	"strings"
	"time"

	"github.com/debraj-b/kubedoctor/internal/checks"
	"github.com/pterm/pterm"
)

const maxNameLen = 30 // truncate long resource names to keep table clean

// Summary holds the scan metadata shown in the header.
type Summary struct {
	ClusterName string
	Namespaces  int
	Pods        int
	Deployments int
	Services    int
	Duration    time.Duration
}

// Print renders the full report — header, per-namespace tables, log summaries, and summary bar.
func Print(findings []checks.Finding, summary Summary) {
	printHeader(summary)

	if len(findings) == 0 {
		pterm.Success.Println("No issues found. Cluster looks healthy!")
		printSummaryBar(findings)
		return
	}

	// Group findings by namespace
	grouped := groupByNamespace(findings)

	// Print each namespace section
	for _, ns := range namespaceOrder(grouped) {
		nsFindings := grouped[ns]
		sorted := sortFindings(nsFindings)

		// Namespace header
		pterm.DefaultSection.
			WithStyle(pterm.NewStyle(pterm.FgCyan, pterm.Bold)).
			Printf("Namespace: %s (%d finding(s))\n", ns, len(sorted))

		// Compact table — 4 columns, no logs
		printFindingsTable(sorted)

		// Log summaries below the table for critical pod failures only
		printLogSummaries(sorted)
	}

	printSummaryBar(findings)
}

// printHeader renders the scan metadata at the top.
func printHeader(s Summary) {
	fmt.Println()
	pterm.DefaultHeader.
		WithFullWidth(true).
		WithBackgroundStyle(pterm.NewStyle(pterm.BgDarkGray)).
		WithTextStyle(pterm.NewStyle(pterm.FgWhite, pterm.Bold)).
		Println("  K8S DOCTOR — Cluster Health Report  ")

	pterm.Info.Printfln(
		"Cluster: %s   Namespaces: %d   Pods: %d   Deployments: %d   Services: %d   Duration: %s",
		s.ClusterName, s.Namespaces, s.Pods, s.Deployments, s.Services,
		s.Duration.Round(time.Millisecond),
	)
	fmt.Println()
}

// printFindingsTable renders a compact 4-column bordered table for a namespace's findings.
func printFindingsTable(findings []checks.Finding) {
	tableData := pterm.TableData{
		{"SEVERITY", "KIND", "NAME", "MESSAGE"},
	}

	for _, f := range findings {
		tableData = append(tableData, []string{
			severityColored(f.Severity),
			f.Kind,
			truncate(stripPodHash(f.Name), maxNameLen),
			truncateWords(f.Message, 65),
		})
	}

	pterm.DefaultTable.
		WithHasHeader(true).
		WithBoxed(true).
		WithHeaderRowSeparator("─").
		WithData(tableData).
		Render()

	fmt.Println()
}

// printLogSummaries prints structured log summaries for critical pod failures.
func printLogSummaries(findings []checks.Finding) {
	var criticalWithLogs []checks.Finding
	for _, f := range findings {
		if f.Severity == checks.Critical && f.Kind == "Pod" && f.LogSummary != nil {
			criticalWithLogs = append(criticalWithLogs, f)
		}
	}

	if len(criticalWithLogs) == 0 {
		return
	}

	pterm.FgGray.Println("  Crash Details:")

	for _, f := range criticalWithLogs {
		s := f.LogSummary
		name := truncate(stripPodHash(f.Name), maxNameLen)

		// Box per pod crash
		content := fmt.Sprintf(
			" Pod      : %s\n Exit Code : %s\n Pattern   : %s\n Last Log  : %s",
			name,
			exitCodeLabel(s.ExitCode),
			patternLabel(s.Pattern),
			wrapLine(s.LastLine, 70),
		)
		pterm.DefaultBox.
			WithTitle(pterm.FgRed.Sprint("CRITICAL")).
			WithTitleTopLeft().
			Println(content)
	}

	fmt.Println()
}

// printSummaryBar renders counts and overall health at the bottom.
func printSummaryBar(findings []checks.Finding) {
	critical, warning, info := countBySeverity(findings)
	overall, style := overallHealth(critical, warning)

	pterm.DefaultBox.Printfln(
		"%s   %s   %s   Overall: %s",
		pterm.FgRed.Sprintf("CRITICAL  %d", critical),
		pterm.FgYellow.Sprintf("WARNING   %d", warning),
		pterm.FgCyan.Sprintf("INFO      %d", info),
		style.Sprint(overall),
	)
	fmt.Println()
}

// groupByNamespace groups findings by their namespace.
func groupByNamespace(findings []checks.Finding) map[string][]checks.Finding {
	grouped := make(map[string][]checks.Finding)
	for _, f := range findings {
		grouped[f.Namespace] = append(grouped[f.Namespace], f)
	}
	return grouped
}

// namespaceOrder returns namespaces sorted so those with CRITICAL findings come first.
func namespaceOrder(grouped map[string][]checks.Finding) []string {
	var critical, rest []string
	for ns, findings := range grouped {
		hasCritical := false
		for _, f := range findings {
			if f.Severity == checks.Critical {
				hasCritical = true
				break
			}
		}
		if hasCritical {
			critical = append(critical, ns)
		} else {
			rest = append(rest, ns)
		}
	}
	return append(critical, rest...)
}

// sortFindings orders findings: CRITICAL → WARNING → INFO.
func sortFindings(findings []checks.Finding) []checks.Finding {
	order := map[string]int{checks.Critical: 0, checks.Warning: 1, checks.Info: 2}
	sorted := make([]checks.Finding, len(findings))
	copy(sorted, findings)
	for i := 1; i < len(sorted); i++ {
		for j := i; j > 0 && order[sorted[j].Severity] < order[sorted[j-1].Severity]; j-- {
			sorted[j], sorted[j-1] = sorted[j-1], sorted[j]
		}
	}
	return sorted
}

// stripPodHash removes the replicaset and pod hash suffix from pod names.
// e.g. "crashloop-app-5fbbd76764-tqcn7" → "crashloop-app"
// Only strips segments that look like actual K8s hashes (short, purely alphanumeric).
func stripPodHash(name string) string {
	parts := strings.Split(name, "-")
	if len(parts) <= 2 {
		return name
	}
	last := parts[len(parts)-1]
	secondLast := parts[len(parts)-2]
	if isHash(last) && isHash(secondLast) {
		return strings.Join(parts[:len(parts)-2], "-")
	}
	return name
}

// isHash returns true if a string looks like a K8s-generated hash segment
// (short, lowercase alphanumeric only — not a real word like "app" or "limits").
func isHash(s string) bool {
	if len(s) < 4 || len(s) > 12 {
		return false
	}
	// Real hashes are purely lowercase letters and digits with no vowel-heavy patterns
	digits := 0
	for _, c := range s {
		if c >= '0' && c <= '9' {
			digits++
		} else if c < 'a' || c > 'z' {
			return false
		}
	}
	// Must have at least one digit — real words like "app" or "limits" have none
	return digits >= 1
}

// truncate shortens a string to max length with ".." suffix.
func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-2] + ".."
}

// truncateWords shortens a string at the last complete word boundary before max chars.
// Appends ".." so the user knows it's cut. Full detail is in the log summary section.
func truncateWords(s string, max int) string {
	if len(s) <= max {
		return s
	}
	cut := s[:max]
	lastSpace := strings.LastIndex(cut, " ")
	if lastSpace > 0 {
		cut = cut[:lastSpace]
	}
	return cut + ".."
}

// wrapLine wraps a long line at word boundaries up to the given width.
func wrapLine(line string, width int) string {
	if len(line) <= width {
		return line
	}
	words := strings.Fields(line)
	var current, result string
	for _, word := range words {
		if current == "" {
			current = word
		} else if len(current)+1+len(word) <= width {
			current += " " + word
		} else {
			result += current + "\n             "
			current = word
		}
	}
	if current != "" {
		result += current
	}
	return result
}

// exitCodeLabel returns a human-readable exit code label.
func exitCodeLabel(code string) string {
	switch code {
	case "1":
		return "1 (general error)"
	case "137":
		return "137 (OOMKilled / SIGKILL)"
	case "143":
		return "143 (SIGTERM)"
	case "126":
		return "126 (permission denied)"
	case "127":
		return "127 (command not found)"
	case "":
		return "unknown"
	default:
		return code
	}
}

// patternLabel colors the pattern string based on severity.
func patternLabel(pattern string) string {
	if pattern == "unknown" || pattern == "" {
		return pterm.FgGray.Sprint("unknown")
	}
	return pterm.FgYellow.Sprint(pattern)
}

// severityColored returns a colored severity string.
func severityColored(severity string) string {
	switch severity {
	case checks.Critical:
		return pterm.FgRed.Sprint(severity)
	case checks.Warning:
		return pterm.FgYellow.Sprint(severity)
	case checks.Info:
		return pterm.FgCyan.Sprint(severity)
	default:
		return severity
	}
}

// countBySeverity returns counts per severity level.
func countBySeverity(findings []checks.Finding) (critical, warning, info int) {
	for _, f := range findings {
		switch f.Severity {
		case checks.Critical:
			critical++
		case checks.Warning:
			warning++
		case checks.Info:
			info++
		}
	}
	return
}

// overallHealth returns health label and style.
func overallHealth(critical, warning int) (string, *pterm.Style) {
	switch {
	case critical > 0:
		return "CRITICAL", pterm.NewStyle(pterm.FgRed, pterm.Bold)
	case warning > 0:
		return "DEGRADED", pterm.NewStyle(pterm.FgYellow, pterm.Bold)
	default:
		return "HEALTHY", pterm.NewStyle(pterm.FgGreen, pterm.Bold)
	}
}
