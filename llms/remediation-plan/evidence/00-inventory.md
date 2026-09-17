# Phase 0 — Final-Shape Violation Inventory (Task 0.2)

Input for Phase 1+. Nothing here is fixed in this task. Sources: code search at commit
`cacaa6355c1147a2d2b3712595684947c72f941a`, `llms/endpoint-reference.md`, Yaak workspace
(`app.yaak.desktop/db.sqlite`).

| # | Finding | Owner | Source | Required final shape | Migration impact | Test impact | Yaak impact |
| --- | --- | --- | --- | --- | --- | --- | --- |
| 1 | `oidc_clients.secret` still written/read as mirror of the first credentials entry | federation/oidc | `modules/federation/oidc/store_client.go` (`AddClientSecret` mirrors first entry, `DeleteClientSecret` clears column, `clientColumns` selects `c.secret`) | Single storage shape: credentials JSONB only; no mirror column | Task 1.2 drop column | Store + authz-code tests must not depend on mirror | None (secret flows unchanged on the wire) |
| 2 | `LegacySecretID` synthetic entry + legacy fallback comparison | federation/oidc | `store_client.go:74`, `scanClient` synthesizes `{ID: "legacy"}` entry when credentials JSONB empty; `token.go secretMatches` falls back to `client.SecretHash` | Verification reads credentials list only; no synthetic entry, no fallback | Task 1.1 removes code path; Task 1.2 drops column | New tests: create, multi-secret, expiry, inactive, rotation, deletion, auth after column gone | None |
| 3 | `oidc_clients.image_type` / `dark_image_type` columns still read/written | federation/oidc, admin/apiaccess | `store_client.go` (`clientColumns`, `scanClient`, logo update), `handler_meta.go` (`has_logo` derives from `ImageType`), `modules/admin/apiaccess/store.go:233` (`image_type IS NOT NULL` projections) | `logo_path` is the only logo storage; meta views derive presence from it | Task 1.2 drop both columns + contract test | Meta/apiaccess projections tests updated | None (responses keep `has_logo` shape) |
| 4 | `oidc_refresh_tokens` table has no runtime caller | federation/oidc, database | `database/migrations/00004_create_federation_tables.sql`; only references: `database/migrator_test.go:83`, `database/schema_fresh_test.go:28`. Runtime refresh tokens live in `oauth2_sessions` (`store_token.go`) | Table + index dropped | Task 1.3 new migration; migrator count/assertions + schema contract + backup tests updated | Migrator/schema tests updated | None |
| 5 | Application Images Yaak folder exists for an excluded feature | yaak artifacts | `app.yaak.desktop/db.sqlite` folder `fl_3VCPmqfLP6` ("Application Images"), 0 requests | Folder removed; no Application Images requests | None | None | Task 2.x cleanup |
| 6 | Stale "planned (phase 8)" request description | yaak artifacts | Yaak request "Refresh client metadata document" (`POST …/clients/{id}/refresh` description) | Description states the implemented contract, no phase marker | None | None | Task 2.x cleanup |
| 7 | `pkg/responder` imported in service/area files | federation/oidc, federation/discovery | `modules/federation/oidc/{authorize,token,device,userinfo,par}.go`, `modules/federation/discovery/schema.go` (module `module.go` files that host handlers are not violations) | responder used only in handler files; area files return errors | None | None | None |
| 8 | Ad-hoc request validation bypasses `pkg/validate` | identity, federation/oidc | `modules/identity/webauthn/handler.go:94` ("session_id is required"), `modules/identity/onetimeaccess/handler.go:209` ("token is required"), `modules/federation/oidc/device.go` (manual `PostFormValue` guards), `modules/identity/signup/handler.go:163` (manual TTL parse) | Code-first `Validate()` methods; 422 via responder only | None | Validation tests per endpoint | None |
| 9 | Public responses leak `err.Error()` internals | identity, admin, federation | 24+ `responder.Fail(..., err.Error())` sites: `signup/handler.go:228-239`, `devicelogin/handler.go:105,125`, `scimsync/handler.go:150` (`WithError`), `user/handler.go:28-30`, `usergroup/handler.go:24,244`, `apiaccess/handler.go:21-23`, `apikey/handler.go:22`, `customclaim/handler.go:24`, `totp/handler.go:280`, `webhook/handler.go:24-26`, `account/handler.go:201`, `recovery/recovery.go:274`, `oidc/handler.go:201,289` | Stable public messages; error taxonomy mapped to status + fixed text | None | Error-message assertions per surface | None |
| 10 | Missing live evidence per endpoint row | verification | `llms/endpoint-reference.md` Evidence column cites unit tests only; no per-row live Yaak evidence registry; tango server was not running at baseline | Every row carries live Yaak evidence against the running server | None | None | Task 5.x live sweep |

## Sequencing dependencies

- Finding 1+2 block Task 1.1 (code) → Task 1.2 (column drop).
- Finding 4 gates Task 1.3.
- Findings 3 and 1.2 share the migration.
- Findings 7–9 belong to Phase 3/4 scopes; 5–6 to Phase 2; 10 to Phase 5.
