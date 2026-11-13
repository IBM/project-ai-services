package com.ibm.aiservices.mock;

import java.util.Collections;
import java.util.LinkedHashMap;
import java.util.Map;

/**
 * Runtime settings for the mock server, populated from command line options.
 */
public final class MockConfig {

    private String m_host = "0.0.0.0";
    private long m_jobDurationMillis = 6000L;
    private long m_streamDelayMillis = 40L;
    private String m_catalogUsername = "admin";
    private String m_catalogPassword = "admin";
    private String m_catalogRuntime = "podman";
    private final Map<String, Integer> m_ports = new LinkedHashMap<String, Integer>();
    private final Map<String, Boolean> m_enabled = new LinkedHashMap<String, Boolean>();

    /**
     * Returns the bind address for every service.
     *
     * @return the host name or IP address
     */
    public String getHost() {
        return m_host;
    }

    /**
     * Sets the bind address for every service.
     *
     * @param _host the host name or IP address
     */
    public void setHost(final String _host) {
        m_host = _host;
    }

    /**
     * Returns how long a simulated asynchronous job takes from submission to completion.
     *
     * @return the duration in milliseconds
     */
    public long getJobDurationMillis() {
        return m_jobDurationMillis;
    }

    /**
     * Sets how long a simulated asynchronous job takes from submission to completion.
     *
     * @param _jobDurationMillis the duration in milliseconds; {@code 0} completes jobs immediately
     */
    public void setJobDurationMillis(final long _jobDurationMillis) {
        m_jobDurationMillis = Math.max(0L, _jobDurationMillis);
    }

    /**
     * Returns the pause between streamed server-sent events.
     *
     * @return the delay in milliseconds
     */
    public long getStreamDelayMillis() {
        return m_streamDelayMillis;
    }

    /**
     * Sets the pause between streamed server-sent events.
     *
     * @param _streamDelayMillis the delay in milliseconds
     */
    public void setStreamDelayMillis(final long _streamDelayMillis) {
        m_streamDelayMillis = Math.max(0L, _streamDelayMillis);
    }

    /**
     * Returns the user name accepted by the catalog login endpoint.
     *
     * @return the user name
     */
    public String getCatalogUsername() {
        return m_catalogUsername;
    }

    /**
     * Sets the user name accepted by the catalog login endpoint.
     *
     * @param _catalogUsername the user name
     */
    public void setCatalogUsername(final String _catalogUsername) {
        m_catalogUsername = _catalogUsername;
    }

    /**
     * Returns the password accepted by the catalog login endpoint.
     *
     * @return the password
     */
    public String getCatalogPassword() {
        return m_catalogPassword;
    }

    /**
     * Sets the password accepted by the catalog login endpoint.
     *
     * @param _catalogPassword the password
     */
    public void setCatalogPassword(final String _catalogPassword) {
        m_catalogPassword = _catalogPassword;
    }

    /**
     * Returns the container runtime the mock catalog server pretends to manage.
     *
     * @return {@code podman} or {@code openshift}
     */
    public String getCatalogRuntime() {
        return m_catalogRuntime;
    }

    /**
     * Sets the container runtime the mock catalog server pretends to manage.
     *
     * @param _catalogRuntime {@code podman} or {@code openshift}
     */
    public void setCatalogRuntime(final String _catalogRuntime) {
        m_catalogRuntime = _catalogRuntime;
    }

    /**
     * Returns the port configured for a service.
     *
     * @param _service the service name
     * @param _defaultPort the port used when none was configured
     * @return the port
     */
    public int getPort(final String _service, final int _defaultPort) {
        final Integer port = m_ports.get(_service);
        return null == port ? _defaultPort : port.intValue();
    }

    /**
     * Overrides the port of a service.
     *
     * @param _service the service name
     * @param _port the port
     */
    public void setPort(final String _service, final int _port) {
        m_ports.put(_service, Integer.valueOf(_port));
    }

    /**
     * Returns the configured port overrides.
     *
     * @return an unmodifiable map from service name to port
     */
    public Map<String, Integer> getPortOverrides() {
        return Collections.unmodifiableMap(m_ports);
    }

    /**
     * Returns whether a service should be started.
     *
     * @param _service the service name
     * @return {@code true} unless the service was disabled, or other services were explicitly selected
     */
    public boolean isEnabled(final String _service) {
        if (m_enabled.isEmpty()) {
            return true;
        }
        final Boolean enabled = m_enabled.get(_service);
        return null != enabled && enabled.booleanValue();
    }

    /**
     * Restricts startup to an explicit set of services.
     *
     * @param _service a service to start
     */
    public void enable(final String _service) {
        m_enabled.put(_service, Boolean.TRUE);
    }
}
