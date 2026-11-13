package com.ibm.aiservices.mock.service.common;

import java.nio.charset.StandardCharsets;
import java.util.Locale;

import com.ibm.aiservices.mock.http.MultipartForm;

/**
 * Text content recovered from an uploaded file.
 * <p>
 * Plain text formats are decoded as UTF-8. Binary formats such as PDF or DOCX are not parsed; the mock substitutes placeholder text whose length scales with the file size, so word counts stay plausible.
 */
public final class UploadedText {

    private UploadedText() {
    }

    /**
     * Returns the lower-case extension of a file name, including the dot.
     *
     * @param _filename the file name; may be {@code null}
     * @return the extension, e.g. {@code .pdf}, or an empty string
     */
    public static String extension(final String _filename) {
        if (null == _filename) {
            return "";
        }
        final int dot = _filename.lastIndexOf('.');
        return dot < 0 ? "" : _filename.substring(dot).toLowerCase(Locale.ROOT);
    }

    /**
     * Returns a file name without its extension.
     *
     * @param _filename the file name
     * @return the stem
     */
    public static String stem(final String _filename) {
        final int dot = _filename.lastIndexOf('.');
        return dot <= 0 ? _filename : _filename.substring(0, dot);
    }

    /**
     * Extracts text from an uploaded file.
     *
     * @param _file the uploaded part
     * @return the decoded text, or generated placeholder text for binary formats
     */
    public static String extract(final MultipartForm.Part _file) {
        final String extension = extension(_file.getFilename());
        if (".txt".equals(extension) || ".md".equals(extension) || ".html".equals(extension) || ".htm".equals(extension) || ".json".equals(extension)) {
            return new String(_file.getData(), StandardCharsets.UTF_8);
        }
        return placeholder(_file.getFilename(), _file.getData().length);
    }

    /**
     * Generates placeholder text standing in for a binary document's content.
     *
     * @param _filename the document name
     * @param _sizeBytes the document size, used to scale the text
     * @return the placeholder text
     */
    public static String placeholder(final String _filename, final int _sizeBytes) {
        final int sentences = Math.max(3, Math.min(400, _sizeBytes / 600));
        final StringBuilder text = new StringBuilder();
        for (int i = 0; i < sentences; i++) {
            text.append("Section ").append(i + 1).append(" of ").append(_filename).append(" describes the configuration, operation and expected results of the system in detail. ");
        }
        return text.toString().trim();
    }
}
