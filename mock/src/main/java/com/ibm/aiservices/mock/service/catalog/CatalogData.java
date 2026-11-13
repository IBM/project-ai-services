package com.ibm.aiservices.mock.service.catalog;

import java.util.ArrayList;
import java.util.Arrays;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;

import com.ibm.aiservices.mock.util.Json;

/**
 * Static catalog content mirroring {@code ai-services/assets}: the RAG architecture, its services, component providers, models and connectors.
 */
final class CatalogData {

    /** Accelerator resource name used by Spyre profiles. */
    static final String SPYRE = "ibm.com/spyre_pf";

    private static final long GIB = 1024L * 1024L * 1024L;

    private CatalogData() {
    }

    /**
     * A component provider such as {@code vllm-spyre} for {@code llm}.
     */
    static final class Provider {

        private final String m_componentType;
        private final String m_id;
        private final String m_name;
        private final String m_description;
        private final String m_deploymentType;
        private final boolean m_isDefault;
        private final Map<String, Object> m_resources;

        /**
         * Creates a provider.
         *
         * @param _componentType the component type it serves
         * @param _id the provider ID
         * @param _name the display name
         * @param _description the description
         * @param _deploymentType {@code local} or {@code remote}
         * @param _isDefault whether it is the default for its component type
         * @param _resources the resources it requests, or {@code null} for remote providers
         */
        Provider(final String _componentType, final String _id, final String _name, final String _description, final String _deploymentType, final boolean _isDefault, final Map<String, Object> _resources) {
            m_componentType = _componentType;
            m_id = _id;
            m_name = _name;
            m_description = _description;
            m_deploymentType = _deploymentType;
            m_isDefault = _isDefault;
            m_resources = _resources;
        }

        String getComponentType() {
            return m_componentType;
        }

        String getId() {
            return m_id;
        }

        String getName() {
            return m_name;
        }

        String getDeploymentType() {
            return m_deploymentType;
        }

        /**
         * Returns the provider as listed in deploy options.
         *
         * @return the provider object
         */
        Map<String, Object> toDeployOption() {
            final Map<String, Object> option = Json.obj("id", m_id, "name", m_name, "description", m_description, "version", "1.0.0", "default", Boolean.valueOf(m_isDefault), "schema", "/api/v1/components/" + m_componentType + "/providers/" + m_id + "/params");
            if (null != m_resources) {
                option.put("resources", m_resources);
            }
            return option;
        }
    }

    /**
     * A model entry from a provider's {@code models.yaml}.
     */
    static final class Model {

        private final String m_id;
        private final String m_name;
        private final String m_componentType;
        private final String m_certifiedBy;
        private final String m_license;
        private final String m_description;
        private final List<String> m_providerIds;

        /**
         * Creates a model entry.
         *
         * @param _id the model ID
         * @param _name the display name
         * @param _componentType the component type
         * @param _certifiedBy the certifying organization
         * @param _license the license
         * @param _description the description
         * @param _providerIds the IDs of providers that can serve it
         */
        Model(final String _id, final String _name, final String _componentType, final String _certifiedBy, final String _license, final String _description, final String... _providerIds) {
            m_id = _id;
            m_name = _name;
            m_componentType = _componentType;
            m_certifiedBy = _certifiedBy;
            m_license = _license;
            m_description = _description;
            m_providerIds = Arrays.asList(_providerIds);
        }

        String getId() {
            return m_id;
        }

        String getName() {
            return m_name;
        }

        String getComponentType() {
            return m_componentType;
        }

        List<String> getProviderIds() {
            return m_providerIds;
        }

        String getLicense() {
            return m_license;
        }

        String getCertifiedBy() {
            return m_certifiedBy;
        }

        String getDescription() {
            return m_description;
        }
    }

    /**
     * A catalog service entry.
     */
    static final class Service {

        private final String m_id;
        private final String m_name;
        private final String m_description;
        private final boolean m_isStandalone;
        private final boolean m_isAcceptsDatasource;
        private final List<String> m_architectures;
        private final List<String> m_dependencies;

        /**
         * Creates a service entry.
         *
         * @param _id the service ID
         * @param _name the display name
         * @param _description the description
         * @param _isStandalone whether it can be deployed on its own
         * @param _isAcceptsDatasource whether datasources can be connected to it
         * @param _architectures the architectures that include it
         * @param _dependencies the component types it depends on
         */
        Service(final String _id, final String _name, final String _description, final boolean _isStandalone, final boolean _isAcceptsDatasource, final List<String> _architectures, final String... _dependencies) {
            m_id = _id;
            m_name = _name;
            m_description = _description;
            m_isStandalone = _isStandalone;
            m_isAcceptsDatasource = _isAcceptsDatasource;
            m_architectures = _architectures;
            m_dependencies = Arrays.asList(_dependencies);
        }

        String getId() {
            return m_id;
        }

        String getName() {
            return m_name;
        }

        List<String> getDependencies() {
            return m_dependencies;
        }

        boolean isAcceptsDatasource() {
            return m_isAcceptsDatasource;
        }

        List<String> getArchitectures() {
            return m_architectures;
        }

        /**
         * Returns the summary shown in {@code GET /services}.
         *
         * @return the summary object
         */
        Map<String, Object> toSummary() {
            final Map<String, Object> summary = Json.obj("id", m_id, "name", m_name, "description", m_description, "certified_by", "IBM", "architectures", m_architectures, "standalone", Boolean.valueOf(m_isStandalone));
            if (!m_dependencies.isEmpty()) {
                summary.put("dependencies", dependencyList());
            }
            return summary;
        }

        /**
         * Returns the full service entry shown in {@code GET /services/:id}.
         *
         * @return the service object
         */
        Map<String, Object> toDetail() {
            final Map<String, Object> detail = Json.obj("about", Json.list(Json.obj("title", "Overview", "values", Json.list(m_description))), "id", m_id, "name", m_name, "description", m_description, "type", "service", "certified_by", "IBM", "architectures", m_architectures);
            if (!m_dependencies.isEmpty()) {
                detail.put("dependencies", dependencyList());
            }
            if (m_isStandalone) {
                detail.put("standalone", Boolean.TRUE);
            }
            if (m_isAcceptsDatasource) {
                detail.put("accepts_datasource", Boolean.TRUE);
            }
            return detail;
        }

        /**
         * Returns the dependencies as {@code {"id": ...}} objects.
         *
         * @return the dependency list
         */
        private List<Object> dependencyList() {
            final List<Object> dependencies = new ArrayList<Object>();
            for (final String dependency : m_dependencies) {
                dependencies.add(Json.obj("id", dependency));
            }
            return dependencies;
        }
    }

    private static final Map<String, String[]> COMPONENT_TYPES = new LinkedHashMap<String, String[]>();
    private static final List<Provider> PROVIDERS = new ArrayList<Provider>();
    private static final List<Model> MODELS = new ArrayList<Model>();
    private static final List<Service> SERVICES = new ArrayList<Service>();

    static {
        COMPONENT_TYPES.put("llm", new String[] {"Large Language Model", "LLM"});
        COMPONENT_TYPES.put("embedding", new String[] {"Embedding Model", "Embedding"});
        COMPONENT_TYPES.put("reranker", new String[] {"Reranker Model", "Reranker"});
        COMPONENT_TYPES.put("vector_store", new String[] {"Vector Store", "Vector Store"});

        PROVIDERS.add(new Provider("llm", "vllm-spyre", "Red Hat AI Inference with Spyre", "Deploy instruct models on vLLM inference engine with Spyre acceleration", "local", true, Json.obj("cpu", Integer.valueOf(8), "memory", Long.valueOf(200L * GIB), "accelerators", Json.obj(SPYRE, Integer.valueOf(4)))));
        PROVIDERS.add(new Provider("llm", "vllm-cpu", "AI Inference on CPU", "Deploy instruct models on vLLM inference engine (CPU-only)", "local", false, Json.obj("cpu", Integer.valueOf(16), "memory", Long.valueOf(64L * GIB))));
        PROVIDERS.add(new Provider("llm", "watsonx", "IBM watsonx.ai on IBM Cloud", "Deploy llm models using watsonx", "remote", false, null));
        PROVIDERS.add(new Provider("embedding", "vllm-cpu", "AI Inference on CPU", "Deploy embedding models on vLLM inference engine (CPU-only)", "local", true, Json.obj("cpu", Integer.valueOf(4), "memory", Long.valueOf(8L * GIB))));
        PROVIDERS.add(new Provider("reranker", "vllm-spyre", "Red Hat AI Inference with Spyre", "Deploy reranker models on vLLM inference engine", "local", true, Json.obj("cpu", Integer.valueOf(4), "memory", Long.valueOf(32L * GIB), "accelerators", Json.obj(SPYRE, Integer.valueOf(1)))));
        PROVIDERS.add(new Provider("reranker", "vllm-cpu", "AI Inference on CPU", "Deploy reranker models on vLLM inference engine (CPU-only)", "local", false, Json.obj("cpu", Integer.valueOf(4), "memory", Long.valueOf(8L * GIB))));
        PROVIDERS.add(new Provider("vector_store", "opensearch", "OpenSearch", "Distributed search and analytics engine for vector storage", "local", true, Json.obj("cpu", Integer.valueOf(2), "memory", Long.valueOf(4L * GIB), "storage", Long.valueOf(10L * GIB))));

        MODELS.add(new Model("BAAI/bge-reranker-v2-m3", "BGE Reranker v2 M3", "reranker", "BAAI", "MIT", "A multilingual, cross-encoder based reranker model used to improve retrieval quality by scoring and ranking candidate documents.", "vllm-spyre", "vllm-cpu"));
        MODELS.add(new Model("ibm-granite/granite-3.3-8b-instruct", "Granite 3.3 8B Instruct", "llm", "IBM", "Apache-2.0", "An 8b-parameter instruction-tuned LLM used for natural language understanding and generation tasks.", "vllm-spyre", "vllm-cpu"));
        MODELS.add(new Model("ibm-granite/granite-embedding-278m-multilingual", "Granite Embedding 278M Multilingual", "embedding", "IBM", "Apache-2.0", "Multilingual embedding model for cross-lingual semantic search and document retrieval.", "vllm-cpu"));
        MODELS.add(new Model("ibm/granite-4-h-small", "Granite 4 H Small", "llm", "IBM", "Apache-2.0", "A 32B parameter long-context instruct model finetuned from Granite-4.0-H-Small-Base.", "watsonx"));

        final List<String> rag = Arrays.asList("rag");
        final List<String> none = new ArrayList<String>();
        SERVICES.add(new Service("chat", "Question and answer", "Provides accurate, context-aware responses to user queries. Enables automated customer support, knowledge base interactions, and conversational assistance across various domains.", false, false, rag, "vector_store", "embedding", "llm"));
        SERVICES.add(new Service("digitize", "Digitize documents", "Converts physical and scanned documents into searchable, editable digital formats; enables efficient document management, data extraction, and workflow automation at scale.", true, true, rag, "vector_store", "embedding", "llm"));
        SERVICES.add(new Service("extract", "Extract information", "Takes relevant data from unstructured text and populates a user-supplied JSON schema, enabling robust information retrieval from documents such as emails, invoices, and contracts.", true, false, none, "llm"));
        SERVICES.add(new Service("similarity", "Find similar items",
                "Identifies and retrieves items similar to a reference item based on content, attributes, or patterns; enables efficient recommendation, duplicate detection, and content discovery at scale.", false, false, rag, "vector_store", "embedding", "reranker"));
        SERVICES.add(new Service("summarize", "Summarize", "Condenses long-form content into concise, accurate summaries while preserving key information; enables efficient document analysis and information extraction at scale.", true, false, none, "llm"));
        SERVICES.add(new Service("translate", "Translate", "Translates text and long-form documents between languages using an LLM; supports asynchronous document jobs and synchronous text translation with token-aware chunking.", true, false, none, "llm"));
    }

    static List<Service> services() {
        return SERVICES;
    }

    /**
     * Finds a catalog service.
     *
     * @param _id the service ID
     * @return the service, or {@code null}
     */
    static Service service(final String _id) {
        for (final Service service : SERVICES) {
            if (service.getId().equals(_id)) {
                return service;
            }
        }
        return null;
    }

    static List<Model> models() {
        return MODELS;
    }

    /**
     * Finds a catalog model.
     *
     * @param _id the model ID
     * @return the model, or {@code null}
     */
    static Model model(final String _id) {
        for (final Model model : MODELS) {
            if (model.getId().equals(_id)) {
                return model;
            }
        }
        return null;
    }

    static boolean isComponentType(final String _type) {
        return COMPONENT_TYPES.containsKey(_type);
    }

    static Map<String, Object> componentType(final String _type) {
        final String[] names = COMPONENT_TYPES.get(_type);
        return Json.obj("id", _type, "name", names[0], "short_name", names[1]);
    }

    /**
     * Lists the providers of a component type.
     *
     * @param _componentType the component type
     * @return the providers in catalog order
     */
    static List<Provider> providers(final String _componentType) {
        final List<Provider> providers = new ArrayList<Provider>();
        for (final Provider provider : PROVIDERS) {
            if (provider.getComponentType().equals(_componentType)) {
                providers.add(provider);
            }
        }
        return providers;
    }

    /**
     * Finds a provider of a component type.
     *
     * @param _componentType the component type
     * @param _id the provider ID
     * @return the provider, or {@code null}
     */
    static Provider provider(final String _componentType, final String _id) {
        for (final Provider provider : providers(_componentType)) {
            if (provider.getId().equals(_id)) {
                return provider;
            }
        }
        return null;
    }

    /**
     * Finds a provider by ID in any component type, preferring the given type.
     *
     * @param _providerId the provider ID
     * @param _componentType the preferred component type, or {@code null}
     * @return the provider, or {@code null}
     */
    static Provider anyProvider(final String _providerId, final String _componentType) {
        if (null != _componentType && null != provider(_componentType, _providerId)) {
            return provider(_componentType, _providerId);
        }
        for (final Provider provider : PROVIDERS) {
            if (provider.getId().equals(_providerId)) {
                return provider;
            }
        }
        return null;
    }

    /**
     * Returns the RAG architecture in full.
     *
     * @return the architecture object
     */
    static Map<String, Object> architecture() {
        final Map<String, Object> about = Json.obj("title", "Services", "values", Json.list("Digitize Documents", "Find Similar Items", "Question and Answer"));
        return Json.obj("about", Json.list(about), "id", "rag", "name", "Digital assistants", "description", architectureDescription(), "version", "1.0.0", "type", "architecture",
                "certified_by", "IBM", "runtimes", Json.list("podman"), "global_components", Json.list(Json.obj("type", "vector_store"), Json.obj("type", "embedding")),
                "services", Json.list(Json.obj("id", "chat", "version", ">=1.0.0"), Json.obj("id", "digitize", "version", ">=1.0.0"), Json.obj("id", "similarity", "version",
                ">=1.0.0")), "links", Json.obj("documentation", "https://www.ibm.com/docs/en/aiservices"));
    }

    static String architectureDescription() {
        return "Production-ready tools that help users complete tasks and access information through conversation or commands. Assistants integrate multiple services for complex use cases and support retrieval-augmented generation (RAG).";
    }

    static List<String> architectureServices() {
        return Arrays.asList("chat", "digitize", "similarity");
    }

    /**
     * Returns the deploy options of one component type: every provider that can satisfy it.
     *
     * @param _type the component type
     * @return the component option
     */
    static Map<String, Object> componentOption(final String _type) {
        final List<Object> providers = new ArrayList<Object>();
        for (final Provider provider : providers(_type)) {
            providers.add(provider.toDeployOption());
        }
        return Json.obj("type", _type, "name", COMPONENT_TYPES.get(_type)[0], "providers", providers);
    }

    /**
     * Returns the deploy options of a service.
     *
     * @param _service the service
     * @return the service deploy options
     */
    static Map<String, Object> serviceDeployOptions(final Service _service) {
        final List<Object> components = new ArrayList<Object>();
        for (final String dependency : _service.getDependencies()) {
            components.add(componentOption(dependency));
        }
        return Json.obj("id", _service.getId(), "name", _service.getName(), "version", "1.0.0", "schema", "/api/v1/services/" + _service.getId() + "/params", "accepts_datasource",
                Boolean.valueOf(_service.isAcceptsDatasource()), "resources", Json.obj("cpu", Integer.valueOf(1), "memory", Long.valueOf(2L * GIB)), "components", components);
    }

    /**
     * Returns the container images a service or architecture deploys.
     *
     * @param _serviceIds the services
     * @return the image references
     */
    static List<String> images(final List<String> _serviceIds) {
        final List<String> images = new ArrayList<String>();
        for (final String serviceId : _serviceIds) {
            images.add("icr.io/ai-services-private/" + serviceId + ":1.0.0");
        }
        images.add("icr.io/ai-services-private/vllm-spyre-rhel:0.10.1");
        images.add("docker.io/opensearchproject/opensearch:2.19.0");
        return images;
    }

    /**
     * Returns the profiles a local provider offers for a model.
     *
     * @param _provider the provider
     * @return the profiles
     */
    static List<Object> profiles(final Provider _provider) {
        if ("vllm-spyre".equals(_provider.getId())) {
            return Json.list(Json.obj("id", "standard", "name", "Standard", "description", "4 cards, 200 GiB RAM, 32k context", "default", Boolean.TRUE, "resources",
                    Json.obj("memory", Long.valueOf(200L * GIB), "accelerators", Json.obj(SPYRE, Integer.valueOf(4)))), Json.obj("id", "balanced", "name", "Balanced",
                    "description", "2 cards, 150 GiB RAM, 8k context", "default", Boolean.FALSE, "resources", Json.obj("memory", Long.valueOf(150L * GIB), "accelerators",
                    Json.obj(SPYRE, Integer.valueOf(2)))));
        }
        return Json.list(Json.obj("id", "default", "name", "Default", "description", "CPU-only inference", "default", Boolean.TRUE, "resources", Json.obj("cpu", Integer.valueOf(8), "memory", Long.valueOf(32L * GIB))));
    }

    /**
     * Returns the JSON Schema describing a component provider's parameters.
     *
     * @param _provider the provider
     * @return the schema
     */
    static Map<String, Object> providerParamsSchema(final Provider _provider) {
        final Map<String, Object> properties = new LinkedHashMap<String, Object>();
        if ("remote".equals(_provider.getDeploymentType())) {
            properties.put("url", Json.obj("type", "string", "title", "Endpoint URL", "format", "uri"));
            properties.put("project_id", Json.obj("type", "string", "title", "Project ID"));
            properties.put("api_key", Json.obj("type", "string", "title", "API key", "writeOnly", Boolean.TRUE));
            return Json.obj("$schema", "http://json-schema.org/draft-07/schema#", "type", "object", "properties", properties, "required", Json.list("url", "project_id", "api_key"));
        }
        if ("vector_store".equals(_provider.getComponentType())) {
            properties.put("storage_size", Json.obj("type", "string", "title", "Storage size", "default", "10Gi"));
            return Json.obj("$schema", "http://json-schema.org/draft-07/schema#", "type", "object", "properties", properties);
        }
        final List<Object> modelIds = new ArrayList<Object>();
        for (final Model model : MODELS) {
            if (model.getComponentType().equals(_provider.getComponentType()) && model.getProviderIds().contains(_provider.getId())) {
                modelIds.add(model.getId());
            }
        }
        properties.put("model", Json.obj("type", "string", "title", "Model", "enum", modelIds, "default", modelIds.isEmpty() ? null : modelIds.get(0), "x-data-id", "models"));
        properties.put("max_model_len", Json.obj("type", "integer", "title", "Maximum context length", "default", Integer.valueOf(32768)));
        return Json.obj("$schema", "http://json-schema.org/draft-07/schema#", "type", "object", "properties", properties, "required", Json.list("model"));
    }

    /**
     * Returns the JSON Schema describing a service's parameters.
     *
     * @param _service the service
     * @return the schema
     */
    static Map<String, Object> serviceParamsSchema(final Service _service) {
        final Map<String, Object> properties = new LinkedHashMap<String, Object>();
        properties.put("replicas", Json.obj("type", "integer", "title", "Replicas", "default", Integer.valueOf(1), "minimum", Integer.valueOf(1)));
        properties.put("log_level", Json.obj("type", "string", "title", "Log level", "enum", Json.list("DEBUG", "INFO", "WARNING", "ERROR"), "default", "INFO"));
        if ("chat".equals(_service.getId())) {
            properties.put("max_tokens", Json.obj("type", "integer", "title", "Maximum response tokens", "default", Integer.valueOf(512)));
        }
        return Json.obj("$schema", "http://json-schema.org/draft-07/schema#", "type", "object", "title", _service.getName() + " parameters", "properties", properties);
    }

    /**
     * Returns the connector providers, keyed by connector type.
     *
     * @return a list of {@code {type, typeName, providerId, providerName, description}} rows
     */
    static List<String[]> connectors() {
        return Arrays.asList(
                new String[] {"datasource", "Data sources", "object_storage", "Object Storage", "S3-compatible object storage bucket, such as IBM Cloud Object Storage or MinIO."},
                new String[] {"datasource", "Data sources", "file_system", "File System", "Remote file system reachable over SFTP."},
                new String[] {"models", "Model providers", "vllm", "vLLM", "Any OpenAI-compatible vLLM endpoint."},
                new String[] {"models", "Model providers", "watsonx", "IBM watsonx.ai", "Models hosted on IBM watsonx.ai."});
    }

    /**
     * Returns the JSON Schema describing a connector provider's parameters.
     *
     * @param _providerId the provider ID
     * @return the schema
     */
    static Map<String, Object> connectorParamsSchema(final String _providerId) {
        final Map<String, Object> properties = new LinkedHashMap<String, Object>();
        final List<Object> required;
        if ("object_storage".equals(_providerId)) {
            properties.put("endpoint", Json.obj("type", "string", "title", "Endpoint", "format", "uri", "x-section", "Connection"));
            properties.put("bucket", Json.obj("type", "string", "title", "Bucket", "x-section", "Connection"));
            properties.put("access_key_id", Json.obj("type", "string", "title", "Access key ID", "x-section", "Authentication"));
            properties.put("secret_access_key", Json.obj("type", "string", "title", "Secret access key", "writeOnly", Boolean.TRUE, "x-section", "Authentication"));
            required = Json.list("endpoint", "bucket", "access_key_id", "secret_access_key");
        } else if ("file_system".equals(_providerId)) {
            properties.put("host", Json.obj("type", "string", "title", "Host", "x-section", "Connection"));
            properties.put("port", Json.obj("type", "integer", "title", "Port", "default", Integer.valueOf(22), "x-section", "Connection"));
            properties.put("remote_path", Json.obj("type", "string", "title", "Remote path", "default", "/", "x-section", "Connection"));
            properties.put("username", Json.obj("type", "string", "title", "Username", "x-section", "Authentication"));
            properties.put("private_key", Json.obj("type", "string", "title", "Private key (PEM)", "writeOnly", Boolean.TRUE, "x-section", "Authentication"));
            required = Json.list("host", "username", "private_key");
        } else {
            properties.put("url", Json.obj("type", "string", "title", "Endpoint URL", "format", "uri", "x-section", "Connection"));
            properties.put("api_key", Json.obj("type", "string", "title", "API key", "writeOnly", Boolean.TRUE, "x-section", "Authentication"));
            required = Json.list("url");
        }
        return Json.obj("$schema", "http://json-schema.org/draft-07/schema#", "type", "object", "properties", properties, "required", required);
    }

    /**
     * Returns the install step files of a service.
     *
     * @param _service the service
     * @param _runtime the runtime
     * @return a map of file name to content
     */
    static Map<String, Object> steps(final Service _service, final String _runtime) {
        return Json.obj("info.md", "# " + _service.getName() + "\n\n" + "Deploys the " + _service.getName() + " service on " + _runtime + ".\n", "next.md",
                "## Next steps\n\nOpen the application in the AI Services console to start using " + _service.getName() + ".\n", "vars_file.yaml",
                "service: " + _service.getId() + "\nruntime: " + _runtime + "\n");
    }
}
