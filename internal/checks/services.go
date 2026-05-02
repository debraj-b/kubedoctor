package checks

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"
)

// CheckServices runs all service-related checks in the given namespace.
func CheckServices(clientset *kubernetes.Clientset, namespace string) ([]Finding, error) {
	services, err := clientset.CoreV1().Services(namespace).List(context.Background(), metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list services in namespace %q: %w", namespace, err)
	}

	var findings []Finding

	for _, svc := range services.Items {
		// Skip Kubernetes internal services (e.g. kubernetes.default)
		if svc.Spec.ClusterIP == "None" || len(svc.Spec.Selector) == 0 {
			continue
		}

		// Check if the service selector matches any running pods
		selector := labels.Set(svc.Spec.Selector).AsSelector()
		pods, err := clientset.CoreV1().Pods(svc.Namespace).List(context.Background(), metav1.ListOptions{
			LabelSelector: selector.String(),
		})
		if err != nil {
			continue
		}

		// No pods match this service's selector — traffic will go nowhere
		if len(pods.Items) == 0 {
			findings = append(findings, Finding{
				Severity:  Critical,
				Namespace: svc.Namespace,
				Kind:      "Service",
				Name:      svc.Name,
				Message:   fmt.Sprintf("selector %v matches no pods — traffic will be dropped", svc.Spec.Selector),
			})
			continue
		}

		// Check endpoints — even if pods exist, endpoints must be registered
		endpoints, err := clientset.CoreV1().Endpoints(svc.Namespace).Get(context.Background(), svc.Name, metav1.GetOptions{})
		if err != nil {
			continue
		}

		totalAddresses := 0
		for _, subset := range endpoints.Subsets {
			totalAddresses += len(subset.Addresses)
		}

		// No ready endpoints: pods exist but none are passing readiness checks
		if totalAddresses == 0 {
			findings = append(findings, Finding{
				Severity:  Critical,
				Namespace: svc.Namespace,
				Kind:      "Service",
				Name:      svc.Name,
				Message:   fmt.Sprintf("service has %d matching pods but 0 ready endpoints — pods may be failing readiness probes", len(pods.Items)),
			})
			continue
		}

		// Port mismatch: check that service target ports exist on the matching pods
		for _, svcPort := range svc.Spec.Ports {
			if svcPort.TargetPort.IntVal == 0 {
				continue
			}
			matched := false
			for _, pod := range pods.Items {
				for _, c := range pod.Spec.Containers {
					for _, p := range c.Ports {
						if p.ContainerPort == svcPort.TargetPort.IntVal {
							matched = true
						}
					}
				}
			}
			if !matched {
				findings = append(findings, Finding{
					Severity:  Critical,
					Namespace: svc.Namespace,
					Kind:      "Service",
					Name:      svc.Name,
					Message: fmt.Sprintf(
						"service targetPort %d not found on any matching pod — requests will fail",
						svcPort.TargetPort.IntVal,
					),
				})
			}
		}
	}

	return findings, nil
}
