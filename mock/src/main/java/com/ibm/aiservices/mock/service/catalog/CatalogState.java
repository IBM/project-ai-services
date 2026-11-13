package com.ibm.aiservices.mock.service.catalog;

import java.util.ArrayList;
import java.util.Iterator;
import java.util.LinkedHashMap;
import java.util.LinkedHashSet;
import java.util.List;
import java.util.Map;
import java.util.Set;

import com.ibm.aiservices.mock.MockConfig;
import com.ibm.aiservices.mock.service.JobPhase;
import com.ibm.aiservices.mock.util.Ids;
import com.ibm.aiservices.mock.util.Json;
import com.ibm.aiservices.mock.util.Times;

/**
 * In-memory state of the mock catalog server. All access is guarded by the instance monitor.
 * <p>
 * Deployments (applications and local models) progress from {@code Downloading} through {@code Deploying} to {@code Running} over the configured job duration; deletions complete after the same duration.
 */
final class CatalogState {

    /** Name of the built-in worker representing the control-plane host. */
    static final String LOCAL_WORKER = "Local";

    private final MockConfig m_config;
    private final Map<String, Worker> m_workers = new LinkedHashMap<String, Worker>();
    private final Map<String, Application> m_applications = new LinkedHashMap<String, Application>();
    private final Map<String, Datasource> m_datasources = new LinkedHashMap<String, Datasource>();
    private final Map<String, DeployedModel> m_models = new LinkedHashMap<String, DeployedModel>();
    private final Map<String, Bundle> m_bundles = new LinkedHashMap<String, Bundle>();

    /**
     * A worker host that runs applications.
     */
    static final class Worker {

        private final String m_id = Ids.uuid();
        private final String m_name;
        private final String m_runtimeType;
        private final long m_registeredAt = System.currentTimeMillis();
        private final boolean m_isBootstrapped;

        Worker(final String _name, final String _runtimeType, final boolean _isBootstrapped) {
            m_name = _name;
            m_runtimeType = _runtimeType;
            m_isBootstrapped = _isBootstrapped;
        }

        String getId() {
            return m_id;
        }

        String getName() {
            return m_name;
        }

        String getRuntimeType() {
            return m_runtimeType;
        }

        long getRegisteredAt() {
            return m_registeredAt;
        }

        boolean isBootstrapped() {
            return m_isBootstrapped;
        }

        Map<String, Object> toRef() {
            return Json.obj("id", m_id, "name", m_name, "runtime_type", m_runtimeType);
        }
    }

    /**
     * A component of a deployed service, e.g. the LLM behind chat.
     */
    static final class Component {

        private final String m_id = Ids.uuid();
        private final String m_type;
        private final String m_providerId;
        private final String m_providerName;

        Component(final String _type, final String _providerId, final String _providerName) {
            m_type = _type;
            m_providerId = _providerId;
            m_providerName = _providerName;
        }

        String getId() {
            return m_id;
        }

        String getType() {
            return m_type;
        }

        String getProviderId() {
            return m_providerId;
        }

        String getProviderName() {
            return m_providerName;
        }
    }

    /**
     * A service deployed as part of an application.
     */
    static final class AppService {

        private final String m_id = Ids.uuid();
        private final String m_catalogId;
        private final String m_type;
        private final String m_version;
        private final List<Component> m_components = new ArrayList<Component>();

        AppService(final String _catalogId, final String _type, final String _version) {
            m_catalogId = _catalogId;
            m_type = _type;
            m_version = _version;
        }

        String getId() {
            return m_id;
        }

        String getCatalogId() {
            return m_catalogId;
        }

        String getType() {
            return m_type;
        }

        String getVersion() {
            return m_version;
        }

        List<Component> getComponents() {
            return m_components;
        }
    }

    /**
     * A deployed application.
     */
    static final class Application {

        private final String m_id = Ids.uuid();
        private final String m_catalogId;
        private final String m_deploymentType;
        private final String m_type;
        private final String m_version;
        private final String m_owner;
        private final Worker m_worker;
        private final long m_createdAt = System.currentTimeMillis();
        private final boolean m_isFailing;
        private final List<AppService> m_services = new ArrayList<AppService>();
        private final Map<String, Long> m_datasources = new LinkedHashMap<String, Long>();
        private String m_name;
        private long m_updatedAt = m_createdAt;
        private long m_deleteRequestedAt = -1L;

        /**
         * Creates an application record.
         *
         * @param _name the application name
         * @param _catalogId the architecture or service ID it was created from
         * @param _deploymentType {@code architectures} or {@code services}
         * @param _type the display name of the catalog entry
         * @param _version the catalog version
         * @param _owner the owning user
         * @param _worker the worker it runs on
         * @param _isFailing whether the simulated deployment should fail
         */
        Application(final String _name, final String _catalogId, final String _deploymentType, final String _type, final String _version, final String _owner, final Worker _worker, final boolean _isFailing) {
            m_name = _name;
            m_catalogId = _catalogId;
            m_deploymentType = _deploymentType;
            m_type = _type;
            m_version = _version;
            m_owner = _owner;
            m_worker = _worker;
            m_isFailing = _isFailing;
        }

        String getId() {
            return m_id;
        }

        String getName() {
            return m_name;
        }

        void rename(final String _name) {
            m_name = _name;
            m_updatedAt = System.currentTimeMillis();
        }

        String getCatalogId() {
            return m_catalogId;
        }

        String getDeploymentType() {
            return m_deploymentType;
        }

        String getType() {
            return m_type;
        }

        String getVersion() {
            return m_version;
        }

        String getOwner() {
            return m_owner;
        }

        Worker getWorker() {
            return m_worker;
        }

        long getCreatedAt() {
            return m_createdAt;
        }

        long getUpdatedAt() {
            return m_updatedAt;
        }

        boolean isFailing() {
            return m_isFailing;
        }

        List<AppService> getServices() {
            return m_services;
        }

        /**
         * Returns connected datasource IDs mapped to their connection time.
         *
         * @return the mutable connection map
         */
        Map<String, Long> getDatasources() {
            return m_datasources;
        }

        boolean isDeleting() {
            return 0 <= m_deleteRequestedAt;
        }

        void requestDelete() {
            m_deleteRequestedAt = System.currentTimeMillis();
            m_updatedAt = m_deleteRequestedAt;
        }

        long getDeleteRequestedAt() {
            return m_deleteRequestedAt;
        }

        /**
         * Returns whether any service of the application accepts datasources.
         *
         * @return {@code true} if datasources can be connected
         */
        boolean acceptsDatasources() {
            for (final AppService service : m_services) {
                final CatalogData.Service catalogService = CatalogData.service(service.getCatalogId());
                if (null != catalogService && catalogService.isAcceptsDatasource()) {
                    return true;
                }
            }
            return false;
        }
    }

    /**
     * A registered datasource.
     */
    static final class Datasource {

        private final String m_id = Ids.uuid();
        private final String m_name;
        private final String m_providerId;
        private final String m_owner;
        private final long m_createdAt = System.currentTimeMillis();
        private final Map<String, Object> m_params;
        private long m_updatedAt = m_createdAt;

        Datasource(final String _name, final String _providerId, final String _owner, final Map<String, Object> _params) {
            m_name = _name;
            m_providerId = _providerId;
            m_owner = _owner;
            m_params = _params;
        }

        String getId() {
            return m_id;
        }

        String getName() {
            return m_name;
        }

        String getProviderId() {
            return m_providerId;
        }

        String getOwner() {
            return m_owner;
        }

        long getCreatedAt() {
            return m_createdAt;
        }

        long getUpdatedAt() {
            return m_updatedAt;
        }

        Map<String, Object> getParams() {
            return m_params;
        }

        void touch() {
            m_updatedAt = System.currentTimeMillis();
        }
    }

    /**
     * A model deployed locally or registered as a remote endpoint.
     */
    static final class DeployedModel {

        private final String m_id = Ids.uuid();
        private final String m_deploymentType;
        private final String m_componentType;
        private final String m_name;
        private final CatalogData.Provider m_provider;
        private final String m_model;
        private final String m_profile;
        private final Worker m_worker;
        private final Map<String, Object> m_params;
        private final String m_owner;
        private final long m_createdAt = System.currentTimeMillis();
        private final boolean m_isFailing;
        private long m_deleteRequestedAt = -1L;

        /**
         * Creates a deployed model record.
         *
         * @param _deploymentType {@code local} or {@code remote}
         * @param _componentType the component type
         * @param _name the model name
         * @param _provider the serving provider
         * @param _model the catalog model ID
         * @param _profile the resource profile, or {@code null}
         * @param _worker the worker for local models, or {@code null}
         * @param _params the provider parameters
         * @param _owner the owning user
         * @param _isFailing whether the simulated deployment should fail
         */
        DeployedModel(final String _deploymentType, final String _componentType, final String _name, final CatalogData.Provider _provider, final String _model, final String _profile, final Worker _worker, final Map<String, Object> _params, final String _owner, final boolean _isFailing) {
            m_deploymentType = _deploymentType;
            m_componentType = _componentType;
            m_name = _name;
            m_provider = _provider;
            m_model = _model;
            m_profile = _profile;
            m_worker = _worker;
            m_params = _params;
            m_owner = _owner;
            m_isFailing = _isFailing;
        }

        String getId() {
            return m_id;
        }

        String getDeploymentType() {
            return m_deploymentType;
        }

        String getComponentType() {
            return m_componentType;
        }

        String getName() {
            return m_name;
        }

        CatalogData.Provider getProvider() {
            return m_provider;
        }

        String getModel() {
            return m_model;
        }

        String getProfile() {
            return m_profile;
        }

        Worker getWorker() {
            return m_worker;
        }

        Map<String, Object> getParams() {
            return m_params;
        }

        String getOwner() {
            return m_owner;
        }

        long getCreatedAt() {
            return m_createdAt;
        }

        boolean isFailing() {
            return m_isFailing;
        }

        boolean isDeleting() {
            return 0 <= m_deleteRequestedAt;
        }

        void requestDelete() {
            m_deleteRequestedAt = System.currentTimeMillis();
        }

        long getDeleteRequestedAt() {
            return m_deleteRequestedAt;
        }
    }

    /**
     * An uploaded catalog bundle.
     */
    static final class Bundle {

        private final String m_id = Ids.uuid();
        private final long m_createdAt = System.currentTimeMillis();
        private final String m_createdBy;
        private String m_name;
        private String m_catalogType;
        private String m_catalogId;
        private String m_version;
        private long m_sizeBytes;
        private long m_updatedAt = m_createdAt;

        Bundle(final String _createdBy) {
            m_createdBy = _createdBy;
        }

        String getId() {
            return m_id;
        }

        String getCatalogId() {
            return m_catalogId;
        }

        String getCatalogType() {
            return m_catalogType;
        }

        String getVersion() {
            return m_version;
        }

        long getCreatedAt() {
            return m_createdAt;
        }

        /**
         * Replaces the bundle content.
         *
         * @param _metadata the parsed bundle metadata
         * @param _sizeBytes the archive size
         */
        void update(final BundleArchive.Metadata _metadata, final long _sizeBytes) {
            m_name = _metadata.getName();
            m_catalogType = _metadata.getType();
            m_catalogId = _metadata.getId();
            m_version = _metadata.getVersion();
            m_sizeBytes = _sizeBytes;
            m_updatedAt = System.currentTimeMillis();
        }

        /**
         * Returns the bundle as serialized by the API.
         *
         * @return the bundle object
         */
        Map<String, Object> toView() {
            final Map<String, Object> view = Json.obj("id", m_id);
            if (null != m_name) {
                view.put("name", m_name);
            }
            view.put("status", "active");
            view.put("created_at", Times.isoMicrosZ(m_createdAt));
            view.put("updated_at", Times.isoMicrosZ(m_updatedAt));
            view.put("size_bytes", Long.valueOf(m_sizeBytes));
            view.put("catalog_type", m_catalogType);
            view.put("catalog_id", m_catalogId);
            view.put("version", m_version);
            view.put("created_by", m_createdBy);
            return view;
        }
    }

    CatalogState(final MockConfig _config) {
        m_config = _config;
        final Worker local = new Worker(LOCAL_WORKER, _config.getCatalogRuntime(), true);
        m_workers.put(local.getId(), local);
    }

    MockConfig getConfig() {
        return m_config;
    }

    Map<String, Worker> workers() {
        return m_workers;
    }

    Map<String, Application> applications() {
        purge();
        return m_applications;
    }

    Map<String, Datasource> datasources() {
        return m_datasources;
    }

    Map<String, DeployedModel> models() {
        purge();
        return m_models;
    }

    Map<String, Bundle> bundles() {
        return m_bundles;
    }

    /**
     * Finds a worker by name, treating an empty name as the local worker.
     *
     * @param _name the worker name; {@code null} or empty selects {@link #LOCAL_WORKER}
     * @return the worker, or {@code null}
     */
    Worker workerByName(final String _name) {
        final String name = null == _name || _name.trim().isEmpty() ? LOCAL_WORKER : _name.trim();
        for (final Worker worker : m_workers.values()) {
            if (worker.getName().equals(name)) {
                return worker;
            }
        }
        return null;
    }

    String workerStatus(final Worker _worker) {
        if (_worker.isBootstrapped()) {
            return "ready";
        }
        return JobPhase.DONE == JobPhase.of(_worker.getRegisteredAt(), m_config.getJobDurationMillis(), System.currentTimeMillis()) ? "ready" : "pending";
    }

    /**
     * Removes applications and models whose simulated deletion has finished.
     */
    private void purge() {
        final long now = System.currentTimeMillis();
        final long duration = m_config.getJobDurationMillis();
        final Iterator<Application> applications = m_applications.values().iterator();
        while (applications.hasNext()) {
            final Application application = applications.next();
            if (application.isDeleting() && now - application.getDeleteRequestedAt() >= duration) {
                applications.remove();
            }
        }
        final Iterator<DeployedModel> models = m_models.values().iterator();
        while (models.hasNext()) {
            final DeployedModel model = models.next();
            if (model.isDeleting() && now - model.getDeleteRequestedAt() >= duration) {
                models.remove();
            }
        }
    }

    /**
     * Computes the deployment status shared by applications and local models.
     *
     * @param _createdAt the creation time
     * @param _isFailing whether the deployment should fail
     * @return {@code Downloading}, {@code Deploying}, {@code Running} or {@code Error}
     */
    String deploymentStatus(final long _createdAt, final boolean _isFailing) {
        final JobPhase phase = JobPhase.of(_createdAt, m_config.getJobDurationMillis(), System.currentTimeMillis());
        if (JobPhase.QUEUED == phase) {
            return "Downloading";
        }
        if (JobPhase.RUNNING == phase) {
            return "Deploying";
        }
        return _isFailing ? "Error" : "Running";
    }

    String applicationStatus(final Application _application) {
        return _application.isDeleting() ? "Deleting" : deploymentStatus(_application.getCreatedAt(), _application.isFailing());
    }

    /**
     * Returns the human readable message for the application's status.
     *
     * @param _application the application
     * @return the message; empty when running
     */
    String applicationMessage(final Application _application) {
        final String status = applicationStatus(_application);
        if ("Error".equals(status)) {
            return "Deployment failed: pod crashed during startup (simulated)";
        }
        if ("Downloading".equals(status)) {
            return "Pulling container images";
        }
        return "Deploying".equals(status) ? "Starting pods" : "";
    }

    /**
     * Returns the IDs of applications connected to a datasource.
     *
     * @param _datasourceId the datasource ID
     * @return the connected applications
     */
    List<Application> applicationsUsing(final String _datasourceId) {
        final List<Application> using = new ArrayList<Application>();
        for (final Application application : applications().values()) {
            if (application.getDatasources().containsKey(_datasourceId)) {
                using.add(application);
            }
        }
        return using;
    }

    /**
     * Returns the IDs of applications running on a worker.
     *
     * @param _worker the worker
     * @return the application IDs
     */
    Set<String> applicationIdsOn(final Worker _worker) {
        final Set<String> ids = new LinkedHashSet<String>();
        for (final Application application : applications().values()) {
            if (application.getWorker() == _worker) {
                ids.add(application.getId());
            }
        }
        return ids;
    }
}
