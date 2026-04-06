# LFX UI Persona Service

## Overview

The Persona Service is a decoupled microservice component of the LFX UI layer.
It provides a personalized, fast summary of a user's involvement and status
across Linux Foundation projects and foundations, for the purpose of UI/UX
feature enablement and navigation.

### What this is not

This is **not** a v2 entity/resource API service. It does not define or enforce
access control. The name "Persona" is chosen deliberately over "Role" to avoid
any ambiguity with authorization concepts: personas are about *presenting*
relevant context to the user, not *gating* access.

### What this is

A fast, user-centric aggregation layer. It accelerates, pre-loads, or provides
privileged proxy access to data about a user's involvement or status across
multiple backend systems, organized into a format optimized for UI consumption.

Because this service's primary purpose is to reduce UI churn and latency rather
than expose a stable business API, it is structured as a **NATS RPC endpoint**
rather than a REST API following v2 idioms. Ownership sits with the UI team;
this is not intended to become a "core service" (contrast: User Service `/me`).

## Personas

Personas are **not a singleton** per user. A user may have more than one
persona, and personas may fan out across multiple foundations and/or projects
beneath them.

The personas described below are navigation-centric: they represent a user's
most relevant entry points into the LFX platform.

### Board Member

Determined by membership in a committee whose `category` is `"Board"` (the
exact enum value used by the Committee Service and propagated to indexed
documents).

#### Detection strategy

The `committee_member` OpenSearch document carries a
`committee_category:<value>` tag (e.g. `committee_category:Board`) inherited
from its parent committee at index time. This tag is confirmed present in the
indexed schema; no additional propagation work is needed.

Identity matching presents a complication: the `data.username` field on
`committee_member` records is not reliably populated (it may be an empty
string), while `data.email` is generally present. To maximise recall, the
Persona Service issues **two queries in parallel** against the query service
and merges the results by `committee_member_uid`:

1. **Email match** — filter `object_type:committee_member` +
   `committee_category:Board` + `email:<user-email>` (tag term lookup).
2. **Username match** — filter `object_type:committee_member` +
   `committee_category:Board` + `data.username:<username>` (structured field
   filter, skipped if the caller's username is empty).

Results from both legs are de-duplicated by `id` (the `committee_member` UUID
exposed as `Resource.id` in the Query Service response) before returning to
the caller.

#### Local post-filter for username results

Because the Query Service `filters` parameter issues a term clause against
`data.username` (prefixed internally as `data.username:<value>`), the match
may be overly liberal depending on analyzer behavior. After receiving results
from the username leg, the Persona Service **must perform an exact local
filter**, discarding any records where `data.username` does not exactly equal
the requested username (case-insensitive). The email leg does not require this
treatment because the `email:<value>` tag lookup is an exact term match
against a structured tag value.

#### What is returned

The Query Service returns `Resource` objects with shape `{ type, id, data }`,
where `data` is the raw committee member snapshot. For each de-duplicated
result the Persona Service extracts and returns a stub containing:

- `data.committee_uid` and `data.committee_name` — for UI navigation.
- `data.project_uid` and `data.project_slug` (from the committee's tags,
  denormalised onto the member at index time) — for project-scoped routing.
- `id` (the committee member UUID) — for deep-linking.
- `data.role.name` and `data.voting.status` — informational; the UI decides
  how to present or gate based on these values.

The Persona Service does **not** make access-control decisions based on role
or voting status; it surfaces the data and defers gating entirely to the UI.

#### Query Service API calls

Both legs call `GET /query/resources?v=1` on the Query Service. The existing
API surface is sufficient — no new endpoints or schema changes are required.

**Email leg:**

```
type=committee_member
tags_all=committee_category:Board, email:<user-email>
```

**Username leg** (skipped when username is empty):

```
type=committee_member
tags_all=committee_category:Board
filters=username:<username>
```

The `tags_all` parameter performs an AND match across all supplied tag values,
ensuring only Board-category members are returned. The `filters` parameter
issues a term clause on `data.username`. Results are then locally post-filtered
for exact username equality before merging with the email leg results.

### Executive Director (ED)

Determined by the ED field on the project object.

**TBD:** Add ED as a denormalized username/name/email field to the v2 project
model (same pattern as writers/auditors), and decide between:

- Bidirectional sync with v1, or
- Same model as ITX.

### Maintainer

Determined by CDP data.

**TBD:** Evaluate reuse of the same API used for the affiliations screens.
Consider introducing a caching layer in front of the CDP call.

### Contributor

The "default view." Also filtered by projects the user actually has some
involvement in. It is intentionally out of scope for this service to define
whether contributor status is a hard gate or a "promoted / recommended"
navigation hint — this service is not access control.

Sources that may indicate contributor status:

- CDP (maintainer, contributor, or any activity)
- Access control membership (writer/auditor)
- Committee membership
- ITX activity (meetings, mailing lists)

## Data flow

```mermaid
graph LR
  UI_SSR["UI SSR"] -->|NATS| PS["Persona Service"]

  PS -->|"API call (local-ephemeral-cache-deferred)"| CDP
  PS -->|Board group lookups| QS["Query Service"]
  PS -->|"TBD: ED lookup, if implementing v1 sync into Project Service"| QS

  PS --> JR[("Persona Service\ndurable cache\njob-results DB")]

  ITX -->|"Persona Service worker async polling"| JR
  v1_DB["v1 DB"] -->|"TBD: async polling for v1 EDs\ninstead of implementing via realtime sync"| JR
```

## Open questions

- **`/me` service:** David raised the question of whether a consolidated `/me`
  service is needed to report current roles. The current framing treats this
  more as a UI component: aggregating data from multiple systems, organizing it
  for UI consumption, and ensuring performance is a "UI churn" activity, not a
  "business API." A NATS RPC endpoint (rather than a REST API) reflects this
  distinction.

- **ED sync strategy:** Decide between implementing a bidirectional v1↔v2 sync
  for ED data versus polling v1 DB asynchronously via the job-results DB
  pattern.

- **Contributor gating:** Clarify whether "contributor" is a hard gate or a
  softer "promoted navigation" hint. This is likely outside the scope of this
  service.

- **Query Service committee filtering:** Define what surface area Query Service
  needs to expose to support "does user X have relationship Y to object Z?"
  without leaking the `committee-member` pseudotype.
