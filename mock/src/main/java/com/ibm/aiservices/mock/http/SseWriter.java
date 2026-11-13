package com.ibm.aiservices.mock.http;

import java.io.IOException;
import java.io.InterruptedIOException;
import java.io.OutputStream;
import java.nio.charset.StandardCharsets;

import com.ibm.aiservices.mock.util.Json;

/**
 * Writes server-sent events to a response stream, flushing after every event.
 */
public final class SseWriter {

    private final OutputStream m_out;
    private final long m_delayMillis;

    /**
     * Creates a writer.
     *
     * @param _out the response body stream
     * @param _delayMillis the pause inserted by {@link #pause()} between events, simulating token generation
     */
    public SseWriter(final OutputStream _out, final long _delayMillis) {
        m_out = _out;
        m_delayMillis = _delayMillis;
    }

    /**
     * Writes one {@code data:} event whose payload is the JSON form of a value.
     *
     * @param _payload the value to serialize
     * @throws IOException if the client disconnected
     */
    public void dataJson(final Object _payload) throws IOException {
        data(Json.toJson(_payload));
    }

    /**
     * Writes one {@code data:} event with a literal payload.
     *
     * @param _payload the payload text, e.g. {@code [DONE]}
     * @throws IOException if the client disconnected
     */
    public void data(final String _payload) throws IOException {
        m_out.write(("data: " + _payload + "\n\n").getBytes(StandardCharsets.UTF_8));
        m_out.flush();
    }

    /**
     * Waits for the configured inter-event delay.
     *
     * @throws IOException if the waiting thread is interrupted, which aborts the stream
     */
    public void pause() throws IOException {
        if (m_delayMillis <= 0) {
            return;
        }
        try {
            Thread.sleep(m_delayMillis);
        } catch (final InterruptedException e) {
            Thread.currentThread().interrupt();
            final InterruptedIOException interrupted = new InterruptedIOException("Stream interrupted");
            interrupted.initCause(e);
            throw interrupted;
        }
    }
}
