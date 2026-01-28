package robot

import (
	"reflect"
	"testing"
)

func TestApplyPagination_NoOptions(t *testing.T) {
	items := []int{1, 2, 3}

	got, page := ApplyPagination(items, PaginationOptions{})
	if page != nil {
		t.Fatalf("expected nil pagination info, got %+v", page)
	}
	if !reflect.DeepEqual(got, items) {
		t.Fatalf("expected %v, got %v", items, got)
	}
}

func TestApplyPagination_LimitOnly(t *testing.T) {
	items := []int{1, 2, 3}

	got, page := ApplyPagination(items, PaginationOptions{Limit: 2})
	if page == nil {
		t.Fatal("expected pagination info, got nil")
	}
	if !reflect.DeepEqual(got, []int{1, 2}) {
		t.Fatalf("expected first two items, got %v", got)
	}
	if page.Limit != 2 || page.Offset != 0 || page.Count != 2 || page.Total != 3 {
		t.Fatalf("unexpected pagination info: %+v", page)
	}
	if !page.HasMore || page.NextCursor == nil || *page.NextCursor != 2 {
		t.Fatalf("expected next_cursor=2 and has_more=true, got %+v", page)
	}
}

func TestApplyPagination_OffsetOnly(t *testing.T) {
	items := []int{1, 2, 3}

	got, page := ApplyPagination(items, PaginationOptions{Offset: 2})
	if page == nil {
		t.Fatal("expected pagination info, got nil")
	}
	if !reflect.DeepEqual(got, []int{3}) {
		t.Fatalf("expected last item, got %v", got)
	}
	if page.Offset != 2 || page.Count != 1 || page.Total != 3 {
		t.Fatalf("unexpected pagination info: %+v", page)
	}
	if page.HasMore || page.NextCursor != nil {
		t.Fatalf("expected has_more=false and next_cursor nil, got %+v", page)
	}
}

func TestApplyPagination_LimitAndOffset(t *testing.T) {
	items := []int{1, 2, 3, 4}

	got, page := ApplyPagination(items, PaginationOptions{Limit: 2, Offset: 1})
	if page == nil {
		t.Fatal("expected pagination info, got nil")
	}
	if !reflect.DeepEqual(got, []int{2, 3}) {
		t.Fatalf("expected middle slice, got %v", got)
	}
	if page.Limit != 2 || page.Offset != 1 || page.Count != 2 || page.Total != 4 {
		t.Fatalf("unexpected pagination info: %+v", page)
	}
	if !page.HasMore || page.NextCursor == nil || *page.NextCursor != 3 {
		t.Fatalf("expected next_cursor=3 and has_more=true, got %+v", page)
	}
}

func TestApplyPagination_OffsetBeyondTotal(t *testing.T) {
	items := []int{1, 2, 3}

	got, page := ApplyPagination(items, PaginationOptions{Offset: 10})
	if page == nil {
		t.Fatal("expected pagination info, got nil")
	}
	if len(got) != 0 {
		t.Fatalf("expected empty slice, got %v", got)
	}
	if page.Offset != 3 || page.Count != 0 || page.Total != 3 {
		t.Fatalf("unexpected pagination info: %+v", page)
	}
	if page.HasMore || page.NextCursor != nil {
		t.Fatalf("expected has_more=false and next_cursor nil, got %+v", page)
	}
}

func TestApplyPagination_NegativeOffset(t *testing.T) {
	items := []int{1, 2, 3}

	got, page := ApplyPagination(items, PaginationOptions{Limit: 1, Offset: -5})
	if page == nil {
		t.Fatal("expected pagination info, got nil")
	}
	if !reflect.DeepEqual(got, []int{1}) {
		t.Fatalf("expected first item, got %v", got)
	}
	if page.Offset != 0 {
		t.Fatalf("expected offset clamped to 0, got %d", page.Offset)
	}
}
