package com.ibm.aiservices.mock;

import java.io.IOException;
import java.io.PrintStream;
import java.net.InetSocketAddress;
import java.util.ArrayList;
import java.util.Arrays;
import java.util.List;
import java.util.Locale;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;
import java.util.concurrent.ThreadFactory;
import java.util.concurrent.atomic.AtomicInteger;
import java.util.logging.Level;
import java.util.logging.Logger;

import com.ibm.aiservices.mock.service.MockService;
import com.ibm.aiservices.mock.service.catalog.CatalogService;
import com.ibm.aiservices.mock.service.chatbot.ChatbotService;
import com.ibm.aiservices.mock.service.digitize.DigitizeService;
import com.ibm.aiservices.mock.service.extract.ExtractService;
import com.ibm.aiservices.mock.service.forecast.ForecastService;
import com.ibm.aiservices.mock.service.similarity.SimilarityService;
import com.ibm.aiservices.mock.service.summarize.SummarizeService;
import com.ibm.aiservices.mock.service.translate.TranslateService;
import com.sun.net.httpserver.HttpServer;

/**
 * Entry point of the AI Services mock server. Starts one HTTP listener per mocked service, each on the port the real service uses by default.
 */
public final class MockServer {

    private static final Logger LOG = Logger.getLogger(MockServer.class.getName());

    private final MockConfig m_config;
    private final List<HttpServer> m_servers = new ArrayList<HttpServer>();
    private ExecutorService m_executor;

    /**
     * Thrown for invalid command line arguments.
     */
    static final class UsageException extends Exception {

        private static final long serialVersionUID = 1L;

        UsageException(final String _message) {
            super(_message);
        }
    }

    /**
     * Creates a server for the given configuration.
     *
     * @param _config the configuration
     */
    public MockServer(final MockConfig _config) {
        m_config = _config;
    }

    /**
     * Runs the mock server until the process is terminated.
     *
     * @param _args command line arguments; see {@code --help}
     */
    public static void main(final String[] _args) {
        System.setProperty("java.util.logging.SimpleFormatter.format", "%1$tF %1$tT.%1$tL %4$-7s %5$s%6$s%n");
        final MockConfig config = new MockConfig();
        try {
            if (!parseArguments(_args, config)) {
                printUsage(System.out);
                return;
            }
        } catch (final UsageException e) {
            System.err.println("Error: " + e.getMessage());
            printUsage(System.err);
            System.exit(2);
            return;
        }
        final MockServer server = new MockServer(config);
        Runtime.getRuntime().addShutdownHook(new Thread(server::stop, "mock-shutdown"));
        try {
            server.start();
        } catch (final IOException e) {
            LOG.log(Level.SEVERE, "Failed to start the mock server: " + e.getMessage(), e);
            server.stop();
            System.exit(1);
        }
    }

    /**
     * Creates every mocked service.
     *
     * @param _config the configuration
     * @return the services in startup order
     */
    static List<MockService> createServices(final MockConfig _config) {
        return Arrays.asList(new CatalogService(_config), new DigitizeService(_config), new ChatbotService(_config), new SummarizeService(_config), new ExtractService(_config), new SimilarityService(_config), new ForecastService(_config), new TranslateService(_config));
    }

    /**
     * Binds a listener for every enabled service.
     *
     * @throws IOException if a port cannot be bound
     */
    public void start() throws IOException {
        m_executor = Executors.newCachedThreadPool(new ThreadFactory() {

            private final AtomicInteger m_count = new AtomicInteger();

            @Override
            public Thread newThread(final Runnable _task) {
                return new Thread(_task, "mock-http-" + m_count.incrementAndGet());
            }
        });
        final String displayHost = "0.0.0.0".equals(m_config.getHost()) ? "localhost" : m_config.getHost();
        final StringBuilder banner = new StringBuilder("AI Services mock server started:");
        for (final MockService service : createServices(m_config)) {
            if (!m_config.isEnabled(service.getName())) {
                continue;
            }
            final int port = m_config.getPort(service.getName(), service.getDefaultPort());
            final HttpServer server = HttpServer.create(new InetSocketAddress(m_config.getHost(), port), 0);
            server.createContext("/", service.createRouter());
            server.setExecutor(m_executor);
            server.start();
            m_servers.add(server);
            banner.append(String.format(Locale.ROOT, "%n  %-11s http://%s:%d  (%s)", service.getName(), displayHost, Integer.valueOf(port), service.getTitle()));
        }
        banner.append(String.format(Locale.ROOT, "%nCatalog login: %s / <configured password>. Simulated job duration: %d ms.", m_config.getCatalogUsername(), Long.valueOf(m_config.getJobDurationMillis())));
        LOG.info(banner.toString());
    }

    /**
     * Stops every listener. Safe to call more than once.
     */
    public void stop() {
        synchronized (m_servers) {
            for (final HttpServer server : m_servers) {
                server.stop(0);
            }
            m_servers.clear();
        }
        if (null != m_executor) {
            m_executor.shutdownNow();
        }
    }

    /**
     * Parses command line arguments into the configuration. Options accept both {@code --name=value} and {@code --name value}.
     *
     * @param _args the arguments
     * @param _config the configuration to populate
     * @return {@code false} if help was requested
     * @throws UsageException if an argument is unknown or malformed
     */
    static boolean parseArguments(final String[] _args, final MockConfig _config) throws UsageException {
        final List<String> serviceNames = new ArrayList<String>();
        for (final MockService service : createServices(_config)) {
            serviceNames.add(service.getName());
        }
        for (int i = 0; i < _args.length; i++) {
            final String arg = _args[i];
            if ("-h".equals(arg) || "--help".equals(arg)) {
                return false;
            }
            if (!arg.startsWith("--")) {
                throw new UsageException("Unexpected argument '" + arg + "'");
            }
            final int equals = arg.indexOf('=');
            final String name = equals < 0 ? arg.substring(2) : arg.substring(2, equals);
            final String value;
            if (0 <= equals) {
                value = arg.substring(equals + 1);
            } else if (i + 1 < _args.length) {
                value = _args[++i];
            } else {
                throw new UsageException("Option --" + name + " requires a value");
            }
            applyOption(name, value, _config, serviceNames);
        }
        return true;
    }

    /**
     * Applies one option.
     *
     * @param _name the option name without dashes
     * @param _value the option value
     * @param _config the configuration
     * @param _serviceNames the valid service names
     * @throws UsageException if the option is unknown or its value is invalid
     */
    private static void applyOption(final String _name, final String _value, final MockConfig _config, final List<String> _serviceNames) throws UsageException {
        if ("host".equals(_name)) {
            _config.setHost(_value);
        } else if ("services".equals(_name)) {
            for (final String service : _value.split(",")) {
                final String trimmed = service.trim();
                if (!_serviceNames.contains(trimmed)) {
                    throw new UsageException("Unknown service '" + trimmed + "'. Valid services: " + String.join(", ", _serviceNames));
                }
                _config.enable(trimmed);
            }
        } else if ("job-duration-ms".equals(_name)) {
            _config.setJobDurationMillis(parseNumber(_name, _value));
        } else if ("stream-delay-ms".equals(_name)) {
            _config.setStreamDelayMillis(parseNumber(_name, _value));
        } else if ("catalog-user".equals(_name)) {
            _config.setCatalogUsername(_value);
        } else if ("catalog-password".equals(_name)) {
            _config.setCatalogPassword(_value);
        } else if ("catalog-runtime".equals(_name)) {
            if (!"podman".equals(_value) && !"openshift".equals(_value)) {
                throw new UsageException("--catalog-runtime must be 'podman' or 'openshift'");
            }
            _config.setCatalogRuntime(_value);
        } else if (_name.endsWith("-port") && _serviceNames.contains(_name.substring(0, _name.length() - "-port".length()))) {
            final long port = parseNumber(_name, _value);
            if (port < 1 || port > 65535) {
                throw new UsageException("--" + _name + " must be between 1 and 65535");
            }
            _config.setPort(_name.substring(0, _name.length() - "-port".length()), (int) port);
        } else {
            throw new UsageException("Unknown option --" + _name);
        }
    }

    /**
     * Parses a numeric option value.
     *
     * @param _name the option name, for the error message
     * @param _value the raw value
     * @return the parsed number
     */
    private static long parseNumber(final String _name, final String _value) throws UsageException {
        try {
            return Long.parseLong(_value.trim());
        } catch (final NumberFormatException e) {
            final UsageException usage = new UsageException("--" + _name + " expects a number, got '" + _value + "'");
            usage.initCause(e);
            throw usage;
        }
    }

    /**
     * Prints command line help.
     *
     * @param _out the stream to print to
     */
    private static void printUsage(final PrintStream _out) {
        final MockConfig defaults = new MockConfig();
        _out.println("Usage: java -jar ai-services-mock-jar-with-dependencies.jar [options]");
        _out.println();
        _out.println("Starts in-memory mocks of the AI Services REST APIs, one listener per service.");
        _out.println();
        _out.println("Options:");
        _out.println("  --host <address>            Bind address for all services (default " + defaults.getHost() + ")");
        _out.println("  --services <a,b,...>        Start only these services (default: all)");
        _out.println("  --<service>-port <port>     Override a service port, e.g. --catalog-port 18080");
        _out.println("  --job-duration-ms <ms>      Time a simulated job or deployment takes (default " + defaults.getJobDurationMillis() + ")");
        _out.println("  --stream-delay-ms <ms>      Delay between streamed SSE chunks (default " + defaults.getStreamDelayMillis() + ")");
        _out.println("  --catalog-user <name>       Catalog login user name (default " + defaults.getCatalogUsername() + ")");
        _out.println("  --catalog-password <pass>   Catalog login password (default " + defaults.getCatalogPassword() + ")");
        _out.println("  --catalog-runtime <rt>      podman or openshift (default " + defaults.getCatalogRuntime() + ")");
        _out.println("  -h, --help                  Show this help");
        _out.println();
        _out.println("Services and default ports:");
        for (final MockService service : createServices(defaults)) {
            _out.println(String.format(Locale.ROOT, "  %-11s %5d  %s", service.getName(), Integer.valueOf(service.getDefaultPort()), service.getTitle()));
        }
    }
}
