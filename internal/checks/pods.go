package checks

import (
	"context"
	"fmt"
	"io"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// knownPatterns maps keywords found in logs to a human-readable error category.
// Order matters — first match wins.
var knownPatterns = []struct {
	keyword  string
	category string
}{
	{"connection refused", "connection refused"},
	{"no such file", "missing file or binary"},
	{"permission denied", "permission denied"},
	{"out of memory", "out of memory"},
	{"OOM", "out of memory"},
	{"panic", "panic / unhandled exception"},
	{"timeout", "timeout"},
	{"segfault", "segmentation fault"},
	{"exec format error", "wrong binary architecture"},
	{"address already in use", "port conflict"},
	{"certificate", "TLS / certificate error"},
}

// CheckPods runs all pod-related checks in the given namespace.
func CheckPods(clientset *kubernetes.Clientset, namespace string) ([]Finding, error) {
	pods, err := clientset.CoreV1().Pods(namespace).List(context.Background(), metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list pods in namespace %q: %w", namespace, err)
	}

	var findings []Finding

	for _, pod := range pods.Items {
		findings = append(findings, checkPodStatus(clientset, pod)...)
		findings = append(findings, checkPodResources(pod)...)
		findings = append(findings, checkPodProbes(pod)...)
	}

	return findings, nil
}

// checkPodStatus checks the runtime state of the pod.
func checkPodStatus(clientset *kubernetes.Clientset, pod corev1.Pod) []Finding {
	var findings []Finding

	for _, cs := range pod.Status.ContainerStatuses {
		// CrashLoopBackOff
		if cs.State.Waiting != nil && cs.State.Waiting.Reason == "CrashLoopBackOff" {
			summary := buildLogSummary(clientset, pod, cs)
			findings = append(findings, Finding{
				Severity:   Critical,
				Namespace:  pod.Namespace,
				Kind:       "Pod",
				Name:       pod.Name,
				Message:    fmt.Sprintf("container %q is in CrashLoopBackOff", cs.Name),
				LogSummary: summary,
			})
		}

		// OOMKilled
		if cs.LastTerminationState.Terminated != nil &&
			cs.LastTerminationState.Terminated.Reason == "OOMKilled" {
			summary := buildLogSummary(clientset, pod, cs)
			findings = append(findings, Finding{
				Severity:   Critical,
				Namespace:  pod.Namespace,
				Kind:       "Pod",
				Name:       pod.Name,
				Message:    fmt.Sprintf("container %q was OOMKilled — memory limit too low", cs.Name),
				LogSummary: summary,
			})
		}

		// High restart count
		if cs.RestartCount > 5 {
			severity := Warning
			if cs.RestartCount > 20 {
				severity = Critical
			}
			findings = append(findings, Finding{
				Severity:  severity,
				Namespace: pod.Namespace,
				Kind:      "Pod",
				Name:      pod.Name,
				Message:   fmt.Sprintf("container %q has restarted %d times", cs.Name, cs.RestartCount),
			})
		}
	}

	// Pending pod
	if pod.Status.Phase == corev1.PodPending {
		findings = append(findings, Finding{
			Severity:  Warning,
			Namespace: pod.Namespace,
			Kind:      "Pod",
			Name:      pod.Name,
			Message:   "pod is Pending — may be unschedulable due to resource pressure",
		})
	}

	return findings
}

// buildLogSummary fetches the last logs from a crashed container and
// returns a structured summary: exit code, matched error pattern, and best log line.
func buildLogSummary(clientset *kubernetes.Clientset, pod corev1.Pod, cs corev1.ContainerStatus) *LogSummary {
	summary := &LogSummary{}

	// Exit code comes directly from the K8s API — no log parsing needed
	if cs.LastTerminationState.Terminated != nil {
		summary.ExitCode = fmt.Sprintf("%d", cs.LastTerminationState.Terminated.ExitCode)
	}

	// Fetch last 20 lines from the previous terminated run
	tailLines := int64(20)
	req := clientset.CoreV1().Pods(pod.Namespace).GetLogs(pod.Name, &corev1.PodLogOptions{
		Container: cs.Name,
		Previous:  true,
		TailLines: &tailLines,
	})

	stream, err := req.Stream(context.Background())
	if err != nil {
		return summary
	}
	defer stream.Close()

	raw, err := io.ReadAll(stream)
	if err != nil || len(raw) == 0 {
		return summary
	}

	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")

	// Match against known patterns — first match wins
	for _, pattern := range knownPatterns {
		for i := len(lines) - 1; i >= 0; i-- {
			line := strings.TrimSpace(lines[i])
			if strings.Contains(strings.ToLower(line), strings.ToLower(pattern.keyword)) {
				summary.Pattern = pattern.category
				summary.LastLine = line
				return summary
			}
		}
	}

	// No known pattern matched — use the last non-empty line as context
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line != "" {
			summary.Pattern = "unknown"
			summary.LastLine = line
			return summary
		}
	}

	return summary
}

// checkPodResources checks whether containers have resource requests and limits set.
func checkPodResources(pod corev1.Pod) []Finding {
	var findings []Finding

	for _, c := range pod.Spec.Containers {
		if c.Resources.Requests == nil || len(c.Resources.Requests) == 0 {
			findings = append(findings, Finding{
				Severity:  Warning,
				Namespace: pod.Namespace,
				Kind:      "Pod",
				Name:      pod.Name,
				Message:   fmt.Sprintf("container %q has no resource requests set", c.Name),
			})
		}

		if c.Resources.Limits == nil || len(c.Resources.Limits) == 0 {
			findings = append(findings, Finding{
				Severity:  Warning,
				Namespace: pod.Namespace,
				Kind:      "Pod",
				Name:      pod.Name,
				Message:   fmt.Sprintf("container %q has no resource limits set", c.Name),
			})
		}
	}

	return findings
}

// checkPodProbes checks whether containers have liveness and readiness probes.
func checkPodProbes(pod corev1.Pod) []Finding {
	var findings []Finding

	for _, c := range pod.Spec.Containers {
		if c.LivenessProbe == nil {
			findings = append(findings, Finding{
				Severity:  Info,
				Namespace: pod.Namespace,
				Kind:      "Pod",
				Name:      pod.Name,
				Message:   fmt.Sprintf("container %q has no liveness probe", c.Name),
			})
		}

		if c.ReadinessProbe == nil {
			findings = append(findings, Finding{
				Severity:  Info,
				Namespace: pod.Namespace,
				Kind:      "Pod",
				Name:      pod.Name,
				Message:   fmt.Sprintf("container %q has no readiness probe", c.Name),
			})
		}
	}

	return findings
}
