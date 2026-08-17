# OpenWebUI-Side Feedback & Requests on the OAuth2 Integration Contract

> Response to `2026-08-16-oauth2-openwebui-integration-handoff.md`, written
> during implementation-planning review on the OpenWebUI side. Organized so
> each item can be tracked independently: confirmations, schema changes,
> a link-handling requirement, and a set of foundational assumptions we'd
> like explicitly reconfirmed before we build against them.

## 1. Confirmed — proceeding as specified

- New callback route `GET /auth/newapi/callback?code=&state=`, IdP-initiated
  (new-api starts the hand-off from a page the user is already on). We are
  **not** wiring this through OpenWebUI's generic-OIDC provider settings —
  agreed, that flow assumes OpenWebUI initiates the redirect, which doesn't
  apply here.
- Server-to-server `POST /oauth2/token` (authorization_code grant) and
  `GET /oauth2/userinfo` — implementing exactly per your spec, including the
  `invalid_grant` / `invalid_client` error shapes.
- No PKCE, no refresh token, no consent screen — confirmed, none needed on
  our side either.
- `state` is passed through opaquely and not validated by us. We understand
  and accept this as an inherent trade-off of an IdP-initiated flow: if an
  attacker gets a victim to click a link containing the *attacker's own*
  valid `code`, the victim's browser ends up logged into the attacker's own
  new-api-linked OpenWebUI account — not a takeover of the victim's existing
  account, just an unwanted "session swap." Please confirm you're comfortable
  accepting this as-is rather than adding further mitigation on either side.

## 2. Schema change requested

### 2.1 Add an admin/role claim to `/oauth2/userinfo`

Current response:

```json
{ "sub": "...", "email": "...", "name": "..." }
```

We'd like one more field so OpenWebUI can auto-sync the admin role instead
of relying only on a manual "first SSO login becomes admin" bootstrap:

```json
{ "sub": "...", "email": "...", "name": "...", "is_admin": true }
```

A generic `role` string instead of a boolean is also fine if that fits your
model better — we just need something we can map to OpenWebUI's
`admin` / `user` role.

**Not blocking for an initial rollout.** Until this ships, OpenWebUI will
rely on its existing "first SSO login becomes admin" bootstrap, with any
further admin promotions handled manually and out of band. We do need this
before we can fully remove the admin user-management UI on our side, though.

## 3. Link-handling requirement

The authorization `code` your side issues is single-use with a 120s TTL (as
documented). Please make sure the "Open in Chat App" entry point is a
same-tab click / JS-triggered navigation, **not a plain copyable or
shareable URL**. If that link is ever pasted into a tool with link-unfurling
(Slack, Discord, etc.), the unfurl bot will fetch it server-side and burn
the code before the real user's browser does — producing a confusing
"code already used" failure on the user's first real click.

## 4. Please reconfirm — our design depends on these being exactly true

1. **Code semantics**: 120s TTL, strictly single-use; a second use of the
   same code always returns `invalid_grant`, never partially succeeds.
2. **Access token lifetime and logout behavior**: `access_token` expires in
   24h. We understand this was intentionally shortened from new-api's
   default 30-day session lifetime specifically to bound the exposure
   window for a user who closes the tab without explicitly logging out —
   confirming our shared understanding of *why* 24h was chosen, not asking
   for a change. Separately: logging out of new-api invalidates the token
   **immediately**, not just at the 24h natural expiry. This immediate
   invalidation is what lets an OpenWebUI chat session go dead on its very
   next request after a new-api logout, without OpenWebUI needing a
   separate logout webhook or push notification from your side.
3. **Billing equivalence**: the `access_token` returned from
   `/oauth2/token` is the *same* credential (or maps 1:1 to the same
   underlying token/quota) that new-api's OpenAI-compatible relay endpoints
   (`/v1/chat/completions`, `/v1/models`) accept as
   `Authorization: Bearer`, and usage against those endpoints is attributed
   to that specific user's own account/quota — not a shared system-level
   key. Our entire per-user billing/cost-attribution design depends on this
   being true exactly as stated in the original handoff doc.

## 5. Values we still need before implementation

(Restating from your original doc, for tracking on our end.)

- [ ] `client_id`
- [ ] `client_secret`
- [ ] new-api base URL for `/oauth2/token`, `/oauth2/userinfo`, and the
      relay endpoints
- [ ] Exact callback URL to register as your `redirect_uri`:
      `https://<openwebui-host>/auth/newapi/callback` — final hostname to
      be confirmed once our deployment domain is finalized; we'll provide
      the exact value before go-live.

---

Prepared by the OpenWebUI-side integration team, 2026-08-17. Happy to walk
through any of this live if that's easier than iterating over documents.
