# AGENTS.md — LFX v2 Persona Service

Guidance for AI coding assistants (Claude Code, OpenCode, Cursor, Copilot, etc.) working in this repository. Read this before making changes. `CLAUDE.md` is a symlink to this file.

For deep design rationale see `README.md` and `ARCHITECTURE.md`.

---

## 1. What this service is

- Go microservice in the LFX v2 / LFX Self-Service platform.
- Aggregates a user's involvement across LF projects and foundations into a single UI-friendly "persona" response consumed by the LFX Self-Service UI.
- **Not** an authorization service — access control lives in OpenFGA / the access-check service.
- **Not** a REST resource API — HTTP is only for Kubernetes health probes (`/livez`, `/readyz`).
- Primary contract is **NATS request/reply** on subject `lfx.personas-api.get`, queue group `lfx.personas-api.queue` (see `pkg/constants/subjects.go`).
- A user can have multiple detections per project; the UI decides how to render personas from the returned `detections[]`.

---

## 2. Architecture (hexagonal / ports & adapters)

```
cmd/server/                   Composition root
├── main.go                   Flag parsing, OTel bootstrap, HTTP + NATS wiring
├── http.go                   Hand-written HTTP server bootstrap (mounts the Goa-generated health server)
├── design/persona.go         Goa DSL — HEALTH ENDPOINTS ONLY
└── service/                  Provider wiring (NATS, Query, CDP)
    ├── persona.go
    ├── providers.go          Functional-option composition (WithCDP, WithQueryService)
    └── message_handler.go

internal/domain/              Pure domain (no I/O)
├── model/persona.go          PersonaRequest, PersonaResponse, Project, Detection
└── port/                     Interfaces the domain depends on
    ├── message_handler.go
    └── transport_messenger.go

internal/service/             Application services / business logic
├── message_handler.go        personaHandler — fans out to sources, merges by project_uid
├── source_board_member.go
├── source_executive_director.go
├── source_mailing_list.go
├── source_meeting_attendance.go
├── source_writer_auditor.go
└── username_validation.go    [a-zA-Z0-9._-]

internal/infrastructure/      Adapters — external I/O
├── nats/                     NATS client, JetStream KV, OTel context propagation, TransportMessenger
├── query/                    LFX Query Service HTTP client (committee_member, project_settings, groupsio_member, v1_past_meeting_participant)
└── cdp/                      CDP client, Auth0 M2M private-key JWT TokenProvider, NATS KV cache (10-min stale-while-revalidate)

internal/config/config.go     Env-driven config with capability autodegrade

pkg/                          Importable helpers
├── constants/                Env keys, NATS subjects, KV bucket names — DO NOT INLINE THESE STRINGS
├── errors/
├── log/                      Structured slog init
└── utils/otel.go             OTel SDK bootstrap

gen/                          Goa-generated code — DO NOT EDIT BY HAND
├── persona_service/          Service, endpoints, client interfaces
└── http/
    ├── persona_service/      HTTP server + client transport
    ├── cli/                  Generated CLI
    ├── openapi.{json,yaml}   OpenAPI 2.0 spec (health only)
    └── openapi3.{json,yaml}  OpenAPI 3.0 spec (health only)
```

### Ports/adapters discipline

- New external I/O → `internal/infrastructure/<x>/`.
- Expose it via a **port interface** in `internal/domain/port/`.
- Consume through the port from `internal/service/`.

### Adding a new persona data source

1. Create `internal/service/source_<name>.go`.
2. Add a `PersonaHandlerOption` (e.g., `WithFoo(...)`) in `internal/service/message_handler.go`.
3. Wire it in `cmd/server/service/providers.go`.
4. Register merge logic into the fan-out inside `personaHandler.GetPersona` (`internal/service/message_handler.go`).
5. Never make source failures fatal — log and continue with partial results (autodegrade contract).

---

## 3. Tech stack

- **Go 1.25.0** (`go.mod`, `Makefile` `GO_VERSION := 1.25.0`).
- Framework: **Goa v3.23.3** (`goa.design/goa/v3`) — used only for the HTTP health scaffolding. Persona NATS handling is intentionally hand-rolled.
- NATS: `github.com/nats-io/nats.go v1.45.0` (core + JetStream KV).
- Testing: `github.com/stretchr/testify` (assert + require). No mock framework — hand-rolled `httptest` fakes and interface stubs.
- Auth: `github.com/golang-jwt/jwt/v5` + `golang.org/x/oauth2` for Auth0 private-key-JWT client credentials.
- Observability: OpenTelemetry v1.44.0 — `autoexport`, `autoprop`, `otelhttp`, `remychantenay/slog-otel`.
- Container builds: **`ko`** from `./cmd/server` (`.ko.yaml`), with `Version`/`BuildTime`/`GitCommit` ldflags. A Chainguard-based `Dockerfile` is available as a fallback.
- K8s: Helm chart at `charts/lfx-v2-persona-service/`. Image: `ghcr.io/linuxfoundation/lfx-v2-persona-service/server`. ArgoCD manifests live in `linuxfoundation/lfx-v2-argocd`.

---

## 4. Make targets

Development / setup:

- `make setup` — `go mod download && go mod tidy`.
- `make setup-dev` — install `golangci-lint v2.2.2`.
- `make deps` — install Goa CLI.
- `make apigen` — regenerate `gen/` from `cmd/server/design`.

Build / run:

- `make build` — runs `apigen` then builds `bin/lfx-v2-persona-service` with version ldflags.
- `make run` — build + run.
- `make debug` — build + run with `-d` (debug logging).

Quality gate (**pre-commit**):

- `make test` — `go test -v -race -coverprofile=coverage.out ./...`.
- `make fmt` — `go fmt ./...` runs over all packages; the follow-on `gofmt -s -w` pass excludes `./gen/` and `./vendor/` (via `GO_FILES` in `Makefile:23`).
- `make lint` — `golangci-lint run ./...`. Requires the linter to be installed via `make setup-dev` first; the `lint` target's auto-install fallback currently uses the v1 module path and cannot install v2.x, so run `make setup-dev` on a clean machine.
- `make license-check` — enforces the SPDX header on every non-generated `.go/.html/.txt` file.
- `make check` — gofmt check + lint + license-check. **Run this before every commit.**

Container:

- `make docker-build`, `make docker-run`.

---

## 5. Code style, licensing, generated code

- **License header is mandatory** on every new `.go`, `.html`, and `.txt` file (outside `gen/`). CI enforces via `.github/workflows/license-header-check.yml`; the local `make license-check` grep is substring-based (comment-syntax agnostic) but each file type uses its own comment syntax — use the matching variant:
  ```go
  // Copyright The Linux Foundation and each contributor to LFX.
  // SPDX-License-Identifier: MIT
  ```
  ```html
  <!-- Copyright The Linux Foundation and each contributor to LFX. -->
  <!-- SPDX-License-Identifier: MIT -->
  ```
  ```
  # Copyright The Linux Foundation and each contributor to LFX.
  # SPDX-License-Identifier: MIT
  ```
- Format with `gofmt -s`. No gofumpt/goimports configured.
- `golangci-lint` runs with defaults — there is no `.golangci.yml` in the repo. Keep changes clean under those defaults.
- **Never edit files under `gen/`.** Regenerate via `make apigen` after touching `cmd/server/design/persona.go`. `make build` runs `apigen` automatically.
- **Never inline string literals for env var names, NATS subjects, or KV bucket names.** They live in `pkg/constants/`. Changing the NATS **subject** (`PersonaGetSubject`) is a breaking contract change with the UI — callers publish to that subject. Changing the **queue group** (`PersonaServiceQueue`) is a deployment/rollout-coordination concern only (mixed queue groups deliver/reply independently); callers never reference it.

---

## 6. Testing conventions

- Tests are co-located with sources under `internal/`.
- Framework: `stretchr/testify` (`assert` + `require`).
- Race detector is always on via `make test`.
- HTTP dependencies are faked with `httptest.Server`; other adapters use interface stubs. **Do not introduce mockgen or gomock without discussion.**
- Prefer table-driven tests (`internal/service/username_validation_test.go` is a good reference).
- Coverage profile is written to `coverage.out`.

---

## 7. Local development

Prereqs: Go 1.25+, a running NATS server, and optionally Query Service / CDP credentials.

```bash
make setup && make apigen
export NATS_URL=nats://localhost:4222
export QUERY_SERVICE_URL=http://localhost:8081   # or LFX_BASE_URL + LFX_AUDIENCE + Auth0 vars
make run       # or `make debug` for -d debug logging
```

- `.env` in the repo root is a **developer helper** using `1Password op` CLI calls to inject Auth0/CDP creds and pointing at cluster NATS. It is **not** auto-loaded; `source .env` manually.
- The service **autodegrades**:
  - Missing any CDP env var → `cdp_roles` disabled.
  - Missing `QUERY_SERVICE_URL` (and no `LFX_BASE_URL`/`LFX_AUDIENCE`) → Query-Service-backed sources disabled.
- The `persona-cache` NATS KV bucket is **not** created by the service — the Helm chart provisions it (`charts/lfx-v2-persona-service/templates/nats-kv-buckets.yaml`, 24h TTL). Locally, if the bucket is missing the service logs a warning and disables the cache.
- Health endpoints listen on port `8080` (`GET /livez`, `GET /readyz`); override with `-p` / `-bind`.

---

## 8. API and interface

- **Primary contract: NATS** — `lfx.personas-api.get`, JSON request `{username, email}`. Full schema in `README.md` §"API usage" and `ARCHITECTURE.md`.
- **Goa design**: `cmd/server/design/persona.go` — defines **only** `livez` and `readyz`. Do NOT add the persona endpoint to the Goa design.
- **OpenAPI**: auto-generated at `gen/http/openapi.{json,yaml}` and `openapi3.{json,yaml}` (health only).
- No gRPC.

---

## 9. Deployment

- Helm chart: `charts/lfx-v2-persona-service/` (Deployment, Service, ServiceAccount, PDB, `nats-kv-buckets.yaml`). Chart version is bumped by release automation; `appVersion: latest` in-tree.
- Image is built by `ko` and published to `ghcr.io/linuxfoundation/lfx-v2-persona-service/server`.
- CI workflows (`.github/workflows/`):
  - `ko-build-main.yaml` — push to `main`, tags `<sha>` and `development`, SBOM `spdx`, `linux/amd64,linux/arm64`.
  - `ko-build-branch.yaml`, `ko-build-tag.yaml` — branch/tag builds.
  - `license-header-check.yml` — reusable LF workflow with `exclude_pattern: "gen"`.
- ArgoCD app manifests live in `linuxfoundation/lfx-v2-argocd`.

---

## 10. Repo-specific gotchas

- The autodegrade contract is not optional — never make a source failure fatal to the whole response.
- The `nats` file at the repo root is an accidental capture of `nats kv get --help` output; ignore it (do not delete as part of unrelated work).
- `CODEOWNERS` is a single-line file scoping the whole repo to `@linuxfoundation/lfx-v2-persona-service`.
- `.claude/memory/` may contain prior AI notes — worth skimming before large changes, but not authoritative.
- CDP token provider uses Auth0 **private-key JWT (RS256)**; the key is base64-encoded PKCS in `AUTH0_M2M_PRIVATE_BASE64_KEY`. The same provider is reused for LFX API gateway auth with a different audience/scope.
- KV cache uses `PutString`, relies on bucket TTL (24h), and layers a 10-minute app-level stale-while-revalidate on top.
- `Makefile` has `GOOS := linux, GOARCH := amd64` near the top — informational only; `make build` builds for the host OS.

---

## 11. Commit / PR expectations

- Reference the Jira ticket in commit messages: `Issue: LFXV2-XXXX`.
- Sign commits with `-s` and `-S`.
- Run `make check` and `make test` before opening a PR.
- Do not commit anything under `gen/` by hand — regenerate.
- Keep PRs scoped; unrelated cleanups belong in their own ticket.
