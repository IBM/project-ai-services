package com.ibm.aiservices.mock.service.similarity;

import java.util.Arrays;
import java.util.Collections;
import java.util.List;

/**
 * Fixed document chunks returned by the mock retrieval endpoints.
 */
public final class MockCorpus {

    /** Each entry is {@code {filename, type, page_content}}. */
    private static final List<String[]> DOCUMENTS = Collections.unmodifiableList(Arrays.asList(
            new String[] {"ai-services-overview.pdf", "text", "IBM AI Services packages retrieval-augmented generation, summarization, extraction and translation as deployable services on Power servers."},
            new String[] {"ai-services-overview.pdf", "text", "The digitize service converts PDF, DOCX, HTML, Markdown and text documents into structured JSON, Markdown or plain text and can ingest them into the vector store."},
            new String[] {"spyre-accelerator-guide.pdf", "text", "IBM Spyre accelerators run large language models such as Granite with low latency; each card exposes the ibm.com/spyre_pf resource."},
            new String[] {"spyre-accelerator-guide.pdf", "table", "| Model | Cards | Context | \n| granite-3.3-8b-instruct | 4 | 32768 |"},
            new String[] {"operations-handbook.docx", "text", "Applications are deployed from the catalog; each application runs its services and components on a worker using Podman or OpenShift."},
            new String[] {"operations-handbook.docx", "text", "Datasources such as S3 object storage or SFTP file systems can be connected to an application to keep its knowledge base in sync."},
            new String[] {"faq.md", "text", "To ask questions about your documents, upload them with the digitize ingestion operation, then call the chat completions endpoint."},
            new String[] {"faq.md", "text", "The similarity search API supports dense, sparse and hybrid retrieval with optional reranking of the top results."}));

    private MockCorpus() {
    }

    /**
     * Returns the corpus.
     *
     * @return an unmodifiable list of {@code {filename, type, page_content}} entries
     */
    public static List<String[]> documents() {
        return DOCUMENTS;
    }
}
