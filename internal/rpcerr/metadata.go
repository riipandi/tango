package rpcerr

import (
	"math"

	commonv1 "github.com/riipandi/tango/codegen/proto/go/tango/common/v1"
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
		Page:           ToInt32(page),
		Limit:          ToInt32(limit),
		TotalPages:     ToInt32(totalPages),
		TotalItems:     ToInt32(total),
		FirstItemIndex: ToInt32(first),
		LastItemIndex:  ToInt32(last),
	}
}

// ToInt32 saturates a pagination value into the proto int32 range;
// counts beyond the cap are unreachable in practice but must not
// overflow the wire type.
func ToInt32(v int) int32 {
	switch {
	case v > math.MaxInt32:
		return math.MaxInt32
	case v < math.MinInt32:
		return math.MinInt32
	default:
		return int32(v)
	}
}
