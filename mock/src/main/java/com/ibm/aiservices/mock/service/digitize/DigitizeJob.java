package com.ibm.aiservices.mock.service.digitize;

import java.util.ArrayList;
import java.util.List;

/**
 * State of one digitize job. Statuses are derived from elapsed time unless the job was imported with a fixed status or cancelled.
 */
final class DigitizeJob {

    private final String m_id;
    private final String m_name;
    private final String m_operation;
    private final String m_outputFormat;
    private final long m_submittedAt;
    private final List<DigitizeDocument> m_documents = new ArrayList<DigitizeDocument>();
    private String m_importedStatus;
    private Long m_importedCompletedAt;
    private String m_importedError;
    private long m_cancelRequestedAt = -1L;

    /**
     * Creates a job record.
     *
     * @param _id the job ID
     * @param _name the job name, or {@code null}
     * @param _operation {@code ingestion} or {@code digitization}
     * @param _outputFormat the output format
     * @param _submittedAt the submission time in epoch milliseconds
     */
    DigitizeJob(final String _id, final String _name, final String _operation, final String _outputFormat, final long _submittedAt) {
        m_id = _id;
        m_name = _name;
        m_operation = _operation;
        m_outputFormat = _outputFormat;
        m_submittedAt = _submittedAt;
    }

    String getId() {
        return m_id;
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

    List<DigitizeDocument> getDocuments() {
        return m_documents;
    }

    String getImportedStatus() {
        return m_importedStatus;
    }

    Long getImportedCompletedAt() {
        return m_importedCompletedAt;
    }

    String getImportedError() {
        return m_importedError;
    }

    /**
     * Pins the job to the status, completion time and error from an imported record.
     *
     * @param _status the imported status
     * @param _completedAt the imported completion time in epoch milliseconds, or {@code null}
     * @param _error the imported error, or {@code null}
     */
    void setImported(final String _status, final Long _completedAt, final String _error) {
        m_importedStatus = _status;
        m_importedCompletedAt = _completedAt;
        m_importedError = _error;
    }

    long getCancelRequestedAt() {
        return m_cancelRequestedAt;
    }

    boolean isCancelRequested() {
        return 0 <= m_cancelRequestedAt;
    }

    void requestCancel(final long _at) {
        m_cancelRequestedAt = _at;
    }
}
