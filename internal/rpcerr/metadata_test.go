package rpcerr

import (
	"testing"

	"github.com/riipandi/tango/pkg/responder"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNormalizePagePinsTheSharedRules pins the RPC half of the REST
// pagination contract: an unset or zero limit takes the default rather
// than reaching a store as "no LIMIT", an oversized limit is clamped,
// and -1 in either field selects every record.
func TestNormalizePagePinsTheSharedRules(t *testing.T) {
	cases := []struct {
		name              string
		page, limit       int
		wantPage, wantLim int
	}{
		{"unset fields", 0, 0, 1, responder.DefaultPageSize},
		{"zero limit", 2, 0, 2, responder.DefaultPageSize},
		{"negative page", -5, 10, 1, 10},
		{"oversized limit", 1, 5000, 1, responder.MaxPageSize},
		{"all marker", -1, -1, -1, -1},
		{"all limit only", 1, -1, 1, -1},
		{"explicit page", 3, 25, 3, 25},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			page, limit := NormalizePage(tc.page, tc.limit)
			assert.Equal(t, tc.wantPage, page)
			assert.Equal(t, tc.wantLim, limit)
		})
	}
}

// TestListMetadataMatchesTheRestBlock pins the metadata shape against
// the REST metadata for the same input: zero-based inclusive item
// range, the same totals, and an unset range for an empty result.
func TestListMetadataMatchesTheRestBlock(t *testing.T) {
	ctx := t.Context()

	// Page 2 of 3 items, 2 per page: item 2, zero-based.
	meta := ListMetadata(ctx, 2, 2, 3)
	require.NotNil(t, meta)
	assert.Equal(t, int32(200), meta.GetStatusCode())
	assert.Equal(t, int32(2), meta.GetPage())
	assert.Equal(t, int32(2), meta.GetLimit())
	assert.Equal(t, int32(2), meta.GetTotalPages())
	assert.Equal(t, int32(3), meta.GetTotalItems())
	assert.Equal(t, int32(2), meta.GetFirstItemIndex())
	assert.Equal(t, int32(2), meta.GetLastItemIndex())

	// The same window through the REST helper must agree.
	rest := responder.NewPagination(responder.PaginationParams{Page: 2, Limit: 2}, 3)
	require.NotNil(t, rest.FirstItemIndex)
	assert.Equal(t, int(*rest.FirstItemIndex), int(meta.GetFirstItemIndex()))
	assert.Equal(t, int(*rest.LastItemIndex), int(meta.GetLastItemIndex()))
	assert.Equal(t, int(*rest.TotalPages), int(meta.GetTotalPages()))
	assert.Equal(t, int(*rest.TotalItems), int(meta.GetTotalItems()))

	// An empty result reports the totals but no item range.
	empty := ListMetadata(ctx, 1, 25, 0)
	require.NotNil(t, empty)
	assert.Equal(t, int32(0), empty.GetTotalItems())
	assert.Nil(t, empty.FirstItemIndex)
	assert.Nil(t, empty.LastItemIndex)

	// An all-marker request stays unpaged but still reports totals.
	all := ListMetadata(ctx, -1, -1, 4)
	require.NotNil(t, all)
	assert.Equal(t, int32(4), all.GetTotalItems())
	assert.Equal(t, int32(0), all.GetFirstItemIndex())
	assert.Equal(t, int32(3), all.GetLastItemIndex())
	assert.Nil(t, all.TotalPages)
}
