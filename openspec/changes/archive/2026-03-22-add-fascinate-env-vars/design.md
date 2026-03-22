## Context

Fascinate already injects machine guidance into VMs under `/etc/fascinate/AGENTS.md` during first boot and rewrites that file after restore/clone to refresh machine identity. Today there is no first-class environment-variable object in the control plane, so agents and users end up hardcoding machine-specific values like `https://m-1.fascinate.dev` into repo files. That conflicts with snapshot and clone behavior because those literals are copied from the source VM instead of being regenerated for the target machine.

There are no live users to migrate and no backward-compatibility constraints to preserve. This allows the change to make `/etc/fascinate/*` a control-plane-managed surface from the start instead of layering env vars on top of ad hoc guest state.

## Goals / Non-Goals

**Goals:**
- Add a first-class persisted env-var object owned by a Fascinate user.
- Provide built-in machine-specific Fascinate vars that are regenerated per machine on create, restore, and clone.
- Render an effective env set into canonical guest files that shells, agents, and app launchers can use consistently.
- Expose CRUD and inspection surfaces over the API and SSH/frontdoor.
- Update VM-injected `AGENTS.md` / `CLAUDE.md` so agents are taught to use Fascinate env vars instead of hardcoded hostnames.

**Non-Goals:**
- Secret management or encrypted secret projection. This change is for plain env vars only.
- Per-machine or per-project overrides in v1.
- Retroactive repair of already-running app processes that captured old env values in memory.
- Automatic rewriting of arbitrary app config files beyond the managed Fascinate env files and agent instructions.

## Decisions

### 1. Store env vars as a dedicated per-user object
Use a new database table keyed by `(user_id, key)` rather than a JSON blob on `users`.

Rationale:
- Makes CRUD, validation, and diagnostics straightforward.
- Keeps future secrets or per-machine overrides composable.
- Avoids overloading the user record with structured config.

Alternatives considered:
- `users.settings_json`: simpler migration, but harder to query, validate, diff, and extend cleanly.
- Storing only rendered files on disk: wrong source of truth for clone/restore because rendered machine values must be recomputed.

### 2. Split env vars into built-in machine vars and user-defined vars
Fascinate will always provide read-only built-ins such as:
- `FASCINATE_MACHINE_NAME`
- `FASCINATE_MACHINE_ID`
- `FASCINATE_PUBLIC_URL`
- `FASCINATE_PRIMARY_PORT`
- `FASCINATE_BASE_DOMAIN`
- `FASCINATE_HOST_ID`
- `FASCINATE_HOST_REGION`

Users can define additional vars, but cannot override `FASCINATE_*`.

Rationale:
- Solves the clone/source-hostname problem directly.
- Gives agents one stable contract to target across all VMs.

Alternatives considered:
- User-defined only: does not solve machine identity refresh.
- Machine-only: too limited for real app config.

### 3. Render effective env vars at machine sync time, not at write time in the DB
Persist raw user values and compute the effective rendered env when a machine is created, restored, cloned, or explicitly resynced.

Rationale:
- Built-ins differ per machine.
- Keeps snapshots and clones from inheriting stale literal values.
- Makes interpolation deterministic against the target machine.

Alternatives considered:
- Pre-rendering and storing per-user values: breaks machine-specific built-ins.

### 4. Support simple `${NAME}` interpolation with validation
User-defined values may reference built-ins or other user-defined vars via `${NAME}` syntax. Rendering SHALL reject:
- undefined references
- cycles
- attempts to reference forbidden keys

Rationale:
- Lets users define `FRONTEND_URL=${FASCINATE_PUBLIC_URL}` and similar contracts cleanly.
- Keeps behavior predictable enough for agents and tests.

Alternatives considered:
- No interpolation: forces literal duplication of Fascinate metadata and loses most of the value.
- Full shell expansion: too ambiguous and too easy to abuse.

### 5. Expose env vars in canonical guest files under `/etc/fascinate/`
Every machine will receive:
- `/etc/fascinate/env`
- `/etc/fascinate/env.sh`
- `/etc/fascinate/env.json`
- `/etc/profile.d/fascinate-env.sh`

`/etc/profile.d/fascinate-env.sh` will export the rendered vars for interactive shells. `AGENTS.md` / `CLAUDE.md` will point agents at these files explicitly.

Rationale:
- Fits the existing `/etc/fascinate/AGENTS.md` pattern.
- Works for shell usage, Docker Compose `env_file`, and structured tooling.

Alternatives considered:
- `/etc/environment`: too blunt, not structured, and harder to manage safely.
- Home-directory dotfiles only: weaker for future additional users and less canonical.

### 6. Make env sync a control-plane-managed lifecycle step
Fascinate will write env files:
- during first boot guest bootstrap
- after restore / clone identity refresh
- after user env-var updates (best-effort sync to running VMs)

Machine readiness on create/restore/clone should include env-file sync completion.

Rationale:
- Prevents snapshot copies from leaving stale source-machine env files behind.
- Makes the control plane the source of truth instead of guest-local drift.

Alternatives considered:
- Only writing env on first boot: breaks clone correctness.
- Leaving env updates to the user/agent: defeats the feature’s purpose.

### 7. Start with user-global scope only
V1 env vars apply to all machines for a user.

Rationale:
- Solves the immediate machine-identity and agent-config problem with minimal data-model complexity.
- Leaves room for future per-machine overrides or project/workload contracts.

Alternatives considered:
- Per-machine scope immediately: more expressive, but adds UI/API complexity before the basic contract exists.
- Per-project scope: there is no first-class project model yet.

### 8. Treat VM-injected agent instructions as part of the feature contract
The generated `AGENTS.md` / `CLAUDE.md` should tell agents:
- Fascinate env vars exist
- built-in machine vars should be preferred over hardcoded hostnames
- files like `.env` should reference `FASCINATE_PUBLIC_URL` where the app supports env expansion or generated config

Rationale:
- The feature is only useful if agents actually use it.
- The VM bootstrap already owns these instructions, so this is the correct leverage point.

## Risks / Trade-offs

- [User updates env vars while apps are already running] → Existing processes will not magically adopt new values. Mitigation: rewrite guest env files immediately and document that already-running apps may need restart.
- [Interpolation becomes confusing] → Limit syntax to `${NAME}`, reject undefined references and cycles, and expose a machine-specific “effective env” inspection endpoint.
- [Guest file drift after clone/restore] → Re-render and rewrite `/etc/fascinate/env*` during the existing post-restore identity refresh step.
- [Agents still hardcode hostnames] → Update injected `AGENTS.md` / `CLAUDE.md` and benchmark prompts to point at `FASCINATE_PUBLIC_URL`.
- [Future secrets need different handling] → Keep this change strictly for plain env vars and introduce a separate secrets object later.

## Migration Plan

1. Add the new env-var data model and CRUD surfaces.
2. Implement rendering, validation, and interpolation.
3. Add guest env-file generation and shell/profile integration in VM bootstrap.
4. Extend restore/clone identity refresh to rewrite env files.
5. Add running-VM resync on env-var updates.
6. Update SSH/frontdoor commands, API docs, and injected agent instructions.

Rollback:
- Remove the new CRUD surfaces and stop writing `/etc/fascinate/env*`.
- Existing VMs will simply retain the last rendered files until reprovisioned.

Because there are no users to migrate, no data-compatibility bridge is required.

## Open Questions

- Should v1 include a machine-specific “preview effective env” endpoint only, or also a shell/frontdoor command that dumps the effective env for a machine?
- Should user-defined values be able to reference other user-defined values transitively, or should interpolation be limited to built-ins in v1?
- Do we want the first UX to be API/SSH only, or should the TUI also expose env-var management in the same change?
