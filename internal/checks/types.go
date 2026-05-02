package checks

// Severity levels for findings
const (
	Critical = "CRITICAL"
	Warning  = "WARNING"
	Info     = "INFO"
)

// LogSummary holds a structured, summarised view of a pod's crash logs.
// Instead of raw lines, we extract only what matters.
type LogSummary struct {
	ExitCode string // exit code from last termination (e.g. "1", "137")
	Pattern  string // matched error category (e.g. "connection refused", "panic")
	LastLine string // the single most relevant log line containing the pattern
}

// Finding represents a single issue found during a cluster check.
type Finding struct {
	Severity   string     // CRITICAL / WARNING / INFO
	Namespace  string     // which namespace this came from
	Kind       string     // Pod / Deployment / Service
	Name       string     // the resource name
	Message    string     // what's wrong
	LogSummary *LogSummary // structured log summary (only for critical pod failures)
}
