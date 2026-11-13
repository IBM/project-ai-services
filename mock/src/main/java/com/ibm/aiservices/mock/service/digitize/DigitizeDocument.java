package com.ibm.aiservices.mock.service.digitize;

import java.util.ArrayList;
import java.util.List;
import java.util.Map;

/**
 * State of one document submitted to the digitize service.
 * <p>
 * While its job exists, a document's status is derived from the job's simulated progress. When the job is deleted (documents outlive their job) or the document is imported, the status is frozen with {@link #freeze(String, Long, String, Map)}.
 */
final class DigitizeDocument {

    private final String m_id;
    private final String m_jobId;
    private final String m_name;
    private final String m_operation;
    private final String m_outputFormat;
    private final long m_submittedAt;
    private final String m_contentHash;
    private final String m_text;
    private final int m_pages;
    private final boolean m_isFailing;
    private final String m_duplicateStatus;
    private final String m_duplicateMessage;
    private final List<String> m_duplicateNames = new ArrayList<String>();
    private String m_frozenStatus;
    private Long m_frozenCompletedAt;
    private String m_frozenError;
    private Map<String, Object> m_frozenMetadata;

    /**
     * Creates a document record.
     *
     * @param _id the document ID
     * @param _job the owning job, which supplies the operation, output format and submission time
     * @param _name the file name
     * @param _contentHash a hash of the content, used for duplicate detection
     * @param _text the extracted text
     * @param _pages the simulated page count
     * @param _isFailing whether processing should fail
     * @param _duplicateStatus {@code already_exists} or {@code skipped} for duplicates, otherwise {@code null}
     * @param _duplicateMessage the message reported for duplicates, otherwise {@code null}
     */
    DigitizeDocument(final String _id, final DigitizeJob _job, final String _name, final String _contentHash, final String _text, final int _pages, final boolean _isFailing, final String _duplicateStatus, final String _duplicateMessage) {
        m_id = _id;
        m_jobId = _job.getId();
        m_operation = _job.getOperation();
        m_outputFormat = _job.getOutputFormat();
        m_submittedAt = _job.getSubmittedAt();
        m_name = _name;
        m_contentHash = _contentHash;
        m_text = _text;
        m_pages = _pages;
        m_isFailing = _isFailing;
        m_duplicateStatus = _duplicateStatus;
        m_duplicateMessage = _duplicateMessage;
    }

    String getId() {
        return m_id;
    }

    String getJobId() {
        return m_jobId;
    }

    String getName() {
        return m_name;
    }

    String getOperation() {
        return m_operation;
    }

    String getOutputFormat() {
        return m_outputFormat;
    }

    long getSubmittedAt() {
        return m_submittedAt;
    }

    String getContentHash() {
        return m_contentHash;
    }

    String getText() {
        return m_text;
    }

    int getPages() {
        return m_pages;
    }

    boolean isFailing() {
        return m_isFailing;
    }

    boolean isDuplicate() {
        return null != m_duplicateStatus;
    }

    String getDuplicateStatus() {
        return m_duplicateStatus;
    }

    String getDuplicateMessage() {
        return m_duplicateMessage;
    }

    List<String> getDuplicateNames() {
        return m_duplicateNames;
    }

    boolean isFrozen() {
        return null != m_frozenStatus;
    }

    String getFrozenStatus() {
        return m_frozenStatus;
    }

    Long getFrozenCompletedAt() {
        return m_frozenCompletedAt;
    }

    String getFrozenError() {
        return m_frozenError;
    }

    Map<String, Object> getFrozenMetadata() {
        return m_frozenMetadata;
    }

    /**
     * Pins the document's reported state, used for imported documents and documents whose job was deleted.
     *
     * @param _status the status
     * @param _completedAt the completion time in epoch milliseconds, or {@code null}
     * @param _error the error, or {@code null}
     * @param _metadata the metadata, or {@code null}
     */
    void freeze(final String _status, final Long _completedAt, final String _error, final Map<String, Object> _metadata) {
        m_frozenStatus = _status;
        m_frozenCompletedAt = _completedAt;
        m_frozenError = _error;
        m_frozenMetadata = _metadata;
    }
}
