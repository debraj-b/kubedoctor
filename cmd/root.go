package cmd

import (
	"os"

	"github.com/pterm/pterm"
	"github.com/spf13/cobra"
)

var (
	kubeconfig string
	kubecontext string
)

var rootCmd = &cobra.Command{
	Use:   "kubedoctor",
	Short: "A CLI health checker for Kubernetes clusters",
	Long: `kubedoctor scans your Kubernetes cluster and reports issues
across pods, deployments, services, and namespaces.

It checks for common problems like crash loops, missing resource limits,
services with no endpoints, and more — giving you a clear health report.`,
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		pterm.Error.Println(err)
		os.Exit(1)
	}
}

func init() {
	rootCmd.PersistentFlags().StringVar(&kubeconfig, "kubeconfig", "", "path to kubeconfig file (default: ~/.kube/config)")
	rootCmd.PersistentFlags().StringVar(&kubecontext, "context", "", "kubernetes context to use (default: current context)")
}
