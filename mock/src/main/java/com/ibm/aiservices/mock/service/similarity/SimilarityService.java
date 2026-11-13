package com.ibm.aiservices.mock.service.similarity;

import java.util.ArrayList;
import java.util.Arrays;
import java.util.List;
import java.util.Locale;
import java.util.Map;

import com.ibm.aiservices.mock.MockConfig;
import com.ibm.aiservices.mock.http.Request;
import com.ibm.aiservices.mock.http.Response;
import com.ibm.aiservices.mock.http.Router;
import com.ibm.aiservices.mock.service.common.ErrorCode;
import com.ibm.aiservices.mock.service.common.FastApi;
import com.ibm.aiservices.mock.service.common.FastApiService;
import com.ibm.aiservices.mock.service.common.MockText;
import com.ibm.aiservices.mock.util.Json;
import com.ibm.aiservices.mock.util.JsonBody;

/**
 * Mock of the similarity search service ({@code services/similarity}), backed by a small fixed corpus.
 */
public final class SimilarityService extends FastApiService {

    private static final List<String> MODES = Arrays.asList("dense", "sparse", "hybrid");
    private static final int MAX_QUERY_TOKENS = 512;

    /**
     * Creates the service.
     *
     * @param _config the shared mock configuration
     */
    public SimilarityService(final MockConfig _config) {
        super(_config);
    }

    @Override
    public String getName() {
        return "similarity";
    }

    @Override
    public String getTitle() {
        return "Similarity Search API";
    }

    @Override
    public int getDefaultPort() {
        return 7000;
    }

    @Override
    protected void registerApiRoutes(final Router _router) {
        _router.post("/v1/similarity-search", this::search);
    }

    /**
     * Handles {@code POST /v1/similarity-search}.
     *
     * @param _request the request
     * @return the ranked results
     */
    private Response search(final Request _request) {
        final long started = System.nanoTime();
        final JsonBody body = _request.getJson();
        if (!body.has("query")) {
            throw FastApi.missing("body", "query");
        }
        final String query = body.getString("query", "");
        final String mode = body.getString("mode", "dense");
        final long topK = body.getLong("top_k", 10L);
        final boolean rerank = body.getBoolean("rerank", false);
        if (topK < 1) {
            throw FastApi.validation("greater_than_equal", Json.list("body", "top_k"), "Input should be greater than or equal to 1", Long.valueOf(topK));
        }
        if (query.trim().isEmpty()) {
            throw FastApi.error(ErrorCode.EMPTY_INPUT, "query is required");
        }
        if (!MODES.contains(mode)) {
            throw FastApi.error(ErrorCode.INVALID_PARAMETER, "mode must be one of: dense, sparse, hybrid");
        }
        if (MockText.tokenCount(query) > MAX_QUERY_TOKENS) {
            // The real service re-raises its own 400 as a 500 from a broad except block; the mock reports the intended 400.
            throw FastApi.error(ErrorCode.INVALID_REQUEST, "Query exceeds the maximum of " + MAX_QUERY_TOKENS + " tokens");
        }
        final List<Map<String, Object>> results = search(query, (int) topK);
        final String scoreType = rerank ? "relevance" : scoreTypeFor(mode);
        final double elapsed = (System.nanoTime() - started) / 1.0e9;
        final Response response = Response.json(200, Json.obj("score_type", scoreType, "results", results)).withHeader("X-Retrieve-Time", String.format(Locale.ROOT, "%.4f", Double.valueOf(elapsed))).withHeader("X-Total-Time", String.format(Locale.ROOT, "%.4f", Double.valueOf(elapsed)));
        if (rerank) {
            response.withHeader("X-Rerank-Time", "0.0000");
        }
        return response;
    }

    private static String scoreTypeFor(final String _mode) {
        if ("sparse".equals(_mode)) {
            return "bm25";
        }
        return "hybrid".equals(_mode) ? "hybrid" : "cosine";
    }

    /**
     * Scores the mock corpus against a query by word overlap.
     *
     * @param _query the query text
     * @param _topK the number of results to return
     * @return up to {@code _topK} results, best first
     */
    public static List<Map<String, Object>> search(final String _query, final int _topK) {
        final List<Map<String, Object>> results = new ArrayList<Map<String, Object>>();
        final String[] queryWords = _query.toLowerCase(Locale.ROOT).split("\\W+");
        final List<String[]> corpus = MockCorpus.documents();
        for (int i = 0; i < corpus.size(); i++) {
            final String[] document = corpus.get(i);
            final String content = document[2].toLowerCase(Locale.ROOT);
            int overlap = 0;
            for (final String word : queryWords) {
                if (3 < word.length() && content.contains(word)) {
                    overlap++;
                }
            }
            final double score = Math.min(0.99, 0.35 + 0.12 * overlap - 0.01 * i);
            results.add(Json.obj("page_content", document[2], "filename", document[0], "type", document[1], "source", document[0], "chunk_id", String.valueOf(10 + i), "score", Double.valueOf(Math.round(score * 10000.0) / 10000.0)));
        }
        results.sort((_a, _b) -> Double.compare(((Number) _b.get("score")).doubleValue(), ((Number) _a.get("score")).doubleValue()));
        return results.subList(0, Math.min(_topK, results.size()));
    }
}
