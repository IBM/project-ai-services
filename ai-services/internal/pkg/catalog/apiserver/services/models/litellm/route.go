package litellm

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
	// route is removed, by the deletion planner, and is empty when the key cannot be named.
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
