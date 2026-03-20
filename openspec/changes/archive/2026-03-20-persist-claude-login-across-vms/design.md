## Context

Fascinate now provisions user machines as Cloud Hypervisor VMs on the OVH bare-metal host and injects developer tools like Claude Code into each guest. Today all tool auth is guest-local: if a user logs into Claude in one VM, later VMs do not inherit that state, and the user repeats login flows.

The real product need is broader than Claude. Fascinate should eventually handle auth persistence for tools like Claude Code, Codex, OpenCode, and similar agent CLIs without hard-coding a one-off design per tool. At the same time, these tools do not share one universal auth mechanism. Some use interactive session-state on disk, some use API keys, and some rely on cloud/provider credentials.

That means the right abstraction is not “persist Claude login files.” It is a host-managed per-user tool-auth framework with multiple storage modes and tool/method-specific adapters. The first delivery should still ship one concrete adapter: Claude Code subscription login.

## Goals / Non-Goals

**Goals:**
- Introduce a generic per-user tool-auth framework that persists auth independently of any single VM.
- Support multiple auth persistence modes:
  - session-state bundles
  - secret-material injection
  - provider-credential adapters
- Restore supported auth into a VM before the machine is marked ready.
- Capture updated auth state from running VMs and persist it back to the user’s Fascinate account.
- Keep stored auth encrypted at rest and isolated by user, tool, and auth method.
- Ship Claude Code subscription login as the first real adapter on top of the framework.

**Non-Goals:**
- Implement every tool adapter in the first delivery.
- Force all tools into one auth persistence mechanism.
- Replace supported API-key helper flows for tools that already have a better native secret model.
- Multi-host auth replication.
- Real-time shared writable auth state across simultaneously running VMs.

## Decisions

### 1. Model auth as `user + tool + auth method`, not just “Claude login”

Fascinate will persist tool auth using a canonical identity made from:
- Fascinate user ID
- tool ID (`claude`, `codex`, `opencode`, etc.)
- auth method ID (`claude-subscription`, `anthropic-api-key`, `codex-chatgpt`, `bedrock`, etc.)

This becomes a `ToolAuthProfile` in host storage and metadata.

Why:
- It keeps the framework generic from the start.
- It prevents mixing multiple auth modes for the same tool.
- It gives later adapters a place to plug in without redesigning storage.

Alternative considered:
- Persist a single “Claude auth” concept first and generalize later. Rejected because it would hard-code the wrong abstraction into the data model.

### 2. Support three storage modes under one framework

Fascinate will support three adapter storage modes:
- `session_state`: opaque filesystem bundles restored into the guest and captured back out
- `secret_material`: centrally stored secrets projected into the guest as env/config/files
- `provider_credentials`: tool/provider-specific adapters for cloud-backed auth flows

Why:
- These three modes cleanly cover the main auth patterns across current agent CLIs.
- They let the framework stay generic without pretending every tool works the same way.

Alternative considered:
- One generic “copy files around” model for all tools. Rejected because API-key and provider-backed auth are better handled through structured secret projection.

### 3. Keep the framework generic, but ship only Claude subscription first

The first implementation will include the full framework plus one concrete adapter:
- tool: `claude`
- auth method: `claude-subscription`
- storage mode: `session_state`

This adapter will persist the managed set of Claude Code Linux user-state paths as an opaque encrypted bundle.

Why:
- Claude is the immediate user pain.
- The framework becomes reusable immediately, but the initial delivery stays bounded.

Alternative considered:
- Implement Claude, Codex, and OpenCode together. Rejected because it increases scope and debugging surface too early.

### 4. Treat session-state adapters as opaque versioned filesystem bundles

For `session_state` adapters, Fascinate will persist a versioned archive of managed guest paths without parsing the tool’s credential format. Each adapter declares:
- which guest paths belong to the auth profile
- which owner/permission model to restore
- how to validate presence after hydration

Why:
- It is robust against private on-disk auth formats.
- It keeps the framework generic for Claude now and Codex/OpenCode later.

Alternative considered:
- Parse specific token files. Rejected because it is brittle and tool-specific in the wrong layer.

### 5. Hydrate tool auth during provisioning before `RUNNING`

If a user has a stored supported auth profile, Fascinate will restore it into the guest before the machine transitions from `CREATING` to `RUNNING`.

Why:
- “Ready” should mean the tool is ready too.
- It avoids exposing half-configured VMs to users.

Alternative considered:
- Lazy restore on first shell entry. Rejected because it makes readiness inconsistent and complicates first-use flows.

### 6. Capture updated auth at controlled checkpoints plus background sync

For session-state adapters, Fascinate will capture guest auth changes:
- after shell/tutorial exit through the frontdoor
- on VM stop/delete
- through a periodic background sync worker for running VMs

Why:
- It makes “log in once, use later VMs” practical without shared writable mounts.
- It also lets logout/reset propagate.

Alternative considered:
- Capture only on VM destruction. Rejected because it delays availability too much.

### 7. Treat the stored profile as the exact current state, including logout

For session-state adapters, the host profile is the canonical current state. A captured guest snapshot replaces the stored version, including deletions.

Why:
- It makes logout/reset behavior predictable.
- It avoids stale credentials surviving after an intentional logout.

Alternative considered:
- Merge only additive changes. Rejected because it breaks reset/logout semantics.

## Risks / Trade-offs

- **[Different tools have undocumented state layouts]** → Each adapter owns a versioned path manifest and validation logic, while the framework stays storage-mode-driven.
- **[Sensitive auth state on host]** → Encrypt profiles at rest with host-managed key material and keep access keyed by user, tool, and auth method.
- **[Background sync adds load]** → Limit sync to profiles using session-state adapters and only persist on detected changes.
- **[Concurrent VMs change the same profile]** → Use last-successful-sync wins in the first delivery and log sync timestamps for diagnosis.
- **[Framework may feel too abstract too early]** → Keep the abstraction minimal and prove it with exactly one adapter first.
- **[Provider-backed auth may not fit neatly]** → Keep provider-credential adapters explicit so Bedrock/Vertex-style methods do not have to masquerade as session-state bundles.

## Migration Plan

1. Add a generic `ToolAuthProfile` model, host encryption config, and storage layout.
2. Implement storage-mode abstractions and the host helpers they need.
3. Build the Claude subscription `session_state` adapter on top of that framework.
4. Extend VM provisioning to hydrate supported tool auth before a machine becomes `RUNNING`.
5. Add guest capture checkpoints and a periodic sync worker for session-state profiles.
6. Validate from a clean user state on OVH:
   - first Claude VM requires login
   - second Claude VM restores login automatically
   - logout propagates to later Claude VMs
7. Leave room for later adapters such as Codex ChatGPT login, OpenCode `auth.json`, and API/provider methods without redesigning the framework.

Rollback:
- Disable tool-auth hydration/capture and fall back to guest-local auth only.
- Keep the framework metadata, but stop applying profiles if the first adapter is unstable.

## Open Questions

- Which exact Claude Linux paths should the first `session_state` manifest include beyond `~/.claude`?
- Should Codex ChatGPT login be modeled as another `session_state` adapter or do we need a distinct variant?
- What should the first `provider_credentials` adapter target be once Claude subscription is stable: Bedrock, Vertex, or something else?
- Do we want a user-facing “reset tool auth across all VMs” action in the same family of changes, or keep that manual at first?
