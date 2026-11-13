package com.ibm.aiservices.mock.service.chatbot;

import java.io.UnsupportedEncodingException;
import java.math.BigDecimal;
import java.net.URLEncoder;
import java.util.ArrayList;
import java.util.Deque;
import java.util.LinkedList;
import java.util.List;
import java.util.Map;

import com.ibm.aiservices.mock.MockConfig;
import com.ibm.aiservices.mock.http.Request;
import com.ibm.aiservices.mock.http.Response;
import com.ibm.aiservices.mock.http.Router;
import com.ibm.aiservices.mock.http.SseWriter;
import com.ibm.aiservices.mock.service.common.ErrorCode;
import com.ibm.aiservices.mock.service.common.FastApi;
import com.ibm.aiservices.mock.service.common.FastApiService;
import com.ibm.aiservices.mock.service.common.MockText;
import com.ibm.aiservices.mock.service.similarity.SimilarityService;
import com.ibm.aiservices.mock.util.Ids;
import com.ibm.aiservices.mock.util.Json;
import com.ibm.aiservices.mock.util.JsonBody;
import com.ibm.aiservices.mock.util.Times;

/**
 * Mock of the question and answer (RAG chatbot) service ({@code services/chatbot}).
 */
public final class ChatbotService extends FastApiService {

    private static final int MAX_METRICS = 200;
    private static final int MAX_QUERY_WORDS = 2000;

    private final Deque<Map<String, Object>> m_metrics = new LinkedList<Map<String, Object>>();

    /**
     * Creates the service.
     *
     * @param _config the shared mock configuration
     */
    public ChatbotService(final MockConfig _config) {
        super(_config);
    }

    @Override
    public String getName() {
        return "chatbot";
    }

    @Override
    public String getTitle() {
        return "Question and Answer API";
    }

    @Override
    public int getDefaultPort() {
        return 5000;
    }

    @Override
    protected void registerApiRoutes(final Router _router) {
        _router.get("/db-status", _request -> Response.json(200, Json.obj("ready", Boolean.TRUE)));
        _router.get("/v1/models", this::listModels);
        _router.get("/v1/perf_metrics", this::perfMetrics);
        _router.post("/v1/chat/completions", this::chatCompletions);
    }

    private Response listModels(final Request _request) {
        final Map<String, Object> model = Json.obj("id", MockText.MODEL, "object", "model", "created", Long.valueOf(System.currentTimeMillis() / 1000L), "owned_by", "vllm");
        return Response.json(200, Json.obj("object", "list", "data", Json.list(model)));
    }

    /**
     * Handles {@code GET /v1/perf_metrics}, optionally filtered by request ID.
     *
     * @param _request the request
     * @return the recorded metrics
     */
    private Response perfMetrics(final Request _request) {
        final String requestId = _request.getQuery("request_id");
        final List<Map<String, Object>> matches = new ArrayList<Map<String, Object>>();
        synchronized (m_metrics) {
            for (final Map<String, Object> metric : m_metrics) {
                if (null == requestId || requestId.equals(metric.get("request_id"))) {
                    matches.add(metric);
                }
            }
        }
        if (null != requestId && matches.isEmpty()) {
            throw FastApi.error(ErrorCode.RESOURCE_NOT_FOUND, "No metric found for request_id: " + requestId);
        }
        return Response.json(200, Json.obj("metrics", matches));
    }

    /**
     * Handles {@code POST /v1/chat/completions}: answers the last message from the mock corpus, streamed or not.
     *
     * @param _request the request
     * @return a JSON completion or an SSE stream
     */
    private Response chatCompletions(final Request _request) {
        final boolean includeDocs = FastApi.queryBool(_request, "include_docs", false);
        final JsonBody body = _request.getJson();
        final List<Object> messages = body.getList("messages");
        if (null == messages) {
            throw FastApi.missing("body", "messages");
        }
        if (messages.isEmpty()) {
            throw FastApi.error(ErrorCode.EMPTY_INPUT, "messages can't be empty");
        }
        for (int i = 0; i < messages.size(); i++) {
            final Object message = messages.get(i);
            if (!(message instanceof Map) || !(((Map<?, ?>) message).get("content") instanceof String)) {
                throw FastApi.validation("missing", Json.list("body", "messages", Integer.valueOf(i), "content"), "Field required", message);
            }
        }
        final String query = (String) ((Map<?, ?>) messages.get(messages.size() - 1)).get("content");
        if (query.trim().isEmpty()) {
            throw FastApi.error(ErrorCode.EMPTY_INPUT, "Query cannot be empty");
        }
        if (MockText.wordCount(query) > MAX_QUERY_WORDS) {
            throw FastApi.error(ErrorCode.INVALID_PARAMETER, "Your query is too long. Please shorten it and try again.");
        }

        final List<Map<String, Object>> docs = SimilarityService.search(query, 3);
        final String answer = buildAnswer(docs);
        final List<Double> tokenLatencies = body.getBoolean("stream", false) ? new ArrayList<Double>() : null;
        recordMetric(_request.getRequestId(), query, answer, tokenLatencies);

        final Response response;
        if (body.getBoolean("stream", false)) {
            response = streamAnswer(answer, includeDocs ? docs : null);
        } else {
            final Map<String, Object> payload = Json.obj("choices", Json.list(Json.obj("message", Json.obj("content", answer))));
            if (includeDocs) {
                payload.put("docs", docs);
            }
            response = Response.json(200, payload);
        }
        if (1 < messages.size()) {
            response.withHeader("X-Rephrased-Query", percentEncode(query));
        }
        return response;
    }

    private static String buildAnswer(final List<Map<String, Object>> _docs) {
        final StringBuilder answer = new StringBuilder("Based on the retrieved documents, ");
        answer.append(_docs.get(0).get("page_content"));
        answer.append(" (source: ").append(_docs.get(0).get("filename")).append(")");
        return answer.toString();
    }

    /**
     * Streams an answer as OpenAI {@code chat.completion.chunk} events, optionally preceded by a docs event.
     * <p>
     * Like the real service, the stream closes after the last chunk without re-emitting the upstream {@code [DONE]} marker.
     *
     * @param _answer the full answer text
     * @param _docs the retrieved documents to send first, or {@code null}
     * @return the streaming response
     */
    private Response streamAnswer(final String _answer, final List<Map<String, Object>> _docs) {
        final long delay = getConfig().getStreamDelayMillis();
        final String completionId = "chatcmpl-" + Ids.uuid().replace("-", "");
        final long created = System.currentTimeMillis() / 1000L;
        return Response.stream(200, Response.SSE_CONTENT_TYPE, _out -> {
            final SseWriter sse = new SseWriter(_out, delay);
            if (null != _docs) {
                sse.dataJson(Json.obj("choices", Json.list(), "docs", _docs));
            }
            final List<String> chunks = MockText.streamChunks(_answer);
            for (int i = 0; i < chunks.size(); i++) {
                sse.pause();
                final boolean isLast = i == chunks.size() - 1;
                final Map<String, Object> choice = Json.obj("index", Integer.valueOf(0), "delta", Json.obj("content", chunks.get(i)), "logprobs", null, "finish_reason", isLast ? "stop" : null);
                sse.dataJson(Json.obj("id", completionId, "object", "chat.completion.chunk", "created", Long.valueOf(created), "model", MockText.MODEL, "choices", Json.list(choice)));
            }
        }).withHeader("Cache-Control", "no-cache");
    }

    /**
     * Records performance metrics for a completed request, keeping only the most recent entries.
     *
     * @param _requestId the request ID
     * @param _query the query text
     * @param _answer the answer text
     * @param _tokenLatencies per-token latencies for streamed calls, or {@code null}
     */
    private void recordMetric(final String _requestId, final String _query, final String _answer, final List<Double> _tokenLatencies) {
        final long now = System.currentTimeMillis();
        if (null != _tokenLatencies) {
            for (int i = 0; i < MockText.streamChunks(_answer).size(); i++) {
                _tokenLatencies.add(Double.valueOf(getConfig().getStreamDelayMillis() / 1000.0));
            }
        }
        final Map<String, Object> metric = Json.obj("timestamp", BigDecimal.valueOf(now, 3), "readable_timestamp", Times.readable(now), "request_id", _requestId, "retrieve_time",
                Double.valueOf(0.15), "rerank_time", Double.valueOf(0.12), "inference_time", Double.valueOf(1.25), "completion_tokens",
                Integer.valueOf(MockText.tokenCount(_answer)), "prompt_tokens", Integer.valueOf(MockText.tokenCount(_query) + 450), "token_latencies", _tokenLatencies);
        synchronized (m_metrics) {
            m_metrics.addLast(metric);
            while (m_metrics.size() > MAX_METRICS) {
                m_metrics.removeFirst();
            }
        }
    }

    /**
     * Percent-encodes a header value, encoding spaces as {@code %20}.
     *
     * @param _value the value
     * @return the encoded value
     */
    private static String percentEncode(final String _value) {
        try {
            return URLEncoder.encode(_value, "UTF-8").replace("+", "%20");
        } catch (final UnsupportedEncodingException e) {
            // UTF-8 support is mandatory on every Java platform.
            throw new IllegalStateException(e);
        }
    }
}
