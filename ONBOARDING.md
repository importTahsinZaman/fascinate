# Fascinate Onboarding Guide

This guide is for a contributor who is new to this codebase and also new to VM, server, and infrastructure concepts.

The goal is simple: read this from top to bottom and come away able to contribute intelligently to Fascinate without treating the code as magic.

## How To Use This Guide

Read it in order once.

Then come back and use it in three ways:

1. As a glossary when you hit unfamiliar infrastructure terms.
2. As a map when you need to find the right package to change.
3. As a checklist before making control-plane, runtime, browser-terminal, snapshot, or tool-auth changes.

Note: Fascinate is now browser-first. Historical references below to the old SSH frontdoor, Bubble Tea dashboard, or `seed-ssh-key` flow are obsolete and should be ignored if they have not been rewritten yet.

## What Fascinate Is

Fascinate is a browser-first command center for persistent developer VMs.

In practical terms, it gives a user:

- a persistent Linux VM
- shell access to that VM through the browser command center
- a hosted app URL for the VM's main application port
- full-VM snapshots
- true forks created from snapshots
- persisted tool login state for supported tools like Claude, Codex, and GitHub CLI

The codebase is not a generic cloud platform. It is a focused product with a clear shape:

- one Go binary, `fascinate`
- SQLite for product state
- a first-class host registry, even in one-box deployments
- Cloud Hypervisor as the VM runtime
- Linux network namespaces for per-VM isolation
- an HTTP API
- browser auth plus a browser terminal gateway
- host-side storage for persisted auth bundles

The shortest mental model is:

- `internal/controlplane` decides what should exist
- `internal/controlplane/hosts.go` decides which host should do the work
- `internal/runtime/cloudhypervisor` makes it exist on the owning host
- `internal/database` remembers product state
- `internal/httpapi` and `internal/browserterm` let users interact with it

## Start With The Basics

If you are new to infrastructure, these are the minimum concepts you need.

### Server

A server is just a machine running software that other machines connect to.

In this project there are really two levels of "server":

- the host machine running Fascinate itself
- the guest VM running inside that host

### VM

A VM, or virtual machine, is a software-defined computer.

It has:

- virtual CPU
- virtual memory
- a virtual disk
- a virtual network card
- its own operating system

To the user inside the guest, it feels like a normal Linux machine.

### Hypervisor

A hypervisor is the software that runs VMs.

Fascinate uses Cloud Hypervisor, not Docker, not Incus, and not a container runtime. That distinction matters a lot:

- containers share the host kernel
- VMs get their own kernel and boot like full machines

That is why snapshots and forks in this repo are "real VM" operations, not just filesystem copies.

### Control Plane

A control plane is the software that accepts product-level actions and turns them into infrastructure actions.

Examples:

- "create machine"
- "delete machine"
- "create snapshot"
- "fork machine"

In Fascinate, the control plane mostly lives in `internal/controlplane/service.go`.

### Runtime

The runtime is the lower-level machinery that actually boots, restores, deletes, snapshots, and lists VMs.

In this repo:

- `internal/runtime/runtime.go` defines the interface
- `internal/runtime/cloudhypervisor` implements it

### SSH

SSH is used twice here:

- users SSH into Fascinate's front door
- Fascinate itself SSHes into guest VMs to hand the user a shell and to capture or restore tool auth

That second use is easy to miss. The host acts like an operator of the guest.

### Cloud-Init

Cloud-init is a guest bootstrapping mechanism.

When Fascinate creates a fresh VM, it generates a seed image that tells the guest things like:

- hostname
- SSH configuration
- initial first-boot setup
- network setup

This is how a new VM turns from "raw Ubuntu image" into "developer machine with tools installed."

### Network Namespace

A Linux network namespace is an isolated networking environment.

You can think of it as "a separate network stack inside the same host kernel."

Fascinate gives each VM its own namespace. That lets the system:

- isolate per-VM host-side networking
- preserve guest network identity across snapshot restore
- avoid requiring globally unique guest IPs in the host root namespace

### NAT

NAT means rewriting network traffic so one network can reach another through an intermediate machine.

Fascinate sets up NAT rules so guests can reach the outside world.

### Reverse Proxy

A reverse proxy accepts requests and forwards them to another server.

In Fascinate there are two forms of forwarding:

- wildcard HTTP routing from `https://<machine>.<base-domain>` to the VM's app port
- host-side per-machine forwarders that map localhost ports into a guest inside a network namespace

### Snapshot

A snapshot is a point-in-time capture of a VM.

In this repo a saved snapshot includes:

- disk state
- memory state
- device state

That means a restored VM can resume from where the original machine was, not just reboot from disk.

### Fork

In many systems "fork" means "copy the disk and boot a new instance."

Not here.

In Fascinate, fork means:

1. take an implicit snapshot of the source VM
2. restore that snapshot into a new VM

So fork preserves live environment state, not just files on disk.

## The Product From A User's Point Of View

Before reading the internals, understand the user experience.

### Normal flow

1. A user connects to Fascinate over SSH.
2. Their public key is checked against SQLite.
3. If the key is known, they enter the dashboard or run an exec-style SSH command.
4. They can create, delete, fork, snapshot, or enter a shell in a VM.
5. When they exit the guest shell, Fascinate captures supported tool auth from the guest.
6. When they later create a fresh VM, Fascinate restores that stored auth before marking the machine ready.

### Signup flow

If an SSH key is unknown and email signup is configured:

1. the SSH connection is allowed into a restricted signup mode
2. the user enters an email
3. Fascinate emails a 6-digit code
4. the user verifies the code
5. Fascinate creates or upserts the user and associates the SSH key
6. the dashboard opens in the same session

### HTTP flow

The HTTP API is mainly for integration and operator-style use. It exposes:

- health and readiness
- runtime machine listing
- machine CRUD-ish operations
- snapshot listing/creation/deletion
- diagnostics for hosts, machines, snapshots, tool auth, and events

It also provides wildcard proxying for machine subdomains.

## The Big Architecture

Here is the high-level dependency picture:

```text
cmd/fascinate
  -> internal/config
  -> internal/app
       -> internal/database
       -> internal/runtime/cloudhypervisor
       -> internal/toolauth
       -> internal/controlplane
       -> internal/httpapi
       -> internal/signup
       -> internal/sshfrontdoor
       -> internal/tui
```

And here is the operational picture:

```text
User
  -> SSH front door / HTTP API
  -> control plane
  -> database + host registry
  -> host-aware executor boundary
  -> Cloud Hypervisor VM + network namespace + forwarders
```

The cleanest way to think about this system is as two layers of truth plus one placement layer.

### Layer 1: Product truth in SQLite

SQLite stores things the product cares about:

- users
- SSH keys
- machines
- snapshots
- email codes

This is where user ownership, quotas, names, and product state live.

### Placement layer: Host truth in SQLite

The control plane now also tracks hosts as first-class resources.

That layer stores things like:

- host identity
- host role and region
- host health and heartbeat freshness
- host capacity and current allocation
- whether a host is currently eligible for new placement
- which host owns each machine and snapshot

Even in a single-host deployment, the code now behaves as if there is a host registry. That is intentional. It keeps one-box behavior working while preparing the architecture for multi-host execution later.

### Layer 2: Infrastructure truth on disk and in processes

The runtime owns things like:

- machine directories in `data/machines`
- snapshot directories in `data/snapshots`
- `cloud-hypervisor` processes
- network namespaces
- tap devices
- forwarder processes
- VM metadata JSON files

The control plane is constantly reconciling these two worlds.

## The Most Important Packages

If you only remember one section from this document, make it this one.

### `cmd/fascinate`

This is the binary entrypoint.

Read `cmd/fascinate/main.go` first. It shows the entire surface area:

- `serve`
- `migrate`
- `runtime-machines`
- `version`
- `netns-forward`

The default command is `serve`.

### `internal/app`

This is the composition root.

`internal/app/app.go` answers the question: "How does the whole system get wired together?"

It:

- creates the data directory
- opens the database
- runs migrations
- creates the Cloud Hypervisor runtime manager
- creates the tool-auth store and manager
- creates the control-plane service
- ensures the local host exists in the host registry
- heartbeats the local host once during startup
- reconciles runtime state on startup
- creates the HTTP server
- creates the signup service
- creates the SSH front door
- runs the background tool-auth sync loop
- runs the periodic runtime reconcile loop
- runs the periodic local-host heartbeat loop

If you understand `app.New`, you understand how the whole service boots.

### `internal/config`

This package defines the environment-backed configuration model.

Important defaults:

- HTTP listens on `127.0.0.1:8080`
- SSH listens on `127.0.0.1:2222`
- data lives under `./data`
- the default host ID is `local-host`
- the default host role is `combined`
- the default host region is `local`
- runtime machine state lives under `./data/machines`
- snapshots live under `./data/snapshots`
- tool auth lives under `./data/tool-auth`

Important behavior:

- an env file is loaded first
- already-set environment variables win over values in that file
- host configuration is env-backed too: ID, name, region, role, and heartbeat interval

### `internal/database`

This package is the durable product memory.

You should think of its responsibilities as:

- schema migrations
- CRUD for users, keys, machines, snapshots, and email codes
- enforcing soft deletion through `deleted_at`

Important tables:

- `users`
- `ssh_keys`
- `hosts`
- `machines`
- `snapshots`
- `email_codes`
- `events`

The `events` table exists in the initial schema but is not the center of this codebase today.

### `internal/controlplane`

This is the brain.

It enforces:

- machine name validation
- per-user locking
- machine quotas
- machine size limits
- host-aware placement
- host ownership for machines and snapshots
- ownership checks
- async creation flow
- snapshot lifecycle
- tool-auth capture and restore hooks
- startup reconciliation

This package is where product intent turns into runtime calls.

### `internal/runtime`

This package defines the interface between the control plane and the actual VM implementation.

That interface is intentionally small:

- list/get/create/delete machines
- start machines
- list/get/create/delete snapshots
- fork machines
- health check

This is an important design seam. If you ever need to change the VM implementation, this interface is the boundary you would preserve first.

### `internal/runtime/cloudhypervisor`

This is the hands of the system.

It handles:

- VM metadata files
- guest disk creation
- cloud-init seed image generation
- per-VM network namespace setup
- Cloud Hypervisor start and restore
- snapshot artifact creation
- fork-via-snapshot
- host-side forwarders into the namespace
- guest SSH probing
- tool-auth capture and restore transport

This is the most infrastructure-heavy package in the repo.

### `internal/httpapi`

This package exposes product and runtime behavior over HTTP.

It is intentionally thin. It mostly validates input, calls the control plane or runtime, and writes JSON.

It also does wildcard machine proxying when `FASCINATE_BASE_DOMAIN` is set.

The newer diagnostics endpoints are especially helpful when learning the system because they expose operator-facing views of:

- hosts
- per-machine runtime handles and forwarders
- snapshot artifacts
- stored tool-auth profile state
- recent lifecycle events

### `internal/sshfrontdoor`

This package exposes the product over SSH.

That includes:

- public key authentication
- signup-mode handling for unknown keys
- exec-style command handling
- interactive dashboard flow
- guest shell handoff
- tutorial flow
- post-session tool-auth sync

This package is important because it is both an auth surface and a transport layer.

### `internal/tui`

This package contains the Bubble Tea user interface for:

- signup
- the dashboard

It is product-facing and helpful for understanding what the system thinks a machine or snapshot "is" from a user perspective.

### `internal/toolauth`

This package makes persisted tool login state work.

It contains:

- encrypted host-side storage
- adapter definitions
- capture logic
- restore logic
- per-tool rules for which guest files or directories matter

Current adapters:

- Claude subscription login
- Codex ChatGPT/device-style login state
- GitHub CLI login state

## Startup Path

When you run `fascinate serve`, the flow is:

1. `cmd/fascinate/main.go` loads the env file and config.
2. `app.New` creates the app.
3. `database.Open` opens SQLite.
4. `store.Migrate` runs migrations.
5. `cloudhypervisor.New` prepares the runtime manager and ensures directories and guest SSH key material exist.
6. `toolauth.NewStore` prepares encrypted auth storage.
7. `toolauth.NewManager` registers adapters.
8. `controlplane.New` constructs the service.
9. `controlPlane.EnsureLocalHost` makes sure the current box exists in the host registry.
10. `controlPlane.HeartbeatLocalHost` publishes initial host health and capacity.
11. `controlPlane.ReconcileRuntimeState` aligns DB state with runtime state.
12. `httpapi.New` builds the HTTP handler.
13. `signup.New` builds the signup service.
14. `sshfrontdoor.New` builds the SSH server.
15. `App.Run` starts HTTP, SSH, the periodic tool-auth sync loop, the periodic runtime reconcile loop, and the periodic local-host heartbeat loop.

This matters because if startup is broken, the issue is often in one of those boundaries:

- config
- filesystem preparation
- DB migration
- runtime initialization
- reconciliation

## The Data Model

You should be comfortable with the core records because many bugs are really "DB truth vs runtime truth" bugs.

### Users

`users` holds:

- identity by email
- whether the user is admin
- whether the tutorial was completed

Important note: `is_admin` is persisted, but it is not currently the main enforcement mechanism for front-door permissions.

### SSH keys

`ssh_keys` maps public keys to users.

The SSH server authenticates by fingerprint lookup. This is why `seed-ssh-key` and signup both end by creating a DB key record.

### Machines

`machines` holds product-facing machine records.

Important fields:

- `name`: the user-facing machine name
- `owner_user_id`
- `host_id`: which registered host owns the machine
- `runtime_name`: the name used by the runtime
- `source_snapshot_id`: optional source snapshot for snapshot-based machine creation
- `state`
- `primary_port`
- `deleted_at`

Important nuance: the initial schema used `incus_name`, but a later migration renamed it to `runtime_name`. That is a hint about the project's history.

### Snapshots

`snapshots` holds saved snapshot records.

Important fields:

- `name`: user-facing snapshot name
- `host_id`: which registered host owns the snapshot
- `runtime_name`: runtime-facing snapshot identifier
- `source_machine_id`
- `state`
- `artifact_dir`
- `disk_size_bytes`
- `memory_size_bytes`
- `runtime_version`
- `firmware_version`

Critical nuance:

- users identify a snapshot by `name`
- the runtime identifies it by `runtime_name`

In current behavior, `runtime_name` is a generated UUID-like value, while `name` is the friendly per-user name.

That difference is one of the most important details in the whole repo.

### Hosts

`hosts` holds the operator-visible registry of VM-capable Fascinate hosts.

Important fields:

- `id`
- `name`
- `region`
- `role`
- `status`
- `heartbeat_at`
- `total_*` and `allocated_*` capacity fields
- `available_disk_bytes`
- `machine_count`
- `snapshot_count`
- `last_error`

Important nuance:

- a host can be registered but not placement-eligible
- `placement_eligible` is a derived operator-facing concept, not a raw DB column

At the moment, host placement eligibility effectively means:

- host status is active
- heartbeat is fresh
- the host can fit a default-size Fascinate machine right now

## Machine States And What They Mean

The code uses a mix of product-level and runtime-level state.

### Product-level states

In the control plane you will commonly see:

- `CREATING`
- `RUNNING`
- `FAILED`
- `missing`
- `deleted`

Not all of these come from constants in one place. Some are inferred or written directly.

### Runtime-level states

The runtime itself is simpler:

- `RUNNING` if the recorded VMM process is alive
- `STOPPED` otherwise

This means the control plane has richer semantics than the runtime.

For example:

- `CREATING` is a control-plane concept
- the runtime does not report `CREATING`

That explains why `shouldAdoptRuntimeState` refuses to overwrite a DB machine record while it is still marked `CREATING`.

## The Fresh Machine Create Flow

This is the most important control-plane flow to understand.

### High-level story

1. Validate the machine name.
2. Normalize and validate owner email.
3. Lock per-user mutations.
4. Enforce quota and size policy.
5. Ensure the user exists.
6. Resolve an owning host.
7. Optionally sync tool auth from the user's other running machines.
8. Insert a machine record with state `CREATING`.
9. Launch async runtime creation in a goroutine.
10. After the VM becomes reachable, restore tool auth into fresh machines.
11. Mark the machine `RUNNING` or `FAILED`.

For fresh machines, host selection is now host-aware. In the current one-box setup that still resolves to the local host, but the control plane no longer assumes the local runtime is globally authoritative.

### Why creation is async

VM creation can take time:

- disk creation
- cloud-init bootstrapping
- package installation and tool install during first boot
- guest SSH readiness checks

So the control plane persists intent first and finishes in the background.

### What the runtime does during fresh creation

For a non-snapshot create, the runtime:

1. creates a machine directory
2. prepares machine metadata
3. creates an overlay qcow2 disk backed by the base image
4. writes a cloud-init seed image
5. creates the machine's network namespace and devices
6. starts `cloud-hypervisor` inside that namespace
7. starts host-side app and SSH forwarders
8. waits until the guest is reachable and developer tools are installed

That final readiness check is stronger than "SSH is open". It checks for things like:

- cloud-init completion
- `claude`
- `codex`
- `gh`
- `node`
- `go`
- `docker`

That tells you this product considers a machine "ready" only when it is a usable dev box.

## Snapshot-Based Create Flow

When a user creates from a snapshot, the flow changes.

### High-level story

1. Resolve the user-facing snapshot name in SQLite.
2. Confirm the snapshot is `READY`.
3. Resolve the owning host from the snapshot record.
4. Persist the new machine record with `source_snapshot_id`.
5. Queue background creation.
6. During queued creation, load the snapshot record and pass its `runtime_name` into the runtime request.
7. Dispatch the restore through the owning host's executor.
8. The runtime restores from snapshot artifacts instead of booting from the base image.
9. The machine becomes ready only after the restored guest is reachable again.

### Critical behavior difference

Fresh machines get post-create tool-auth restore.

Snapshot-created machines do not.

Why? Because the restored snapshot state is treated as authoritative. The system does not layer a fresh tool-auth restore on top of a snapshot restore.

That is both a product decision and a correctness invariant.

## Fork Flow

Fork is not implemented as "copy a machine record and disk."

It is implemented as:

1. create a temporary implicit snapshot
2. keep the operation on the source machine's owning host
3. restore that snapshot into a new runtime machine
4. persist the new machine record
5. remove the temporary snapshot artifact

This is why fork preserves live environment state.

It is also why fork behaves differently from ordinary create:

- ordinary create persists first, then provisions
- fork provisions first, then persists

That asymmetry matters for failure handling. On fork, if DB persistence fails after runtime success, the control plane cleans up the runtime machine.

## Snapshot Flow

Snapshot creation is another async control-plane flow.

### High-level story

1. Validate ownership and snapshot name.
2. Resolve the source machine's owning host.
3. Generate a runtime snapshot name.
4. Insert a DB record with state `CREATING` and an artifact directory path.
5. Start background snapshot creation.
6. Dispatch snapshot creation through the owning host's executor.
7. The runtime pauses the VM.
8. Cloud Hypervisor writes snapshot restore artifacts.
9. Fascinate copies the VM disk and seed image into the snapshot artifact directory.
10. Fascinate rewrites restore config paths so the snapshot artifact is self-contained.
11. The VM is resumed.
12. The control plane records artifact sizes and marks the snapshot `READY`.

### Why the pause/resume matters

This is a true VM snapshot flow, not a filesystem backup.

Memory and device state are involved, so there is a deliberate pause point around snapshot creation.

## Delete Flow

Delete is simpler, but still has important details.

1. Lock per-user mutations.
2. Load the owned machine record.
3. Cancel any in-flight create if one exists.
4. Best-effort capture tool auth from the machine.
5. Delete the runtime machine.
6. Soft-delete the DB record by setting `state = 'deleted'` and `deleted_at`.

The runtime cleanup includes:

- stopping forwarders
- killing the VM process group
- deleting namespace networking
- removing the machine directory

## Reconciliation: The Glue Between DB And Reality

`ReconcileRuntimeState` is one of the most important methods in the whole codebase.

It runs on startup and does three jobs:

1. delete runtime machines that exist on disk but have no DB record
2. restart background creation for DB machines still marked `CREATING` but not present in the runtime
3. update DB machine state to match runtime state when appropriate

This is crash recovery logic.

Without it, the system would get stuck whenever the process died mid-create or mid-runtime mutation.

There is now also a periodic reconcile loop in `app.Run`, so recovery is no longer just a startup concern.

If you are debugging a mismatch between UI/API state and host reality, always ask:

- what does SQLite say?
- what does the runtime metadata directory say?
- what does reconciliation do on startup?

## Networking Deep Dive

This is the most "systems" part of the repo.

### The problem the code is solving

Fascinate wants:

- each VM isolated from others
- guest traffic to reach the internet
- users to reach the guest's SSH and app ports
- snapshot restores and forks to preserve guest network identity

Those goals are slightly in tension, so the design matters.

### The design

Each VM gets its own Linux network namespace.

Inside that namespace:

- there is a bridge, typically `br0`
- there is a tap device for the VM, typically `tap0`
- there is an uplink veth endpoint, typically `uplink0`

Outside, on the host:

- there is the host side of the veth pair
- the host enables forwarding and NAT
- the host runs per-machine forwarders into the namespace

### Why a namespace per VM?

Because snapshot restore can preserve guest IP and MAC state.

If every guest had to be globally unique in the root namespace, restore and fork would be messier. By isolating each VM inside its own namespace, Fascinate can keep guest identity stable while still routing to each VM safely from the host.

### What the forwarders do

The guest is not exposed directly to the host root namespace on a stable global IP/port.

Instead, Fascinate runs helper processes that:

- listen on `127.0.0.1:<random-port>` on the host
- forward traffic into the guest's namespace IP and target port

There are at least two forwarders per machine:

- app forwarder
- SSH forwarder

That is why the runtime machine record reports:

- `AppHost: 127.0.0.1`
- `AppPort: <forwarded port>`
- `SSHHost: 127.0.0.1`
- `SSHPort: <forwarded port>`

### Why wildcard app routing works

The HTTP server uses the machine name from the incoming host, like:

- `demo.fascinate.dev`

It looks up the machine, resolves its owning host, finds that host's runtime app endpoint, and reverse proxies to the host-side forwarder.

That means public routing is based on machine identity, not on the guest having a unique host-visible IP.

## Guest Identity And Cloud-Init

Fresh boots and restored boots differ.

### Fresh boot

Fresh boot uses:

- overlay disk backed by base image
- cloud-init seed image
- first-boot package and tool installation

### Snapshot restore

Snapshot restore uses:

- copied snapshot disk
- copied restore directory
- rewritten restore config
- restored machine metadata
- preserved guest MAC identity

Then Fascinate refreshes machine identity details in the guest, including host-facing instructions for Claude workflows.

## SSH Front Door Deep Dive

This is the user experience layer.

### Authentication

The SSH server checks the client's public key fingerprint against SQLite.

Possible outcomes:

- known key: authenticated as a user
- unknown key and signup enabled: authenticated into signup-required mode
- unknown key and signup disabled: rejected

### Session types

The SSH server supports:

- interactive `shell`
- exec requests
- PTY and window resizing

### Exec-style commands

Users can run commands like:

- `machines`
- `snapshots`
- `create <name>`
- `create <name> --from-snapshot <snapshot>`
- `fork <source> <target>`
- `snapshot save <machine> <name>`
- `snapshot delete <name>`
- `delete <name> --confirm <name>`
- `shell <name>`
- `tutorial <name>`

### Interactive flow

Interactive sessions show the Bubble Tea dashboard. From there users can:

- browse machines
- browse snapshots
- create machines
- create from a selected snapshot
- fork a running machine
- save snapshots
- delete machines or snapshots
- enter a guest shell
- start a tutorial flow

### Guest shell handoff

When a user enters a machine shell, Fascinate:

1. looks up the machine and confirms it is `RUNNING`
2. waits briefly for guest SSH readiness if needed
3. runs the host's `ssh` binary to connect to the guest via the forwarder
4. attaches the user's session PTY to that child SSH process
5. after exit, captures tool-auth state from the guest

This is a very important idea: the SSH front door is not the same as guest SSH. It is a broker.

## Tool Auth Deep Dive

This is one of the codebase's most distinctive features.

### The problem

Users do not want to log into Claude, Codex, or GitHub CLI again every time they make a new VM.

### The design

Fascinate stores tool auth as encrypted per-user profiles on the host.

Profiles are keyed by:

- user
- tool
- auth method

That keying prevents cross-user or cross-tool leakage.

### What gets stored

The current implementation uses session-state adapters. That means it captures opaque files or directories from the guest rather than trying to parse proprietary auth formats.

Examples:

- Claude: `.claude.json` and `.claude/`
- Codex: `.codex/`
- GitHub CLI: `.config/gh`, `.gitconfig`, `.git-credentials`

### Security model

Tool auth bundles are encrypted at rest using AES-GCM with a host-side 32-byte key stored at `FASCINATE_TOOL_AUTH_KEY_PATH`.

Important operational consequence:

if you lose or rotate that key incorrectly, old stored auth bundles cannot be read.

### Capture timing

Capture happens in two modes:

- exact capture after a user shell or tutorial session
- non-destructive background capture for running machines

The non-destructive mode will preserve an existing non-empty stored profile if a capture comes back empty.

That prevents an incidental empty read from wiping a good stored login.

### Restore timing

Restore happens before a fresh machine becomes `RUNNING`.

That means the user's first shell in a later VM should already have their supported tool auth available.

## The TUI: What The Product Thinks Matters

The dashboard is useful not just as a UI, but as a product specification in code.

By reading `internal/tui/dashboard.go`, you learn what actions the product wants to foreground:

- new machine
- enter shell
- tutorial
- fork
- snapshot
- delete

You also learn the intended state gating:

- only `RUNNING` machines can be shelled into
- only `RUNNING` machines can be forked
- only `RUNNING` machines can be snapshotted
- `CREATING` resources auto-refresh in the UI

This is a good place to look when you are unsure what behavior is product-correct versus merely technically possible.

The TUI remains host-agnostic from the user's point of view. That is deliberate: host IDs are now important internally and in diagnostics, but normal user flows still treat the product as "machines and snapshots" rather than "hosts and placement."

## Reading Order For The Codebase

If you want the fastest path to competence, read in this order.

1. `README.md`
2. `AGENTS.md`
3. `cmd/fascinate/main.go`
4. `internal/app/app.go`
5. `internal/config/config.go`
6. `internal/runtime/runtime.go`
7. `internal/controlplane/hosts.go`
8. `internal/controlplane/service.go`
9. `internal/httpapi/server.go`
10. `internal/sshfrontdoor/server.go`
11. `internal/runtime/cloudhypervisor/runtime.go`
12. `internal/runtime/cloudhypervisor/network.go`
13. `internal/runtime/cloudhypervisor/snapshots.go`
14. `internal/toolauth/manager.go`
15. `internal/toolauth/store.go`
16. `internal/tui/dashboard.go`
17. `internal/signup/service.go`
18. `internal/database/migrations/*.sql`
19. `internal/controlplane/hosts_test.go`
20. `internal/controlplane/service_test.go`
21. `internal/sshfrontdoor/server_test.go`
22. `internal/httpapi/server_test.go`

That order intentionally alternates between product-facing code and systems-facing code.

## The Most Important Invariants

These are the rules you should actively protect when changing the code.

### Snapshot identity invariant

Friendly snapshot names are not runtime snapshot names.

The user-facing name lives in SQLite as `snapshots.name`.

The runtime artifact directory is keyed by `snapshots.runtime_name`.

If you blur those two concepts, you will create restore bugs.

### Create-state invariant

The DB should not prematurely adopt runtime state while a machine is still `CREATING`.

That is why the control plane does not overwrite `CREATING` with a runtime-reported state until create finalization succeeds.

### Ownership invariant

Users can only act on their own machines and snapshots, and lifecycle operations must resolve through the owning host.

Ownership checks live in the control plane and DB access patterns, not only in outer transport layers.

### Host locality invariant

Machines and snapshots now have explicit host ownership.

That means:

- restore from snapshot should run on the snapshot's host
- fork should stay on the source machine's host
- shell, routing, and diagnostics should resolve through the owning host

Even if only one host exists today, this invariant is now part of the architecture.

### Restore-authority invariant

Snapshot-created and forked machines treat restored state as authoritative.

Do not add fresh-boot-only behavior on top of restored machines unless the product explicitly wants it.

### Tool-auth isolation invariant

Stored tool auth is always scoped by user, tool, and auth method.

There must never be a fallback that guesses or substitutes another user's profile.

### Namespace-routing invariant

Public reachability depends on per-machine host-side forwarding and proxying, not on globally unique guest IPs visible in the root namespace.

### No-Incus invariant

Do not reintroduce Incus or container-era assumptions. This project is now explicitly Cloud Hypervisor based.

## Common Places New Contributors Get Confused

### "Why are there two machine states?"

Because product state and runtime state are different concerns.

SQLite tracks workflow semantics like `CREATING` and `FAILED`.
The runtime only knows if a VM process is alive or not.

### "Why does the runtime use metadata JSON files?"

Because it needs durable host-side state that exists independently of SQLite and directly supports runtime operations like restart, cleanup, forwarder recovery, snapshot creation, and restore.

### "Why is fork not async like create?"

Because the implementation currently creates the runtime fork first and persists the DB record second. That is a deliberate trade-off, but it means fork and create have different failure shapes.

### "Why is guest readiness so strict?"

Because this product is selling "developer machine ready for real work," not merely "VM booted."

### "Why does the host SSH into the guest?"

Because that is how Fascinate:

- opens user shells
- checks guest readiness
- captures tool-auth state
- restores tool-auth state
- refreshes guest identity instructions

## Tests That Matter Most

You do not need to memorize every test file, but you should know where truth lives.

### Best tests for control-plane behavior

- `internal/controlplane/hosts_test.go`
- `internal/controlplane/service_test.go`

These are the most important test files for host-aware placement, lifecycle behavior, reconciliation, and tool-auth integration.

### Best tests for SSH and UX behavior

- `internal/sshfrontdoor/server_test.go`
- `internal/tui/dashboard_test.go`
- `internal/tui/signup_test.go`

### Best tests for HTTP behavior

- `internal/httpapi/server_test.go`

### Best tests for persistence behavior

- `internal/database/database_test.go`
- `internal/config/envfile_test.go`

### Best tests for runtime behavior

- `internal/runtime/cloudhypervisor/runtime_test.go`

When in doubt, read tests before changing code. They often reveal the intended semantics more clearly than the implementation itself.

## Ops And Host Scripts

Do not ignore `ops/`.

Even if you are only changing Go code, the host scripts tell you what the runtime needs from the machine it runs on.

Important scripts:

- `ops/host/bootstrap.sh`
- `ops/host/install-control-plane.sh`
- `ops/host/verify.sh`
- `ops/host/smoke.sh`
- `ops/host/stress.sh`
- `ops/host/benchmark.sh`
- `ops/host/diagnostics.sh`
- `ops/host/smoke-snapshots.sh`
- `ops/host/smoke-tool-auth.sh`
- `ops/cloudhypervisor/build-base-image.sh`

These scripts teach you:

- what packages and kernel features the host needs
- how the base guest image is prepared
- what end-to-end behaviors are important enough to smoke-test

One nuance worth knowing now: `ops/host/smoke-tool-auth.sh` has evolved into a more targeted persistence and diagnostics harness. It is especially useful when you are changing tool-auth behavior, but it is not the general-purpose first smoke to run after every unrelated runtime change.

## How To Contribute Intelligently

When you start changing code, use this decision process.

### If the change is about user-visible machine behavior

Start in:

- `internal/controlplane/service.go`
- the relevant tests in `internal/controlplane/service_test.go`

Then trace into:

- `internal/runtime/runtime.go`
- `internal/runtime/cloudhypervisor/...`

### If the change is about shell access or signup

Start in:

- `internal/sshfrontdoor/server.go`
- `internal/signup/service.go`
- `internal/tui/...`

### If the change is about app routing or API behavior

Start in:

- `internal/httpapi/server.go`

### If the change is about persisted auth

Start in:

- `internal/toolauth/manager.go`
- `internal/toolauth/store.go`
- the relevant adapter file

Then inspect where the control plane calls:

- `RestoreAll`
- `CaptureAll`
- `CaptureAllNonDestructive`

### If the change is about state mismatches, orphan VMs, or boot recovery

Start in:

- `internal/controlplane/service.go`

Specifically the reconciliation path.

### If the change is about hosts, placement, heartbeats, or cross-host readiness

Start in:

- `internal/controlplane/hosts.go`
- `internal/controlplane/service.go`
- `internal/database/hosts.go`

Then read the matching host and diagnostics tests.

### If the change is about networking, snapshots, restore, or host-level behavior

Start in:

- `internal/runtime/cloudhypervisor/network.go`
- `internal/runtime/cloudhypervisor/snapshots.go`
- `ops/host/*`

## How To Debug This Codebase

When behavior seems wrong, ask these questions in order.

### 1. What does the DB think?

Check:

- the host record
- the machine row
- the snapshot row
- the user row

Especially:

- host ownership
- state
- owner
- runtime name
- soft-deletion fields

### 2. What does the runtime think?

Check:

- machine metadata under `data/machines/<name>/machine.json`
- snapshot metadata under `data/snapshots/<runtime-name>/snapshot.json`
- whether the `cloud-hypervisor` process is alive
- whether the forwarder PIDs are alive

If you are debugging a host-aware issue, also check the diagnostics endpoints before dropping into manual host forensics:

- `/v1/diagnostics/hosts`
- `/v1/diagnostics/machines/{name}`
- `/v1/diagnostics/snapshots/{name}`
- `/v1/diagnostics/tool-auth`
- `/v1/diagnostics/events`

### 3. Is the mismatch expected by design?

Examples:

- DB says `CREATING`, runtime says `RUNNING` while create finalization has not happened yet
- DB says `RUNNING`, runtime is gone, and the service updates state to `missing`

### 4. Does startup reconciliation fix it?

If yes, you may be looking at an expected crash-recovery path.

### 5. Is the bug product-level or runtime-level?

That question tells you where to patch:

- control plane
- runtime
- SSH/API transport
- UI only

## A Good First Week Plan

If you want a practical ramp-up path, do this.

### Day 1

- Read `README.md`, `AGENTS.md`, and this guide.
- Read `cmd/fascinate/main.go` and `internal/app/app.go`.
- Read the DB migrations.

### Day 2

- Read `internal/controlplane/service.go` end to end.
- Read `internal/controlplane/service_test.go`.
- Write down the machine lifecycle in your own words.

### Day 3

- Read `internal/runtime/cloudhypervisor/runtime.go`.
- Read `network.go` and `snapshots.go`.
- Draw the namespace and forwarder architecture on paper.

### Day 4

- Read `internal/sshfrontdoor/server.go`, `internal/tui/dashboard.go`, and `internal/signup/service.go`.
- Follow one user session from SSH auth to dashboard to guest shell to tool-auth sync.

### Day 5

- Read `internal/toolauth/*`.
- Read the tool-auth spec in `openspec/specs/persistent-tool-auth/spec.md`.
- Trace exactly when capture and restore happen.

### Day 6

- Read `internal/httpapi/server.go` and its tests.
- Read the snapshot spec in `openspec/changes/add-vm-snapshots/specs/vm-snapshots/spec.md`.

### Day 7

- Pick one tiny change.
- Before coding, explain to yourself which layer owns the behavior.
- Add or update tests before or alongside the change.

## Good Starter Contributions

These are the kinds of tasks that teach the codebase well.

- Improve error messages in SSH or HTTP flows.
- Add a missing test for a state transition.
- Tighten validation around machine or snapshot naming.
- Improve docs where behavior in code is subtle.
- Add observability or logging around reconciliation or tool-auth sync.
- Improve dashboard clarity around pending machine or snapshot states.

## Risky Changes For New Contributors

These are worth approaching carefully.

- anything touching snapshot restore config rewriting
- anything touching network namespace setup and teardown
- changes to state transition rules
- changes to create vs fork semantics
- changes to tool-auth capture/restore timing
- schema or persisted-format changes
- host bootstrap or deploy changes

## Questions To Ask Before Any Medium-Sized Change

1. Which layer should own this behavior: transport, control plane, runtime, or storage?
2. Is the source of truth SQLite, runtime metadata, or both?
3. Does this affect fresh create, snapshot create, fork, delete, or reconcile?
4. Does this affect shell access, HTTP reachability, or both?
5. Does this affect tool-auth timing?
6. What test should fail before I change the code?
7. Does the README or an active spec need updating too?

## Commands You Should Memorize

Development:

```bash
make run
make build
go test ./...
go test ./internal/controlplane/...
go test ./internal/runtime/cloudhypervisor/...
go test ./internal/sshfrontdoor/...
go test ./internal/httpapi/...
```

Ops validation:

```bash
make verify-ops
```

Operator-oriented helpers:

```bash
sudo ./ops/host/diagnostics.sh hosts
sudo ./ops/host/diagnostics.sh machine you@example.com machine-name
sudo ./ops/host/diagnostics.sh snapshot you@example.com snapshot-name
sudo ./ops/host/diagnostics.sh tool-auth you@example.com
sudo ./ops/host/diagnostics.sh events you@example.com 100
```

Live validation harnesses:

```bash
sudo ./ops/host/smoke.sh
sudo ./ops/host/stress.sh
sudo ./ops/host/benchmark.sh
sudo ./ops/host/smoke-snapshots.sh
sudo ./ops/host/smoke-tool-auth.sh
```

Local API smoke:

```bash
curl http://127.0.0.1:8080/healthz
curl http://127.0.0.1:8080/readyz
curl http://127.0.0.1:8080/v1/runtime/machines
curl http://127.0.0.1:8080/v1/machines?owner_email=you@example.com
```

SSH usage:

```bash
./bin/fascinate seed-ssh-key --email you@example.com --name laptop --public-key-file ~/.ssh/id_ed25519.pub
ssh -p 2222 localhost
ssh -p 2222 localhost machines
ssh -p 2222 localhost shell my-machine
```

## Final Mental Model

If you only keep one deep model in your head, make it this:

Fascinate is a stateful translator between product intent and VM reality.

The product side says:

- who owns what
- what should exist
- what state a machine or snapshot should be in
- when a user should be allowed to act
- which host owns each machine and snapshot
- whether a host is currently eligible for new default-size placement

The infrastructure side says:

- what VM processes exist
- what disks and snapshot artifacts exist
- what networking exists
- whether a guest is actually reachable

Most of the interesting code is about keeping those two sides aligned without exposing the complexity to the user.

Once you can spot which side a bug belongs to, contributing becomes much easier.

## If You Want To Go Even Deeper

After finishing this guide, the best next step is not reading more random files. It is tracing one full story at a time:

1. Fresh create: SSH command or dashboard action all the way to `RUNNING`.
2. Snapshot create: user action all the way to saved artifact directory.
3. Fork: source machine to implicit snapshot to restored target.
4. Shell session: front door auth to guest shell to tool-auth capture.
5. Crash recovery: partial create to startup reconciliation.

If you can explain each of those out loud, you are ready to contribute productively.
