package com.ibm.aiservices.mock.service.common;

import java.util.ArrayList;
import java.util.List;
import java.util.Locale;

/**
 * Deterministic placeholder text and token estimates standing in for LLM output.
 */
public final class MockText {

    /** Model name reported in response metadata. */
    public static final String MODEL = "ibm-granite/granite-3.3-8b-instruct";

    private MockText() {
    }

    /**
     * Counts whitespace separated words.
     *
     * @param _text the text; may be {@code null}
     * @return the word count
     */
    public static int wordCount(final String _text) {
        if (null == _text || _text.trim().isEmpty()) {
            return 0;
        }
        return _text.trim().split("\\s+").length;
    }

    /**
     * Estimates the token count of a text with the common rule of thumb of 1.3 tokens per word.
     *
     * @param _text the text; may be {@code null}
     * @return the estimated token count, at least 1 for non-empty text
     */
    public static int tokenCount(final String _text) {
        final int words = wordCount(_text);
        return 0 == words ? 0 : Math.max(1, (int) Math.ceil(words * 1.3));
    }

    /**
     * Produces a mock summary of a text by taking its leading words.
     *
     * @param _text the source text
     * @param _maxWords the maximum number of words to keep
     * @return the summary
     */
    public static String summarize(final String _text, final int _maxWords) {
        final String[] words = null == _text ? new String[0] : _text.trim().split("\\s+");
        final StringBuilder summary = new StringBuilder("Summary: ");
        final int count = Math.min(words.length, Math.max(1, _maxWords));
        for (int i = 0; i < count; i++) {
            if (0 < i) {
                summary.append(' ');
            }
            summary.append(words[i]);
        }
        if (words.length > count) {
            summary.append(" ...");
        }
        return summary.toString();
    }

    /**
     * Produces a mock translation by tagging the input with the target language.
     *
     * @param _text the source text
     * @param _targetLanguage the target language name
     * @return the mock translation
     */
    public static String translate(final String _text, final String _targetLanguage) {
        return "[" + _targetLanguage.toLowerCase(Locale.ROOT) + "] " + _text;
    }

    /**
     * Splits text into small chunks to emit as streamed deltas.
     *
     * @param _text the full text
     * @return word-sized chunks that concatenate back to the input
     */
    public static List<String> streamChunks(final String _text) {
        final List<String> chunks = new ArrayList<String>();
        final String[] words = _text.split(" ");
        for (int i = 0; i < words.length; i++) {
            chunks.add(0 == i ? words[i] : " " + words[i]);
        }
        return chunks;
    }

    /**
     * Guesses whether text is German, standing in for the services' lingua-based language detection.
     *
     * @param _text the text
     * @return {@code german} or {@code english}
     */
    public static String detectLanguage(final String _text) {
        final String lower = null == _text ? "" : " " + _text.toLowerCase(Locale.ROOT) + " ";
        final String[] markers = {" der ", " die ", " das ", " und ", " ist ", " nicht ", " ich ", " ein ", " eine ", " mit "};
        int hits = 0;
        for (final String marker : markers) {
            if (lower.contains(marker)) {
                hits++;
            }
        }
        return hits >= 2 ? "german" : "english";
    }
}
