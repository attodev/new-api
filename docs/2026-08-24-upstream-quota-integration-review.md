# Upstream quota integration review and follow-up notes

Date: 2026-08-24

This document records the upstream synchronization, compatibility review,
follow-up fixes, validation, and deferred work completed on
`codex/upstream-token-quota-reserve` before merging it into `team`.

## Scope

The work reviewed changes recently brought from upstream together with the
local organization billing, tiered retry, asynchronous task settlement, and
Claude relay changes. The main goals were to prevent concurrent quota
overspending, keep Redis and batched database accounting consistent, and make
organization settlement use the organization selected when a request started.

## Commits merged

| Commit | Summary | Upstream or related commit |
| --- | --- | --- |
| `15834520f` | Atomic token quota reservation | [upstream `ccd535ef8`](https://github.com/QuantumNous/new-api/commit/ccd535ef8) |
| `f9b1d1f50` | Organization wallet adjustment during tiered retry reservation | Related to local tiered retry billing |
| `8e242b25f` | Organization snapshot for asynchronous task settlement and refunds | Related to organization wallet billing |
| `30559b2b8` | Restore inline PDF/text file handling in the Claude relay | [upstream `263b9bc`](https://github.com/QuantumNous/new-api/commit/263b9bc695af1e557ed5a20ba910d682025cb0b1) |
| `ba6afab57` | Atomic user wallet reservation | [upstream `ccd535ef8`](https://github.com/QuantumNous/new-api/commit/ccd535ef8), related to `15834520f` |
| `3cfc88575` | Synchronous token quota cache deltas | Related to `15834520f` |
| `5e6c6a716` | Synchronous user quota cache deltas | Related to `ba6afab57` |
| `9d0fa91fd` | Atomic organization wallet reservation and pinned organization settlement | Related to `0ac72dcf7`, `f9b1d1f50`, and `8e242b25f` |
| `f1dd2da1e` | Track queued and in-flight quota batches and retry failed writes | Related to the atomic user/token reservation changes |
| `117fec391` | Preserve live quota while hydrating user cache metadata | Related to `ba6afab57` and `5e6c6a716` |
| `10ade7045` | Serialize cache hydration with local batch reservation state | Related to `15834520f`, `ba6afab57`, `5e6c6a716`, `f1dd2da1e`, and `117fec391` |
| `552ec4d14` | Harden tiered retry group switching | [upstream `e5aa3f5fd`](https://github.com/QuantumNous/new-api/commit/e5aa3f5fd), related to `f9b1d1f50` |

Each port or follow-up commit contains its upstream link and/or
`Related-Commit` trailers in the commit message for later tracing.

## Resulting behavior

- User and limited-token pre-consumption use atomic check-and-decrement paths.
- Explicit token settlement deltas update both remaining and used quota.
- Redis quota updates are visible before the adjustment call returns.
- Queued and in-flight local batch deltas prevent stale database fallback on a
  single node.
- Failed user/token quota batch writes are returned to the queue.
- Cache hydration does not overwrite a live quota balance with an older
  database snapshot.
- Organization member quota and the shared organization wallet are reserved
  atomically at their respective balance boundaries.
- Organization settlement and asynchronous task refunds remain bound to the
  organization captured when billing started.
- Tiered retries refresh the selected group before reserving additional quota,
  preserve used-channel history, and clear stale free-model state when moving
  from a free group to a paid group.
- Claude inline file conversion supports PDF documents and text files and skips
  unsupported file types without emitting malformed content.

## Validation performed

- Full `model`, `service`, `controller`, and Claude relay package tests passed.
- Focused user/token/organization quota concurrency tests passed with the Go
  race detector.
- Tiered group-switch regression tests passed.
- `go vet` passed for the changed model, service, controller, and Claude relay
  packages.
- `git diff --check` passed and the feature worktree was clean before this
  document was added.
- `go test ./...` ran all discovered packages successfully except the root
  package setup, which still cannot compile because `web/classic/dist` is not
  present. This is an existing workspace artifact issue, not a failure caused
  by these changes.

## Operational assumption for this merge

The deployment is currently single-node and uses Redis with batch updates.
Under that assumption, the implemented local pending/in-flight protection is
functionally covered by the regression tests above.

## Deferred TODO

These items were intentionally deferred because the current deployment is
single-node. They should be completed before scaling out or enabling other
cache configurations.

1. Replace the per-type global batch mutexes with per-ID or striped locking.
   The current user and token locks are held across Redis Lua calls, so unrelated
   users or tokens are serialized and a slow Redis call can create a lock
   convoy.
2. Move pending/in-flight quota versioning to shared Redis state before running
   multiple application nodes. The current maps and mutexes are process-local;
   another node cannot see a delta waiting to be flushed by this node and could
   hydrate a deleted cache entry from a stale database snapshot.
3. Define and enforce the supported behavior for
   `BATCH_UPDATE_ENABLED=true` without Redis. Safe options are to reject this
   configuration at startup or write user/token quota changes directly to the
   database when Redis is unavailable.
4. Generate `.understand-anything/knowledge-graph.json` with `/understand` if a
   graph-based blast-radius overlay is desired for a future review. The graph
   was not present during this review, so dependency tracing was performed
   manually.
5. Restore or build `web/classic/dist` when a completely green root-package
   `go test ./...` result is required.

## Repository state note

The feature branch was committed locally. An earlier attempt to push it was
not performed because the external `origin` destination required explicit
ownership confirmation. This document and the feature commits are intended to
be merged locally into `team`; pushing `team` remains a separate, explicit
operation.
