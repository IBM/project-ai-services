package kubeconfig

const (
	operatorGroup   = "operators.coreos.com"
	coreGroup       = ""
	rbacGroup       = "rbac.authorization.k8s.io"
	sccGroup        = "security.openshift.io"
	certGroup       = "cert-manager.io"
	nfdGroup        = "nfd.openshift.io"
	dscInitGroup    = "dscinitialization.opendatahub.io"
	dscClusterGroup = "datasciencecluster.opendatahub.io"
	spyreGroup      = "spyre.ibm.com"
)

// permissionCheck defines a permission that needs to be validated.
type permissionCheck struct {
	group     string
	resource  string
	verb      string
	namespace string // empty string means cluster-scoped
}

// getBootstrapSpecificPermissions returns detailed permission checks for bootstrap operations.
// Focuses on core operator installation permissions - if user can install operators,
// they typically have permissions for the custom resources those operators manage.
func getBootstrapSpecificPermissions() []permissionCheck {
	return []permissionCheck{
		// Namespace operations
		{group: coreGroup, resource: "namespaces", verb: "get", namespace: ""},
		{group: coreGroup, resource: "namespaces", verb: "patch", namespace: ""},

		// Node operations
		{group: coreGroup, resource: "nodes", verb: "list", namespace: ""},

		// SCC usage
		{group: sccGroup, resource: "securitycontextconstraints", verb: "use", namespace: ""},

		// OperatorGroup operations
		{group: operatorGroup, resource: "operatorgroups", verb: "get", namespace: ""},
		{group: operatorGroup, resource: "operatorgroups", verb: "patch", namespace: ""},

		// Subscription operations
		{group: operatorGroup, resource: "subscriptions", verb: "get", namespace: ""},
		{group: operatorGroup, resource: "subscriptions", verb: "patch", namespace: ""},

		// ClusterServiceVersion operations
		{group: operatorGroup, resource: "clusterserviceversions", verb: "get", namespace: ""},

		// Cert-manager resources
		{group: certGroup, resource: "issuers", verb: "patch", namespace: ""},
		{group: certGroup, resource: "certificates", verb: "patch", namespace: ""},

		// NFD resources
		{group: nfdGroup, resource: "nodefeaturediscoveries", verb: "patch", namespace: ""},

		// DSC initialization
		{group: dscInitGroup, resource: "dscinitializations", verb: "get", namespace: ""},
		{group: dscInitGroup, resource: "dscinitializations", verb: "patch", namespace: ""},

		// DataScienceCluster
		{group: dscClusterGroup, resource: "datascienceclusters", verb: "get", namespace: ""},
		{group: dscClusterGroup, resource: "datascienceclusters", verb: "patch", namespace: ""},

		// Spyre resources
		{group: spyreGroup, resource: "spyreclusterpolicies", verb: "get", namespace: ""},
	}
}

// Made with Bob
