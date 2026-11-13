package com.ibm.aiservices.mock.service;

import java.util.Locale;

import com.ibm.aiservices.mock.MockConfig;
import com.ibm.aiservices.mock.http.ErrorResponder;
import com.ibm.aiservices.mock.http.Router;

/**
 * Base class for one mocked service. Each service listens on its own port, as in a real deployment.
 */
public abstract class MockService {

    private final MockConfig m_config;

    /**
     * Creates the service.
     *
     * @param _config the shared mock configuration
     */
    protected MockService(final MockConfig _config) {
        m_config = _config;
    }

    /**
     * Returns the shared configuration.
     *
     * @return the configuration
     */
    protected MockConfig getConfig() {
        return m_config;
    }

    /**
     * Returns the short name used on the command line and in logs.
     *
     * @return the service name, e.g. {@code digitize}
     */
    public abstract String getName();

    /**
     * Returns a one-line description shown in the startup banner and documentation page.
     *
     * @return the description
     */
    public abstract String getTitle();

    /**
     * Returns the port the real service listens on by default.
     *
     * @return the default port
     */
    public abstract int getDefaultPort();

    /**
     * Returns the responder that formats framework-level errors like the real service.
     *
     * @return the error responder
     */
    protected abstract ErrorResponder getErrorResponder();

    /**
     * Registers every route of the service.
     *
     * @param _router the router to register with
     */
    protected abstract void registerRoutes(Router _router);

    /**
     * Builds the fully populated router for this service.
     *
     * @return the router
     */
    public final Router createRouter() {
        final Router router = new Router(getName(), getErrorResponder());
        registerRoutes(router);
        return router;
    }

    /**
     * Returns the simulated duration of an asynchronous job.
     *
     * @return the duration in milliseconds
     */
    protected long jobDuration() {
        return m_config.getJobDurationMillis();
    }

    /**
     * Computes the simulated lifecycle phase of an asynchronous job.
     *
     * @param _submittedAt the submission time in epoch milliseconds
     * @return the current phase
     */
    protected JobPhase phaseOf(final long _submittedAt) {
        return JobPhase.of(_submittedAt, jobDuration(), System.currentTimeMillis());
    }

    /**
     * Returns the simulated completion time of a job submitted at the given time.
     *
     * @param _submittedAt the submission time in epoch milliseconds
     * @return the completion time in epoch milliseconds
     */
    protected long completionTime(final long _submittedAt) {
        return _submittedAt + jobDuration();
    }

    /**
     * Returns whether a client-supplied name asks the mock to simulate a failure.
     * <p>
     * Any job name or file name containing {@code fail} (case-insensitive) produces a failed job, so clients can exercise their error paths.
     *
     * @param _names the names to inspect; {@code null} entries are ignored
     * @return {@code true} if any name requests a failure
     */
    public static boolean requestsFailure(final String... _names) {
        for (final String name : _names) {
            if (null != name && name.toLowerCase(Locale.ROOT).contains("fail")) {
                return true;
            }
        }
        return false;
    }
}
