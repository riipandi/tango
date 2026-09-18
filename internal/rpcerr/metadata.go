package rpcerr

import (
	"math"

	commonv1 "github.com/riipandi/tango/gen/proto/go/tango/common/v1"
)

// PageMetadata builds the shared pagination block for list RPCs.
// Callers decide when a page is "unpaged" and skip the metadata.
func PageMetadata(page, limit, total int) *commonv1.PageMetadata {
	totalPages := (total + limit - 1) / limit
	first := (page-1)*limit + 1
	last := min(page*limit, total)
	if first > total {
		first = 0
	}
	return &commonv1.PageMetadata{
		Page:           toInt32(page),
		Limit:          toInt32(limit),
		TotalPages:     toInt32(totalPages),
		TotalItems:     toInt32(total),
		FirstItemIndex: toInt32(first),
		LastItemIndex:  toInt32(last),
	}
}

// toInt32 saturates a pagination value into the proto int32 range;
// counts beyond the cap are unreachable in practice but must not
// overflow the wire type.
func toInt32(v int) int32 {
	switch {
	case v > math.MaxInt32:
		return math.MaxInt32
	case v < math.MinInt32:
		return math.MinInt32
	default:
		return int32(v)
	}
}
