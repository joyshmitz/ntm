package robot

// PaginationOptions configures limit/offset pagination.
type PaginationOptions struct {
	Limit  int
	Offset int
}

// PaginationInfo describes pagination state for array outputs.
type PaginationInfo struct {
	Limit      int  `json:"limit"`
	Offset     int  `json:"offset"`
	Count      int  `json:"count"`
	Total      int  `json:"total"`
	HasMore    bool `json:"has_more"`
	NextCursor *int `json:"next_cursor,omitempty"`
}

// ApplyPagination slices items based on limit/offset and returns pagination metadata.
func ApplyPagination[T any](items []T, opts PaginationOptions) ([]T, *PaginationInfo) {
	if opts.Limit <= 0 && opts.Offset <= 0 {
		return items, nil
	}

	total := len(items)
	offset := opts.Offset
	if offset < 0 {
		offset = 0
	}
	if offset > total {
		offset = total
	}

	limit := opts.Limit
	if limit <= 0 {
		limit = total - offset
	}

	end := offset + limit
	if end > total {
		end = total
	}

	paged := items[offset:end]
	hasMore := end < total
	var next *int
	if hasMore {
		n := end
		next = &n
	}

	info := &PaginationInfo{
		Limit:      limit,
		Offset:     offset,
		Count:      len(paged),
		Total:      total,
		HasMore:    hasMore,
		NextCursor: next,
	}

	return paged, info
}
