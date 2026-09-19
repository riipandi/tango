package rpcerr

import (
	"context"
	"math"

	commonv1 "github.com/riipandi/tango/codegen/proto/go/tango/common/v1"
	"github.com/riipandi/tango/pkg/responder"
)

// NormalizePage applies the shared pagination rules (pkg/responder) to
// raw proto fields: a page below 1 becomes the first page, a limit below
// 1 takes the default rather than returning an unbounded result, a limit
// above the cap is clamped, and -1 in either field selects every record.
// Every list handler routes through it, so an unset limit can no longer
// reach a store as "no LIMIT".
func NormalizePage(page, limit int) (int, int) {
	return responder.NormalizePage(page, limit, responder.DefaultPageSize, responder.MaxPageSize)
}

// ResponseMetadata mirrors the REST envelope metadata block for one RPC
// response. The status code is the transport status the Connect error
// mapping produces, and the request id is the correlation id the
// transport middleware assigned.
func ResponseMetadata(ctx context.Context, statusCode int) *commonv1.ResponseMetadata {
	return &commonv1.ResponseMetadata{
		StatusCode: int32Ptr(statusCode),
		RequestId:  strPtr(responder.RequestIDFromContext(ctx)),
	}
}

// ListMetadata builds the envelope metadata for a list response: the
// transport status, the request id, and the pagination block under the
// same rules and zero-based item range as the REST metadata.
func ListMetadata(ctx context.Context, page, limit, total int) *commonv1.ResponseMetadata {
	meta := ResponseMetadata(ctx, 200)
	if limit != responder.PaginationAll && limit < 1 {
		return meta
	}
	meta.Page = int32Ptr(page)
	meta.Limit = int32Ptr(limit)
	meta.TotalItems = int32Ptr(total)
	if !responder.All(page, limit) {
		meta.TotalPages = int32Ptr((total + limit - 1) / limit)
	}
	if first, last, ok := responder.ItemRange(page, limit, total); ok {
		meta.FirstItemIndex = int32Ptr(first)
		meta.LastItemIndex = int32Ptr(last)
	}
	return meta
}

// WithPagination adds the shared pagination block to response metadata
// built by ResponseMetadata.
func WithPagination(meta *commonv1.ResponseMetadata, page, limit, total int) *commonv1.ResponseMetadata {
	if meta == nil {
		meta = &commonv1.ResponseMetadata{}
	}
	if limit != responder.PaginationAll && limit < 1 {
		return meta
	}
	meta.Page = int32Ptr(page)
	meta.Limit = int32Ptr(limit)
	meta.TotalItems = int32Ptr(total)
	if !responder.All(page, limit) {
		meta.TotalPages = int32Ptr((total + limit - 1) / limit)
	}
	if first, last, ok := responder.ItemRange(page, limit, total); ok {
		meta.FirstItemIndex = int32Ptr(first)
		meta.LastItemIndex = int32Ptr(last)
	}
	return meta
}

// int32Ptr returns a pointer to a saturated int32.
func int32Ptr(v int) *int32 {
	saturated := ToInt32(v)
	return &saturated
}

// strPtr returns a pointer to a string, or nil when it is empty so the
// JSON mapping omits it the way the REST envelope omits an empty id.
func strPtr(v string) *string {
	if v == "" {
		return nil
	}
	return &v
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
