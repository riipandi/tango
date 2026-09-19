# API Response

Two transports, one contract: REST answers the envelope below, and
ConnectRPC answers the same metadata block under protobuf field names.
The rules are defined once in `pkg/responder` and both transports use
them, so a client reads the same page, limit, totals, and item range
from either surface.

## Field naming

Every JSON field on both transports is **snake_case**.

REST gets this from the Go struct tags. ConnectRPC needs an explicit
step: protobuf's JSON mapping defaults to lowerCamelCase, so each
service registers a codec that serializes under the declared proto
field names instead. Without it the two transports of one contract
would disagree on field naming.

Requests are tolerant: protojson accepts both spellings, so a body may
send `display_name` or `displayName` and unmarshal the same way.

Two protocol surfaces keep camelCase because their specifications
require it, and neither is part of this envelope contract:

- SCIM 2.0 (`/scim/v2/*`) — `userName`, `displayName`, `givenName`, `Resources`, `totalResults`.
- WebAuthn (`/api/webauthn/*`) — `publicKey`, `rp`, `user`, `challenge`.

| REST envelope              | ConnectRPC                                                          |
| -------------------------- | ------------------------------------------------------------------- |
| `status`                   | the Connect error code (`ok` is the absence of an error)            |
| `message`                  | the Connect error message                                           |
| `data`                     | the response message's own payload field (`users`, `api_keys`, ...) |
| `metadata.status_code`     | `metadata.status_code`                                              |
| `metadata.request_id`      | `metadata.request_id`                                               |
| `metadata.rate_limit`      | `metadata.rate_limit`                                               |
| `metadata.page`            | `metadata.page`                                                     |
| `metadata.last_item_index` | `metadata.last_item_index`                                          |
| `links`                    | not modelled yet; RPCs that need it add the field explicitly        |

Minimal sucess response:

```json
{
    status: success
    message?: string
    metadata: {
        status_code: number (http code)
        request_id: string
    }
}
```

Complete success response:

```json
{
    status: success
    message?: string
    data?: {} | []
    metadata: {
        status_code: number (http code)
        request_id: string
        trace_id?: string

        // Rate limit information
        rate_limit?: ApiRateLimit

        // Pagination information (HATEOAS-first)
        ...ApiPagination
    }

    // Simple HATEOAS-friendly link map.
    // Keys are link relations (e.g. "self", "next", "prev", "related") and values are URLs or null.
    links? Record<string, string | null> | null
}
```

ApiPagination:

```json
{
    page?: number | null // current page number
    limit?: number | null // number of items per page

    // Totals (optional when unknown or not applicable)
    total_pages?: number | null
    total_items?: number | null

    // Inclusive item range in the current page (optional)
    first_item_index?: number | null
    last_item_index?: number | null
}
```

PaginationParams (query params):

```json
{
    page?: number // number of the page to retrieve, set -1 for all records
    limit?: number // number of items per page, set -1 for all records
    sort_by?: string // sort by column or field name
    sort_order?: 'asc' | 'desc' // sort direction
}
```

Pagination rules (enforced once in `pkg/responder`, shared by REST and
ConnectRPC):

- `page` starts at 1; a value below 1 becomes 1.
- `limit` defaults to 25 and caps at 100.
- `-1` in either field returns every record.
- On the REST query surface an empty `page`/`limit` takes the default; a
  present but invalid value is rejected. On the ConnectRPC surface a proto3
  `int32` cannot express "unset" separately from zero, so `0` takes the
  default rather than returning an unbounded result.
- `first_item_index` and `last_item_index` are zero-based and inclusive.
  Both are omitted when the range is unknown (empty result, or every record
  requested with no rows).
- `sort_by`/`sort_order` are parsed by `ParsePagination` but no store applies
  them yet; each list keeps its own fixed order.

Error response:

```json
{
    status: error
    message: string (general error message)
    error?: {} | [] | null
    metadata: {
        status_code: number (http code)
        request_id: string
    }
}
```

Links:

```json
{
    links?: {
        self?: string | null
        next?: string | null
        prev?: string | null
        first?: string | null
        last?: string | null
        [rel: string]: string | null | undefined
    } | null
}
```

ApiRateLimit:

```json
{
    limit: number // maximum number of requests allowed in the current window
    remaining: number // number of requests left in the current window
    reset: number // unix timestamp when the limit resets
}
```
