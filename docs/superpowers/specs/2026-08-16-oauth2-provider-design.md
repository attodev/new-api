# OAuth2 Provider for External App Integration — Design

## Context

new-api's playground UI is minimal and users want a full ChatGPT-style chat
experience instead. Rather than building that UI in-house, the plan is to
link out to an external chat app (OpenWebUI, planned for a near-total
rewrite/replacement on that side — its integration work is out of scope for
this spec).

The external app needs to authenticate its users against new-api and then
call new-api's OpenAI-compatible relay API on their behalf, with usage
correctly attributed and billed per user.

This spec covers **only the new-api side**: turning new-api into a minimal
OAuth2 authorization server so the external app's existing generic-OIDC login
support can be pointed at it.

## Goals

- External app can redirect a logged-in new-api user through a standard
  OAuth2 authorization-code flow and receive an access token.
- That access token behaves exactly like a normal `sk-` API token for
  billing, rate limiting, and logging, without appearing in the user's
  regular token list.
- Logging out of new-api immediately invalidates the external app's access.
- No new persistent DB rows for these tokens — validity is scoped to Redis
  with a TTL, mirroring how the token is actually used (validated across
  independent requests during a bounded session, not stored forever).

## Non-goals

- Changes to the external app (OpenWebUI) — assumed to already support
  generic OIDC login and to be reworked independently.
- Supporting multiple/dynamic OAuth2 clients. This is a single fixed,
  first-party client.
- A user consent screen. Since the only client is our own external app,
  authorization is auto-approved for any already-authenticated new-api user.
- PKCE. The client is a confidential, server-side client (OpenWebUI backend
  exchanges the code from its own server), so a `client_secret` is
  sufficient; PKCE exists for public clients that can't hold a secret.

## Decisions

| Question | Decision |
|---|---|
| Number of OAuth2 clients | Exactly one, fixed (no client registration/management UI) |
| Token TTL | 24 hours, independent of the 30-day new-api login session |
| Token permission scope | Same group as the user's own default group (`relayInfo.UsingGroup`), matching how the built-in Playground scopes its temporary token |
| `redirect_uri` configuration | Stored as an admin-editable system setting (not hardcoded), so ops can repoint it without a redeploy |
| Logout behavior | Logging out of new-api immediately revokes the OAuth2-issued token |
| Token exchange security | `client_secret` (system-setting-stored) + `state` param validation. No PKCE. |
| Token persistence | Redis-only, 24h TTL. No `Token` DB row is created. |

### Why Redis-only, no DB row

The playground's existing temp-token trick (see `controller/playground.go`)
works with a pure in-memory, never-persisted `Token` struct because it's
created and consumed within a single request's lifecycle. An OAuth2-issued
token doesn't have that luxury: the external app receives it once and then
uses it across many independent HTTP requests, possibly minutes or hours
apart. That requires the token to be verifiable by a later, separate
request — but that requirement is satisfied by anything shared across
requests, not specifically by DB durability.

`model.GetTokenByKey` already checks Redis first and only falls back to DB
on a cache miss. So an OAuth2 token can be written directly into that same
Redis cache format (`cacheSetToken`-equivalent) with a 24h TTL and skip the
DB insert entirely:

- No dangling DB rows to clean up or explicitly hide from the token list UI.
- Self-expires — no cron/sweep job needed.
- Trade-off: if Redis is flushed/restarted mid-session, the token silently
  becomes invalid and the external app must re-authenticate. Acceptable for
  a session-scoped credential; this deployment always runs Redis enabled
  (confirmed: token/user cache paths already assume Redis availability in
  production use).

## Architecture

### New routes (top-level, NOT under existing `/api/oauth/:provider`)

The existing `apiRouter.GET("/oauth/:provider", ...)` is a catch-all for
external login providers (GitHub, Discord, etc.) — mounting anything under
`/oauth/*` risks the catch-all swallowing it. The new provider endpoints use
a distinct `/oauth2` prefix:

- `GET /oauth2/authorize`
- `POST /oauth2/token`
- `GET /oauth2/userinfo`

### New files

- `setting/system_setting/oauth2_client.go` — config vars
  (`OAuth2Enabled`, `OAuth2ClientID`, `OAuth2ClientSecret`,
  `OAuth2RedirectURI`) plus admin get/update wiring, following the existing
  pattern in `setting/system_setting/oidc.go`.
- `model/oauth2_session.go` — Redis-backed helpers:
  - `IssueOAuth2Token(userId int) (*Token, error)` — builds a `Token{Name:
    fmt.Sprintf("oauth2-%s", system_setting.OAuth2ClientID), Group: <user's
    default group>}`, generates a key, caches it directly (bypassing DB
    insert), returns it. The fixed `Name` (derived from the configured
    `client_id`, e.g. `"oauth2-openwebui"`) is what shows up consistently in
    usage logs across re-issuances.
  - `RevokeOAuth2Token(userId int) error` — looks up the reverse index and
    deletes the cached token.
  - `storeAuthCode(code string, userId int) error` / `consumeAuthCode(code
    string) (int, error)` — single-use, 120s TTL, deleted on read.
- `controller/oauth2_provider.go` — the three HTTP handlers.
- Router wiring for the new group (in `router/api-router.go` or a sibling
  file, matching existing conventions).
- One additional call in `controller/user.go`'s existing `Logout()` to
  invoke `model.RevokeOAuth2Token(userId)`.

### Redis keys

| Key | Value | TTL | Purpose |
|---|---|---|---|
| `token:<hmac(key)>` | Token fields (existing format via `cacheSetToken`) | 24h | The actual bearer token, validated by the existing `TokenAuth()` path unchanged |
| `oauth2:code:<code>` | `userId` | 120s | Single-use authorization code, deleted on first read |
| `oauth2:session_token:<userId>` | raw token key | 24h | Reverse index so logout/re-authorization can find and invalidate the live token |

## Data flow

1. External app redirects the browser to `GET /oauth2/authorize?client_id=&redirect_uri=&response_type=code&state=`.
2. Handler validates `client_id` and `redirect_uri` against the configured
   system-setting values with **exact string match**. Mismatch → `400`,
   *no redirect* (redirecting on a bad `redirect_uri` is itself an open-redirect
   vulnerability).
3. If the requester has a valid new-api session cookie: generate a random
   auth code, store it (`oauth2:code:<code>` → `userId`, 120s TTL), redirect
   to `<redirect_uri>?code=<code>&state=<state>`.
4. If no valid session: redirect to the login page with a return-to
   parameter pointing back at this `/oauth2/authorize` request; resume from
   step 2 after login.
5. External app's backend calls `POST /oauth2/token` with `code`,
   `client_id`, `client_secret`, `grant_type=authorization_code`.
6. Handler validates `client_secret`, consumes the auth code (single use —
   deleted from Redis on read; reuse → `400 invalid_grant`), then calls
   `IssueOAuth2Token(userId)`. Before writing the new cache entry, it also
   overwrites `oauth2:session_token:<userId>`, invalidating any prior live
   OAuth2 token for that user so at most one stays valid at a time (avoids
   orphaned tokens accumulating from repeated logins).
7. Responds `{"access_token": "...", "token_type": "Bearer", "expires_in": 86400}`.
8. External app calls `GET /oauth2/userinfo` with `Authorization: Bearer
   <access_token>`. Handler resolves the token via the existing
   `GetTokenByKey` path, loads the associated user, returns `{"sub":
   ..., "email": ..., "name": ...}` for the external app's account
   auto-provisioning.
9. All subsequent chat/completions calls from the external app use this
   `access_token` exactly like a normal `sk-` token — no changes needed to
   `TokenAuth()`, `Distribute()`, billing, or logging. Logs show
   `token_name = "oauth2-<client_id>"` (e.g. `"oauth2-openwebui"`),
   consistently, across re-issued tokens.
10. On `Logout()`, the handler now also calls `RevokeOAuth2Token(userId)`,
    which reads `oauth2:session_token:<userId>` and deletes the
    corresponding `token:<hmac(key)>` cache entry — the external app's
    access stops working immediately.

## Error handling

| Condition | Response |
|---|---|
| `client_id` or `redirect_uri` mismatch at `/authorize` | `400`, no redirect |
| No valid session at `/authorize` | Redirect to login, then resume |
| Expired or already-used code at `/token` | `400 {"error": "invalid_grant"}` |
| Wrong `client_secret` at `/token` | `401 {"error": "invalid_client"}` |
| Invalid/expired bearer at `/userinfo` | `401` |
| OAuth2 feature disabled (`OAuth2Enabled=false`) | `404` on all three routes |

## Security notes

- `redirect_uri` is checked with exact string equality, not prefix or
  substring matching, to close the open-redirect vector.
- `client_secret` is generated server-side (not user-chosen) and stored via
  the existing system-setting admin flow; never logged.
- The auth code is single-use and short-lived (120s) — standard
  authorization-code-flow hygiene.
- Because tokens are Redis-only, a full Redis flush invalidates all live
  OAuth2 sessions at once (acceptable — same blast radius as any other
  Redis-dependent cache in this deployment).

## Testing

- Go table-driven tests for each handler:
  - `/authorize`: valid session, no session, `redirect_uri` mismatch,
    `client_id` mismatch, feature disabled.
  - `/token`: valid code, expired code, already-used code, wrong
    `client_secret`, malformed `grant_type`.
  - `/userinfo`: valid token, expired/invalid token.
- Logout integration test: issue a token, log out, confirm the token no
  longer authenticates.
- Re-authorization test: issue a token, re-run the flow before expiry,
  confirm the old token is invalidated and only the new one works.
- No OpenWebUI-side testing in scope — the three endpoints are validated
  standalone (curl / Go tests simulating the client role).
