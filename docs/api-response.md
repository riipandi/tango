# API Response

Two transports, one contract: ConnectRPC serves the internal backoffice and
is the primary protocol; REST serves external integrations, and the
OAuth2/OIDC identity-provider features will land on it. The contract the two
share is semantic — field naming, the pagination rules, the error semantics —
and each transport writes it in its own idiom. The rules are defined once in
`pkg/responder` and both transports use them, so a client reads the same
page, limit, totals, and item range from either surface.

## Field naming

Every JSON field on both transports is **snake_case**.

REST gets this from the Go struct tags. ConnectRPC needs an explicit
step: protobuf's JSON mapping defaults to lowerCamelCase, so each
service registers a codec that serializes under the declared proto
field names instead. Without it the two transports of one contract
would disagree on field naming.

Requests are tolerant: protojson accepts both spellings, so a body may
send `display_name` or `displayName` and unmarshal the same way.

Three protocol surfaces keep camelCase because their specifications
require it, and none is part of this envelope contract:

- SCIM 2.0 (`/scim/v2/*`) — `userName`, `displayName`, `givenName`, `Resources`, `totalResults`.
- WebAuthn (`/api/webauthn/*`) — `publicKey`, `rp`, `user`, `challenge`.
- OIDC (planned) — its endpoints answer the shapes the specification
  defines, not the envelope.

## Where each fact lives

| Fact                | ConnectRPC (internal, primary)                        | REST (external)                          |
| ------------------- | ----------------------------------------------------- | ---------------------------------------- |
| Status              | the connect code (`ok` is the absence of an error)    | `status` + `metadata.status_code`        |
| Error message       | the connect error message                             | `message` in the error envelope          |
| Structured error    | a typed message attached as a connect error detail    | `error` in the error envelope            |
| Request/trace id    | the `X-Request-Id` response header                    | `metadata.request_id` + the same header  |
| Rate limit          | `X-RateLimit-*` headers; a limited call is refused with `resource_exhausted` (429) | `metadata.rate_limit` + the same headers; a limited request is refused with the envelope |
| Pagination          | a `tango.common.v1.ListMetadata` block on the message | `metadata` pagination fields             |
| Payload             | the response message's own typed fields               | `data` in the envelope                   |
| Links (HATEOAS)     | not modelled; a page token when a list needs one      | the `links` map                          |

Envelope-in-body is right where the fact is domain (pagination) and an
anti-pattern where the protocol already carries it: a Connect client raises
on an error, so an error envelope inside an RPC body is unreadable exactly
where it would matter. The shared block the REST envelope writes and the RPC
metadata block used to mirror is why `common.proto` once carried
`status_code` and `request_id`; it carries the pagination block alone now.

## REST envelope

Minimal success response:

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

## ConnectRPC surface

A procedure answers its response message alone: typed payload fields, and the
`tango.common.v1.ListMetadata` block on a list message. The facts around the
payload travel with the protocol, and a generated client reads them without
parsing a body:

- **Status** is the connect code: `ok` on success, `unavailable` for a
  dependency that is down, `unimplemented` for an unknown procedure.
- **The correlation id** is the `X-Request-Id` response header the request
  middleware writes on every answer — success, failure, and the not-found
  boundary — before any handler runs. No handler or interceptor writes it
  again: connect merges handler-set headers by appending, so a second writer
  answers with the id twice.
- **Rate-limit state** is the `X-RateLimit-*` response headers the limiter
  middleware writes. A limited call is refused with `resource_exhausted` —
  the code the specification maps to 429 — and a `Retry-After` header, never
  the REST envelope. The health procedure is not throttled, the same as its
  REST twin.
- **A structured failure** is a message attached to the connect error's
  details, the typed counterpart of the REST envelope's `error` field.
- **Request validation** is declarative: the constraints live in the
  contracts as protovalidate options (`buf.validate.field`), and the validate
  interceptor enforces them before a handler runs. A refusal is
  `invalid_argument` with a `buf.validate.ErrorInfo` detail — the field
  paths and rule descriptions a client renders per field. A rule a contract
  cannot express (a uniqueness check, a token's existence) is domain logic
  and answers its own code from the feature; the REST surface keeps
  `pkg/validate`'s field errors in the envelope.
