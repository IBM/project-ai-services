package litellm

import (
	"fmt"

	"github.com/google/uuid"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/proxy"
	runtimeTypes "github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
	workerconstants "github.com/project-ai-services/ai-services/internal/pkg/worker/constants"
)

// PublicKeyAlias returns the alias of the public LiteLLM key of a model, "<model>-<id>", where id is the
// component or connector ID. Generating and revoking the key must use this one alias.
func PublicKeyAlias(model string, id uuid.UUID) string {
	return fmt.Sprintf("%s-%s", model, id)
}

// InternalKeyAlias returns the alias of the internal LiteLLM key an application's services use to
// reach a model, "<model>-<id>-app-<appSlug>": the public key's alias with the application's
// instance slug added, where id is the component or connector ID. An application has one such key
// per model it uses, so revoking it on deletion must use this one alias. It differs from
// PublicKeyAlias, so an internal key never replaces or revokes the public one.
func InternalKeyAlias(model string, id uuid.UUID, appSlug string) string {
	return fmt.Sprintf("%s-app-%s", PublicKeyAlias(model, id), appSlug)
}

// Address is where the services of an application reach the model gateway.
type Address struct {
	Host string
	Port string
	// PathPrefix is the URL prefix of the gateway on that host: empty when the services reach the
	// gateway directly, "/litellm" when they reach it through their worker's Caddy private egress.
	PathPrefix string
}

// GatewayAddress returns where the services of an application on the given runtime reach the
// gateway.
//   - OpenShift: the gateway's service, by its cluster-wide name.
//   - Podman, on the control plane's own worker: the gateway pod, by name.
//   - Podman, on a remote worker: the worker's own Caddy private egress, which forwards
//     /litellm over mTLS to the control plane's private ingress (see workerconstants.LiteLLMPathPrefix).
//     The worker's network cannot resolve the gateway pod, so this is the only way to reach it.
//
// TODO(litellm): a remote OpenShift worker has no Caddy to egress through, so its services still
// get the gateway's cluster-wide service name, which only resolves in the control plane's cluster.
func GatewayAddress(runtime runtimeTypes.RuntimeType, remoteWorker bool) Address {
	switch {
	case runtime == runtimeTypes.RuntimeTypeOpenShift:
		return Address{Host: constants.LiteLLMServiceHost, Port: constants.LiteLLMPort}
	case remoteWorker:
		return Address{
			Host:       workerconstants.WorkerCaddyPodName,
			Port:       proxy.PrivateEgressPort,
			PathPrefix: workerconstants.LiteLLMPathPrefix,
		}
	default:
		return Address{Host: constants.LiteLLMPodName, Port: constants.LiteLLMPort}
	}
}

// TokenizeEndpoint returns the URL of the /tokenize passthrough on the gateway for the model
// deployment id. The passthrough is named after the component ID (the route's Tag), so each
// deployment is tokenized by its own model's tokenizer.
func (g Address) TokenizeEndpoint(id uuid.UUID) string {
	return fmt.Sprintf("http://%s:%s%s/tokenize/%s", g.Host, g.Port, g.PathPrefix, id)
}

// ParamAPIBase is the litellm_params key of the model endpoint. It is the only provider param
// the platform interprets itself (to derive the /tokenize target); every other param is
// forwarded to LiteLLM untouched.
const ParamAPIBase = "api_base"

// Route describes how one model is exposed through the LiteLLM gateway. Both local pods and remote
// connectors are described by a route, so they share the same register/probe/rollback logic.
type Route struct {
	// LiteLLMModelID is the unique ID assigned and returned by LiteLLM /model/new upon registration.
	LiteLLMModelID string
	// Tag is the component or connector ID. It tags the LiteLLM model and suffixes the passthrough.
	Tag string
	// Provider is the LiteLLM provider prefix (e.g. "watsonx", "hosted_vllm"). It prefixes the
	// passthrough ID so a route can be told apart by provider.
	Provider string
	// KeyAlias is the alias of the model's public LiteLLM key, "<model>-<id>". It is only set when a
	// route is removed, by the deletion planner.
	KeyAlias string
	// Mode is the LiteLLM model mode ("chat", "embedding", "rerank", ...), from component metadata.
	Mode string
	// Model is the name consumers use; UpstreamModel is the provider-prefixed name LiteLLM forwards.
	Model         string
	UpstreamModel string
	// APIBase is the endpoint the platform derives for a local pod from the endpoint its Deployer
	// returned. Local requests carry no api_base (their params are template values), and remote
	// models supply theirs in Params, so exactly one of the two is ever set.
	APIBase string
	// Params are forwarded verbatim as litellm_params (api_base, api_key, project_id, ...). For
	// remote models they are the request params validated against the provider's schema.json, so
	// the schema alone decides what reaches LiteLLM. "model" and "tags" are reserved: they are
	// always set from UpstreamModel and Tag.
	Params map[string]any
	// TokenizeURL, when set, registers a /tokenize passthrough to that target.
	TokenizeURL string
}
