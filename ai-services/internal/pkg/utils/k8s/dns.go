// Package k8s holds small Kubernetes naming helpers shared across runtimes.
package k8s

// ClusterDomainSuffix is the default in-cluster DNS suffix for Services.
const ClusterDomainSuffix = "svc.cluster.local"

// ServiceDNS returns the in-cluster DNS name of a Service: "<name>.<namespace>.svc.cluster.local".
func ServiceDNS(name, namespace string) string {
	return name + "." + namespace + "." + ClusterDomainSuffix
}
