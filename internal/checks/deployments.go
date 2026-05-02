package checks

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// CheckDeployments runs all deployment-related checks in the given namespace.
func CheckDeployments(clientset *kubernetes.Clientset, namespace string) ([]Finding, error) {
	deployments, err := clientset.AppsV1().Deployments(namespace).List(context.Background(), metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list deployments in namespace %q: %w", namespace, err)
	}

	var findings []Finding

	for _, d := range deployments.Items {
		desired := *d.Spec.Replicas

		// Unavailable replicas: deployment is degraded — fewer pods ready than expected
		if d.Status.UnavailableReplicas > 0 {
			findings = append(findings, Finding{
				Severity:  Critical,
				Namespace: d.Namespace,
				Kind:      "Deployment",
				Name:      d.Name,
				Message: fmt.Sprintf(
					"%d/%d replicas unavailable",
					d.Status.UnavailableReplicas, desired,
				),
			})
		}

		// Single replica: no redundancy — one pod crash = full outage
		if desired == 1 {
			findings = append(findings, Finding{
				Severity:  Warning,
				Namespace: d.Namespace,
				Kind:      "Deployment",
				Name:      d.Name,
				Message:   "only 1 replica configured — no redundancy, a single pod failure causes downtime",
			})
		}

		// Stalled rollout: updated replicas exist but aren't becoming ready
		// This means a new version was deployed but something is wrong with it
		if d.Status.UpdatedReplicas < desired && d.Status.UnavailableReplicas == 0 {
			findings = append(findings, Finding{
				Severity:  Warning,
				Namespace: d.Namespace,
				Kind:      "Deployment",
				Name:      d.Name,
				Message: fmt.Sprintf(
					"rollout may be stalled — only %d/%d updated replicas ready",
					d.Status.UpdatedReplicas, desired,
				),
			})
		}

		// No rolling update strategy: Recreate strategy kills all pods before starting new ones
		// causing guaranteed downtime during every deployment
		if d.Spec.Strategy.RollingUpdate == nil {
			findings = append(findings, Finding{
				Severity:  Info,
				Namespace: d.Namespace,
				Kind:      "Deployment",
				Name:      d.Name,
				Message:   "no RollingUpdate strategy set — Recreate strategy causes downtime during deployments",
			})
		}
	}

	return findings, nil
}
