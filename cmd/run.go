package cmd

import (
	"context"
	"fmt"
	"sync"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/debraj-b/kubedoctor/internal/checks"
	"github.com/debraj-b/kubedoctor/internal/kube"
	"github.com/debraj-b/kubedoctor/internal/report"
	"github.com/pterm/pterm"
	"github.com/spf13/cobra"
)

var (
	namespace string
	workers   int
	verbose   bool
)

var runCmd = &cobra.Command{
	Use:   "run",
	Short: "Scan a Kubernetes cluster for issues",
	Long: `Connects to your Kubernetes cluster and runs health checks across
pods, deployments, and services. Reports issues with severity levels
(CRITICAL / WARNING / INFO) and shows log tails for critical pod failures.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runScan()
	},
}

func init() {
	rootCmd.AddCommand(runCmd)
	runCmd.Flags().StringVarP(&namespace, "namespace", "n", "", "namespace to scan (default: all namespaces)")
	runCmd.Flags().IntVarP(&workers, "workers", "w", 10, "number of concurrent namespace workers")
	runCmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "show more log lines for critical failures")
}

func runScan() error {
	// Build the kubernetes client from kubeconfig
	client, err := kube.NewClient(kubeconfig, kubecontext)
	if err != nil {
		return fmt.Errorf("could not connect to cluster: %w", err)
	}

	start := time.Now()

	// Determine which namespaces to scan
	var namespaces []string
	if namespace != "" {
		namespaces = []string{namespace}
	} else {
		nsList, err := client.CoreV1().Namespaces().List(context.Background(), metav1.ListOptions{})
		if err != nil {
			return fmt.Errorf("could not list namespaces: %w", err)
		}
		for _, ns := range nsList.Items {
			namespaces = append(namespaces, ns.Name)
		}
	}

	pterm.Info.Printfln("Scanning %d namespace(s) with %d workers...", len(namespaces), workers)

	// Worker pool setup:
	// - namespaceCh feeds namespace names to workers
	// - resultCh collects findings from all workers
	// - wg tracks when all workers are done
	namespaceCh := make(chan string, len(namespaces))
	resultCh := make(chan []checks.Finding, len(namespaces))
	var wg sync.WaitGroup

	// Feed all namespaces into the channel
	for _, ns := range namespaces {
		namespaceCh <- ns
	}
	close(namespaceCh) // closing tells workers: no more namespaces coming

	// Spin up N workers — each picks a namespace from the channel and scans it
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for ns := range namespaceCh {
				var nsFindings []checks.Finding

				// Run all three checks for this namespace
				if podFindings, err := checks.CheckPods(client, ns); err == nil {
					nsFindings = append(nsFindings, podFindings...)
				}
				if depFindings, err := checks.CheckDeployments(client, ns); err == nil {
					nsFindings = append(nsFindings, depFindings...)
				}
				if svcFindings, err := checks.CheckServices(client, ns); err == nil {
					nsFindings = append(nsFindings, svcFindings...)
				}

				resultCh <- nsFindings
			}
		}()
	}

	// Close resultCh once all workers finish — signals the aggregator below
	go func() {
		wg.Wait()
		close(resultCh)
	}()

	// Collect all findings from workers as they finish
	var allFindings []checks.Finding
	for findings := range resultCh {
		allFindings = append(allFindings, findings...)
	}

	duration := time.Since(start)

	// Gather scan metadata for the report header
	podCount, depCount, svcCount := countResources(allFindings)
	summary := report.Summary{
		ClusterName: currentClusterName(),
		Namespaces:  len(namespaces),
		Pods:        podCount,
		Deployments: depCount,
		Services:    svcCount,
		Duration:    duration,
	}

	report.Print(allFindings, summary)
	return nil
}

// countResources extracts approximate resource counts from findings.
// Not exact (only counts resources that had findings) but good enough for the header.
func countResources(findings []checks.Finding) (pods, deployments, services int) {
	seen := map[string]bool{}
	for _, f := range findings {
		key := f.Kind + "/" + f.Namespace + "/" + f.Name
		if seen[key] {
			continue
		}
		seen[key] = true
		switch f.Kind {
		case "Pod":
			pods++
		case "Deployment":
			deployments++
		case "Service":
			services++
		}
	}
	return
}

// currentClusterName returns the active cluster name from kubeconfig context.
func currentClusterName() string {
	if kubecontext != "" {
		return kubecontext
	}
	return "current-context"
}
