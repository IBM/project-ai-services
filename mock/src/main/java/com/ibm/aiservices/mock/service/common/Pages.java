package com.ibm.aiservices.mock.service.common;

import java.util.ArrayList;
import java.util.List;
import java.util.Map;

import com.ibm.aiservices.mock.util.Json;

/**
 * Pagination helpers for in-memory collections.
 */
public final class Pages {

    private Pages() {
    }

    /**
     * Returns one window of a list by offset and limit.
     *
     * @param <T> the element type
     * @param _items the full, already ordered list
     * @param _offset the number of items to skip
     * @param _limit the maximum number of items to return
     * @return a new list holding the requested window
     */
    public static <T> List<T> slice(final List<T> _items, final int _offset, final int _limit) {
        final int from = Math.min(Math.max(0, _offset), _items.size());
        final int to = Math.min(_items.size(), from + Math.max(0, _limit));
        return new ArrayList<T>(_items.subList(from, to));
    }

    /**
     * Builds the {@code {"pagination": {...}, "data": [...]}} envelope used by the Python services.
     *
     * @param _total the total number of matching items
     * @param _limit the requested limit
     * @param _offset the requested offset
     * @param _data the items on this page
     * @return the envelope
     */
    public static Map<String, Object> offsetEnvelope(final int _total, final int _limit, final int _offset, final List<?> _data) {
        return Json.obj("pagination", Json.obj("total", Integer.valueOf(_total), "limit", Integer.valueOf(_limit), "offset", Integer.valueOf(_offset)), "data", _data);
    }

    /**
     * Builds the page-number pagination block used by the catalog API server.
     *
     * @param _page the one-based page number
     * @param _pageSize the page size
     * @param _totalItems the total number of matching items
     * @return the pagination object
     */
    public static Map<String, Object> pageInfo(final int _page, final int _pageSize, final int _totalItems) {
        final int totalPages = 0 == _totalItems ? 0 : (_totalItems + _pageSize - 1) / _pageSize;
        return Json.obj("page", Integer.valueOf(_page), "page_size", Integer.valueOf(_pageSize), "total_items", Integer.valueOf(_totalItems), "total_pages", Integer.valueOf(totalPages), "has_next", Boolean.valueOf(_page < totalPages), "has_prev", Boolean.valueOf(_page > 1));
    }
}
