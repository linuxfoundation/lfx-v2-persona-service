# LFX v2 Persona Service

The Persona Service is a microservice in the LFX V2 platform. It aggregates a user's involvement across Linux Foundation projects and foundations into a single, UI-friendly response.

The primary consumer is the [LFX Self Serve UI](https://github.com/linuxfoundation/lfx-self-serve). The service answers the question: *"Which projects is this user connected to, and through what kinds of engagement?"* — so the UI can personalize navigation, landing views, and feature surfacing without making many parallel upstream calls on every page load.

## What this is (and is not)

**Personas are not authorization.** The name is deliberate: personas describe *how to present* relevant context to a user, not *whether* they may access something. Access control remains with OpenFGA and the access-check layer.

**This is not a REST resource API.** The main contract is a NATS request/reply endpoint. HTTP is limited to Kubernetes health probes (`/livez`, `/readyz`). See [ARCHITECTURE.md](./ARCHITECTURE.md) for the full design rationale.

A user may have **multiple personas** across different projects and foundations. The service returns one entry per project, with one or more **detection** objects describing *why* that project is relevant.

---

## API usage

### NATS endpoint

| Property | Value |
|----------|-------|
| Subject | `lfx.personas-api.get` |
| Queue group | `lfx.personas-api.queue` |
| Pattern | Request/reply (caller publishes a request; service replies on the inbox) |
| Recommended timeout | 5 seconds |

The service fans out to all enabled data sources in parallel. Partial failures from individual sources are logged and skipped; the response still includes results from sources that succeeded. Hard failures (malformed request, validation error) return a top-level `error` field with an empty `projects` array.

### Request

```json
{
  "username": "jdoe",
  "email": "jdoe@example.com"
}
```

| Field | Required | Notes |
|-------|----------|-------|
| `username` | No* | Auth0 `nickname` / LFX username. May be empty for accounts without a username yet. Sources that match on username are skipped when empty. Must contain only `[a-zA-Z0-9_-]` when provided. |
| `email` | Yes | Primary email, normalized to lowercase. Used as the primary identity signal for email-based lookups. |

\* `email` is strictly required; `username` is optional but unlocks additional matching legs.

### Response

```json
{
  "projects": [
    {
      "project_uid": "a1b2c3d4-...",
      "project_slug": "my-project",
      "detections": [
        {
          "source": "board_member",
          "extra": {
            "committee_uid": "...",
            "committee_name": "TAC",
            "committee_member_uid": "...",
            "role": "Chair",
            "voting_status": "Voting Rep",
            "organization": {
              "id": "0014100000Te2ovAAB",
              "name": "The Linux Foundation",
              "website": "http://linuxfoundation.org"
            }
          }
        },
        {
          "source": "cdp_roles",
          "extra": {
            "contributionCount": 42,
            "roles": [
              {
                "id": "...",
                "role": "Maintainer",
                "startDate": "2024-01-01T00:00:00Z",
                "endDate": null,
                "repoUrl": "https://github.com/org/repo",
                "repoFileUrl": "..."
              }
            ]
          }
        },
        { "source": "mailing_list" }
      ]
    }
  ],
  "error": null
}
```

`projects` is always present. It is `[]` when no matches are found.

#### `detections[].source` values

| Token | Meaning |
|-------|---------|
| `board_member` | Member of a committee with category `Board` |
| `executive_director` | Executive Director of the project |
| `cdp_roles` | CDP project affiliation (roles, contribution count) |
| `cdp_activity` | CDP/Snowflake activity signal *(reserved; not yet implemented)* |
| `writer` | Project writer (access-control membership) |
| `auditor` | Project auditor (access-control membership) |
| `committee_member` | Member of any committee (non-Board community signal) |
| `mailing_list` | Subscribed to a project mailing list |
| `meeting_attendance` | Invited to or attended a project meeting |

A single project may appear once with multiple detections. The same `source` token may appear more than once when the user matches that source multiple times (for example, two Board committees under the same project produce two `board_member` detections with different `extra` values).

The UI is responsible for interpreting detection data — for example, reading `cdp_roles.extra.roles[]` to decide whether to show a "Maintainer" label. The service passes CDP role data through without filtering or interpretation.

#### Error response

```json
{
  "projects": [],
  "error": {
    "code": "validation_error",
    "message": "email is required"
  }
}
```

| Code | When |
|------|------|
| `invalid_request` | Request body is not valid JSON |
| `validation_error` | Missing `email` or invalid `username` characters |

### Calling the API

#### NATS CLI

```bash
nats req lfx.personas-api.get \
  '{"username":"jdoe","email":"jdoe@example.com"}' \
  --timeout 5s
```

#### Go (request/reply)

```go
nc, _ := nats.Connect("nats://localhost:4222")
defer nc.Close()

req := []byte(`{"username":"jdoe","email":"jdoe@example.com"}`)
msg, err := nc.Request("lfx.personas-api.get", req, 5*time.Second)
if err != nil {
    log.Fatal(err)
}
fmt.Println(string(msg.Data))
```

#### Health checks (HTTP)

| Endpoint | Purpose |
|----------|---------|
| `GET /livez` | Liveness — process is running |
| `GET /readyz` | Readiness — NATS connection is healthy |

Default listen port: `8080` (override with `-p`).

---

## Personas

Personas are **navigation-centric views** derived from detections. The service does not return a `"persona": "board_member"` field; instead, the UI maps detection sources (and sometimes `extra` payload details) to the persona experience it should show.

Below is how each persona is determined and what data backs it.

### Board Member

**Intent:** Surface projects where the user sits on a **Board-category committee** — typically the highest-touch governance entry point in the UI.

**How it is calculated:**

1. Query the [Query Service](https://github.com/linuxfoundation/lfx-v2-query-service) for `committee_member` resources with `committee_category:Board`, using two parallel legs:
   - **Email leg:** `type=committee_member`, `tags_all=committee_category:Board,email:<email>`
   - **Username leg** (skipped when username is empty): `type=committee_member`, `tags_all=committee_category:Board`, `filters=username:<username>`
2. De-duplicate results by `committee_member` record ID.
3. Apply a **local exact post-filter** on username-leg results (case-insensitive equality on `data.username`) because Query Service `filters` term clauses can be overly broad.
4. For each match, emit a `board_member` detection with committee context in `extra`:
   - `committee_uid`, `committee_name`, `committee_member_uid`
   - `role`, `voting_status`
   - `organization` (`id`, `name`, `website`)

**Notes:**

- `data.email` on committee member records is the reliable identity signal; `data.username` is often empty in production.
- A user on two Board committees under the same project produces two `board_member` detections.
- Role and voting status are surfaced for display only; they are not permission signals.

---

### Executive Director (ED)

**Intent:** Surface projects where the user is the designated **Executive Director**.

**How it is calculated:**

1. Skip entirely when `username` is empty.
2. Query `project_settings` resources: `type=project_settings`, `filters=executive_director.username:<username>`.
3. Post-filter locally for exact case-insensitive match on `data.executive_director.username`.
4. Resolve `project_slug` via the project service NATS endpoint `lfx.projects-api.get_slug`.
5. Emit an `executive_director` detection per matching project (no `extra` fields).

**Data dependency:** The `executive_director` field on `project_settings` must be populated (synced from v1 Salesforce via the v1 sync helper and indexed into OpenSearch). See [ARCHITECTURE.md](./ARCHITECTURE.md) for the upstream prerequisites.

---

### Community

**Intent:** The **default engagement view** — any project the user has a meaningful connection to, regardless of governance role. Community membership is a navigation hint, not an access gate.

Community is not a single detection token. It is the **union** of six engagement sources. The service merges all matches by `project_uid` and lists every matching source in `detections[]`.

#### Source 1: CDP activity (`cdp_activity`) — planned

Snowflake-backed activity aggregation for projects where the user has recorded contributions but may lack a CDP affiliation entry. The `cdp_activity` source token is defined in the contract but **not yet wired** in the service. When implemented, it will share the CDP member-resolution flow and NATS KV cache described below.

#### Source 2: CDP roles and affiliations (`cdp_roles`)

**How it is calculated:**

1. **Resolve** the CDP member ID: `POST /v1/members/resolve` with `{ "lfids": [username], "emails": [email] }`. A 404 means no CDP profile — this source returns empty.
2. **Fetch affiliations:** `GET /v1/members/{memberId}/project-affiliations`.
3. **Cache** both steps in the NATS KV bucket `persona-cache` (24h TTL, stale-while-revalidate after 10 minutes).
4. **Resolve slugs to v2 UIDs** via project service NATS endpoint `lfx.projects-api.slug_to_uid` (parallel per slug).
5. Skip `nonlf_*` slugs and affiliations that fail UID resolution.
6. Emit `cdp_roles` with `extra.contributionCount` and `extra.roles[]` passed through from CDP.

**Requires:** Full CDP credential set (see [Configuration](#configuration)). Autodegrades when CDP is not configured.

#### Source 3: Writers and auditors (`writer`, `auditor`)

**How it is calculated:**

1. Skip when `username` is empty.
2. Two parallel Query Service legs against `project_settings`:
   - `filters=writers.username:<username>`
   - `filters=auditors.username:<username>`
3. Post-filter each leg against the relevant array (`data.writers` or `data.auditors`) for exact case-insensitive username match.
4. A project where the user is both writer and auditor receives **both** detection tokens on one project entry.
5. Resolve `project_slug` via `lfx.projects-api.get_slug`.

#### Source 4: Committee membership (`committee_member`)

**How it is calculated:**

1. Query **all** `committee_member` resources (no `committee_category:Board` filter), using the same dual email/username leg pattern as Board Member.
2. De-duplicate by record ID; post-filter username leg results.
3. Emit `committee_member` with `extra` containing `committee_uid`, `committee_name`, `committee_member_uid`, and `role`.

Board-category members appear in **both** `board_member` (from the Board-only query) and `committee_member` (from the all-committee query). The UI can use `board_member` for the governance persona and `committee_member` for general community engagement.

#### Source 5: Mailing list subscriptions (`mailing_list`)

**How it is calculated:**

1. Two parallel Query Service legs against `groupsio_member`:
   - **Email leg:** `type=groupsio_member`, `tags_all=email:<email>`
   - **Username leg** (skipped when empty): `type=groupsio_member`, `filters=username:<username>` with local post-filter
2. De-duplicate by record ID.
3. Read `project_uid` and `project_slug` from the enriched indexed record.
4. Emit `mailing_list` detection (no `extra` by default).

Subscription is a navigation hint only, not a permission signal.

#### Source 6: Meeting attendance (`meeting_attendance`)

**How it is calculated:**

1. Two parallel Query Service legs against `v1_past_meeting_participant`:
   - **Email leg:** `tags_all=email:<email>`
   - **Username leg** (skipped when empty): `tags_all=username:<username>`
2. Both legs use tag lookups (exact match) — no post-filter needed.
3. De-duplicate by record ID.
4. Include both invited and attended records (`is_invited` / `is_attended` are not filtered — any engagement counts).
5. Read `project_uid` and `project_slug` from the enriched indexed record.
6. Emit `meeting_attendance` detection.

---

### How sources map to UI personas

| UI persona | Detection signal(s) |
|------------|----------------------|
| Board Member | `board_member` |
| Executive Director | `executive_director` |
| Community (default) | Any of: `cdp_roles`, `cdp_activity`, `writer`, `auditor`, `committee_member`, `mailing_list`, `meeting_attendance` |

A user may qualify for multiple personas on the same project. The UI chooses which view to prioritize based on product rules (typically Board Member and ED take precedence over Community).

---

## Development

### Prerequisites

- Go 1.25+
- [NATS](https://nats.io/) server (local or cluster)
- Optionally: Query Service (direct URL or LFX API gateway), CDP API credentials

### Quick start

```bash
# Install dependencies and generate Goa code
make setup
make apigen

# Build and run locally
export NATS_URL=nats://localhost:4222
export QUERY_SERVICE_URL=http://localhost:8081   # or use LFX_BASE_URL + LFX_AUDIENCE

make run
```

The server listens on `:8080` for health checks and subscribes to `lfx.personas-api.get` on NATS.

For debug logging:

```bash
./bin/lfx-v2-persona-service -d
```

### Make targets

| Target | Description |
|--------|-------------|
| `make setup` | `go mod download` and tidy |
| `make setup-dev` | Install golangci-lint |
| `make apigen` | Regenerate Goa HTTP/health code from `cmd/server/design/` |
| `make build` | Build binary to `bin/lfx-v2-persona-service` |
| `make run` | Build and run |
| `make test` | Run tests with race detector and coverage |
| `make lint` | Run golangci-lint |
| `make fmt` | Format Go source |
| `make check` | Format check + lint + license header check |

### Project layout

```
cmd/server/          Entry point, Goa design, HTTP server, NATS wiring
internal/service/    Persona handler and per-source query logic
internal/infrastructure/
  cdp/               CDP API client, Auth0 token provider, NATS KV cache
  query/             Query Service HTTP client
  nats/              NATS connection, subscriptions, KV store
internal/domain/     Models and port interfaces
gen/                 Generated Goa code (do not edit by hand)
charts/              Helm chart for Kubernetes deployment
```

After changing the Goa design in `cmd/server/design/persona.go`, run `make apigen`.

### Local development with minimal config

The service **autodegrades** when optional credential groups are missing. For local work you typically need only:

```bash
export NATS_URL=nats://localhost:4222
export QUERY_SERVICE_URL=http://query-service:8080
```

This enables Board Member, Executive Director, writer/auditor, committee member, mailing list, and meeting attendance sources. CDP (`cdp_roles`) is disabled until CDP credentials are provided.

### Configuration

| Variable | Required | Notes |
|----------|----------|-------|
| `NATS_URL` | Yes | NATS server URL. Default: `nats://localhost:4222` |
| `QUERY_SERVICE_URL` | One of* | Direct Query Service base URL (no auth) |
| `LFX_BASE_URL` | One of* | LFX API gateway URL (requires Auth0 + `LFX_AUDIENCE`) |
| `LFX_AUDIENCE` | With gateway | Auth0 audience for gateway access |
| `AUTH0_ISSUER_BASE_URL` | CDP / gateway | Auth0 tenant base URL |
| `AUTH0_CLIENT_ID` | CDP / gateway | LFX One M2M application client ID |
| `AUTH0_M2M_PRIVATE_BASE64_KEY` | CDP / gateway | Base64 RSA private key for client assertion JWT |
| `CDP_AUDIENCE` | CDP | Auth0 audience for CDP API |
| `CDP_BASE_URL` | CDP | CDP API base URL |
| `NATS_TIMEOUT` | No | NATS request timeout (default `10s`) |
| `NATS_MAX_RECONNECT` | No | Max reconnect attempts (default `3`) |
| `NATS_RECONNECT_WAIT` | No | Wait between reconnects (default `2s`) |

\* Either `QUERY_SERVICE_URL` or `LFX_BASE_URL` (+ `LFX_AUDIENCE` and Auth0 credentials) must be set for Query Service sources to activate.

**CDP credential group:** All five CDP/Auth0 variables must be present to enable `cdp_roles`. If any are missing, the service logs a warning and continues without CDP.

### Caching

| Data | Backend | TTL |
|------|---------|-----|
| CDP `memberId` | NATS KV (`persona-cache`) | 24 hours |
| CDP affiliations | NATS KV (`persona-cache`) | 24 hours |
| Auth0 M2M token | In-process | Expiry − 5 min |

Query Service lookups are **not** cached.

Stale-while-revalidate: entries younger than 10 minutes are served as-is; older entries are returned immediately while a background refresh runs.

### Testing

```bash
make test
```

Source-specific logic and username validation have unit tests under `internal/service/`.

### Deployment

The Helm chart lives in `charts/lfx-v2-persona-service/`. Images are published to `ghcr.io/linuxfoundation/lfx-v2-persona-service/server`. ArgoCD configuration is in [lfx-v2-argocd](https://github.com/linuxfoundation/lfx-v2-argocd).

---

## Further reading

- [ARCHITECTURE.md](./ARCHITECTURE.md) — Full design spec, data flow diagram, caching strategy, and upstream dependencies
- [SECURITY.md](./SECURITY.md) — Security policy

## License

Code: [MIT](./LICENSE). Documentation: [CC BY 4.0](./LICENSE-docs).
