# OAuth2 Provider for External App Integration — Design

## Context

new-api's playground UI is minimal and users want a full ChatGPT-style chat
experience instead. Rather than building that UI in-house, the plan is to
link out to an external chat app (OpenWebUI, planned for a near-total
rewrite/replacement on that side).

The external app needs to authenticate its users against new-api and then
call new-api's OpenAI-compatible relay API on their behalf, with usage
correctly attributed and billed per user.

This spec covers **the new-api side**: turning new-api into a minimal
OAuth2 provider. The corresponding contract for the OpenWebUI-side rewrite
is tracked separately in
`2026-08-16-oauth2-openwebui-integration-handoff.md`, along with its
follow-up review in
`2026-08-17-openwebui-response-to-newapi-oauth2-handoff.md`. This document
has been updated to match the flow finalized after that review; see
"Revision note" below.

## Revision note (2026-08-17)

The original draft assumed a classic **client-initiated** redirect flow:
the external app sends the browser to `GET /oauth2/authorize`, new-api
checks the session cookie, and redirects back with a code.

That doesn't work here: new-api's session cookie is `SameSite=Strict`
(`main.go:199-202`), so a top-level navigation arriving from another origin
(the external app) does not carry the new-api session cookie. The
"already logged in" check would incorrectly fail.

The design is now **IdP-initiated**: new-api itself mints the
authorization code from a page the user is already on (same-origin, cookie
present) and hands the browser a link straight to the external app's
callback, with the code already attached. There is no browser-facing
`GET /oauth2/authorize` redirect endpoint in this design — its role is
replaced by a same-origin, authenticated `POST /oauth2/session-init` call.

This section documents what changed from the original draft; the rest of
this document describes the current design directly (not as a diff).

## Goals

- A logged-in new-api user can click a link in new-api's own UI and land in
  the external app, already authenticated as themselves.
- The resulting access token behaves exactly like a normal `sk-` API token
  for billing, rate limiting, and logging, without appearing in the user's
  regular token list.
- Logging out of new-api immediately invalidates the external app's access.
- No new persistent DB rows for these tokens — validity is scoped to Redis
  with a TTL, matching how the token is actually used (validated across
  independent requests during a bounded session, not stored forever).

## Non-goals

- Changes to the external app's internals beyond what's specified in the
  handoff contract — that side is being rewritten independently.
- Supporting multiple/dynamic OAuth2 clients. This is a single fixed,
  first-party client.
- A user consent screen. Since the only client is our own external app,
  authorization is auto-approved for any already-authenticated new-api user.
- PKCE and refresh tokens. The client is a confidential, server-side client
  (the external app's backend exchanges the code from its own server), so a
  `client_secret` is sufficient; PKCE exists for public clients that can't
  hold a secret. There's no refresh token — expiry just requires the user
  to click the link again (see "Accepted risk" below for why this is
  intentional, not an oversight).

## Decisions

| Question | Decision |
|---|---|
| Flow initiation | new-api-initiated (IdP-initiated). No browser-facing `/oauth2/authorize` redirect. |
| Number of OAuth2 clients | Exactly one, fixed (no client registration/management UI) |
| Token TTL | 24 hours, independent of the 30-day new-api login session |
| Token permission scope | Same group as the user's own default group (`relayInfo.UsingGroup`), matching how the built-in Playground scopes its temporary token |
| `redirect_uri` (external app's callback URL) | Stored as an admin-editable system setting (not hardcoded), so ops can repoint it without a redeploy |
| Logout behavior | Logging out of new-api immediately revokes the OAuth2-issued token |
| Token exchange security | `client_secret` (system-setting-stored) validation at `/oauth2/token`. `state` is passed through opaquely, not validated by either side. No PKCE. |
| Token persistence | Redis-only, 24h TTL. No `Token` DB row is created. |
| `/oauth2/userinfo` role claim | Includes `is_admin: bool`, sourced from `model.IsAdmin(userId)` |
| Auth code consumption | Must be atomic (Redis `GETDEL`), not a separate GET-then-DEL — see Task-level detail in the implementation plan |

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

### Accepted risk: IdP-initiated "session swap"

Because new-api mints the code unsolicited (rather than in response to a
request the external app itself started and can validate `state` against),
a user who obtains their own valid link and sends it to someone else can
cause that person's browser to authenticate into the *sender's* external-app
account. This is not account takeover of the victim's own account — it's a
"walk into the wrong room" outcome, and it's the same structural trade-off
long-established IdP-initiated SSO flows (e.g. SAML IdP-initiated login)
accept industry-wide.

Mitigations in place:
- The code is single-use and short-lived (120s), so this only works within
  a narrow window right after the sender generates it.
- The external app is expected to show the authenticated account's email
  prominently right after landing, so an attentive recipient notices
  immediately if they ended up in the wrong account (external-app-side
  requirement, not enforced by new-api).

No `state`-based CSRF binding is possible here since the external app never
initiates the request — this is inherent to IdP-initiated flows, not a gap
to close.

## Architecture

### New routes

- `POST /oauth2/session-init` — same-origin only, requires a valid new-api
  session cookie. Called by new-api's own frontend (e.g. from the
  playground page), not by the external app.
- `POST /oauth2/token` — server-to-server, called by the external app's
  backend.
- `GET /oauth2/userinfo` — server-to-server, called by the external app's
  backend.

None of these live under the existing `/api/oauth/:provider` catch-all
(external login providers — GitHub, Discord, etc.); they use a distinct
`/oauth2` prefix to avoid any path collision.

### New files

- `setting/system_setting/oauth2_client.go` — config vars
  (`OAuth2Enabled`, `OAuth2ClientID`, `OAuth2ClientSecret`,
  `OAuth2RedirectURI`) plus admin get/update wiring, following the existing
  pattern in `setting/system_setting/oidc.go`. Values are read/written
  through the existing generic option-update mechanism
  (`config.GlobalConfig.Register("oauth2", ...)`, updated via keys like
  `oauth2.enabled`, `oauth2.client_id`, matching how `handleConfigUpdate` in
  `model/option.go` already dispatches dotted option keys to registered
  config structs) — no new admin endpoint is needed.
- `model/oauth2_session.go` — Redis-backed helpers (package `model`, so it
  can reuse the existing unexported token-cache primitives):
  - `IssueOAuth2Token(userId int, group string, clientId string) (*Token,
    error)` — builds a `Token{Id: -1, UserId: userId, Name:
    fmt.Sprintf("oauth2-%s", clientId), Group: group, Status:
    common.TokenStatusEnabled, UnlimitedQuota: true, ExpiredTime:
    common.GetTimestamp() + oauth2TokenTTLSeconds}`, generates a key via
    `common.GenerateKey()`, caches it directly into the same
    `token:<hmac(key)>` Redis format `cacheSetToken` uses but with a 24h TTL
    instead of the generic cache TTL (bypassing `Token.Insert()` entirely —
    no DB write). Also writes/overwrites the reverse-index key
    (`oauth2:session_token:<userId>`) so a prior live token for this user is
    invalidated.
  - `RevokeOAuth2Token(userId int) error` — reads the reverse-index key and
    deletes the corresponding `token:<hmac(key)>` cache entry.
  - `StoreAuthCode(code string, userId int) error` — `oauth2:code:<code>` →
    `userId`, 120s TTL.
  - `ConsumeAuthCode(code string) (int, error)` — atomic get-and-delete
    (Redis `GETDEL`, not a separate GET then DEL) so two near-simultaneous
    exchange attempts can't both succeed.
- `controller/oauth2_provider.go` — the three HTTP handlers.
- Router wiring for the new group (in `router/api-router.go` or a sibling
  file, matching existing conventions).
- One additional call in `controller/user.go`'s existing `Logout()` to
  invoke `model.RevokeOAuth2Token(userId)`.
- Frontend: a button/action in the playground UI that (1) calls
  `POST /oauth2/session-init` via `fetch` (same-origin, so the `Strict`
  session cookie is sent normally), (2) receives `{"redirect_url": "..."}"`,
  and (3) immediately navigates there via `window.location.href = ...`.
  **This must not be rendered as a static `<a href>`** — the URL must only
  ever exist transiently in the JS response handler, never as copyable
  markup, so it can't be shared, unfurled by chat-link-preview bots, or
  right-click-copied and reused outside the 120s window.

### Redis keys

| Key | Value | TTL | Purpose |
|---|---|---|---|
| `token:<hmac(key)>` | Token fields (existing format via `cacheSetToken`-equivalent) | 24h | The actual bearer token, validated by the existing `TokenAuth()` path unchanged |
| `oauth2:code:<code>` | `userId` | 120s | Single-use authorization code, deleted atomically on first read (`GETDEL`) |
| `oauth2:session_token:<userId>` | raw token key | 24h | Reverse index so logout/re-initiation can find and invalidate the live token |

## Data flow

1. User is on new-api's playground page, already logged in (session cookie
   present). They click "Open in Chat App".
2. Frontend calls `POST /oauth2/session-init` (same-origin fetch, cookie
   sent normally since this is not a cross-site request).
3. Handler reads the session (same check `authHelper` already does),
   generates a random auth code, stores it (`oauth2:code:<code>` →
   `userId`, 120s TTL), and responds:
   ```json
   { "redirect_url": "<OAuth2RedirectURI>?code=<code>&state=<random-state>" }
   ```
   If `OAuth2Enabled` is false or no valid session exists, responds `404` /
   `401` respectively — no code is generated.
4. Frontend immediately does `window.location.href = redirect_url`. Browser
   navigates to the external app's callback with the code attached.
5. External app's backend calls `POST /oauth2/token` with `code`,
   `client_id`, `client_secret`, `grant_type=authorization_code`.
6. Handler validates `client_secret` (constant-time compare), consumes the
   auth code via `ConsumeAuthCode` (atomic; a second call with the same code
   always returns "not found" → `400 invalid_grant`, never partially
   succeeds), then calls `IssueOAuth2Token(userId, userGroup, clientId)`.
7. Responds `{"access_token": "...", "token_type": "Bearer", "expires_in": 86400}`.
8. External app calls `GET /oauth2/userinfo` with `Authorization: Bearer
   <access_token>`. Handler resolves the token via the existing
   `GetTokenByKey` path, loads the associated user, returns:
   ```json
   { "sub": "...", "email": "...", "name": "...", "is_admin": true }
   ```
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
| No valid session at `/oauth2/session-init` | `401` |
| `/oauth2/session-init` called while `OAuth2Enabled=false` | `404` |
| Expired or already-used code at `/oauth2/token` | `400 {"error": "invalid_grant"}` |
| Wrong `client_secret` at `/oauth2/token` | `401 {"error": "invalid_client"}` |
| Invalid/expired bearer at `/oauth2/userinfo` | `401` |
| OAuth2 feature disabled (`OAuth2Enabled=false`) | `404` on all routes |

## Security notes

- `client_secret` is supplied by the admin directly via the existing
  system-setting admin flow (not generated server-side) — the admin
  generates it themselves with any sufficiently random value before sharing
  it with the external-app team out of band; never logged.
- The auth code is single-use (atomically consumed) and short-lived (120s).
- The external app's callback link is never rendered as static/copyable
  markup on the new-api side (see "New files" above) — this bounds, but does
  not eliminate, the accepted IdP-initiated session-swap risk described
  above.
- Because tokens are Redis-only, a full Redis flush invalidates all live
  OAuth2 sessions at once (acceptable — same blast radius as any other
  Redis-dependent cache in this deployment).

## Testing

- Go table-driven tests for each handler:
  - `/oauth2/session-init`: valid session, no session, feature disabled.
  - `/oauth2/token`: valid code, expired code, already-used code (verify
    exactly-once semantics under concurrent calls), wrong `client_secret`,
    malformed `grant_type`.
  - `/oauth2/userinfo`: valid token (including `is_admin` for both admin and
    non-admin users), expired/invalid token.
- Logout integration test: issue a token, log out, confirm the token no
  longer authenticates.
- Re-initiation test: issue a token, re-run the flow before expiry, confirm
  the old token is invalidated and only the new one works.
- No external-app-side testing in scope — the three endpoints are validated
  standalone (curl / Go tests simulating the external app's backend role).
