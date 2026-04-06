## Context

Fascinate now has per-user aggregate budgets, but it still behaves like a fixed-size VM product because every fresh machine is created from one hidden default shape and immediately counts its full CPU/RAM reservation against the user's quota while it is running. That makes the current `2 vCPU / 8 GiB / 50 GiB / 25 VMs` policy feel broken in practice: two default running VMs exhaust CPU headroom even though the UX we want is "many retained machines that share one pool."

The current codebase already gives us several useful primitives:
- machine records persist CPU, memory, and disk metadata
- runtime metadata already distinguishes `RUNNING` from `STOPPED` based on whether the VMM process is alive
- the Cloud Hypervisor manager already has a start path that can boot an existing machine from persisted metadata
- host admission, diagnostics, and browser state transitions are explicit and test-driven

The missing pieces are:
- a first-class stop/start lifecycle exposed through the control plane and browser
- quota rules that charge CPU/RAM only for actively running machines
- a disk-accounting policy that keeps retained machines practical under hidden sizing
- diagnostics and UI behavior that make hidden sizing legible without exposing a manual sizing workflow

Important constraints:
- no user-facing size picker
- no boost mode
- keep the one-box deployment safe under real host capacity limits
- keep snapshots and true fork semantics intact
- preserve honest machine readiness; stopped or starting machines must not look shell-ready

## Goals / Non-Goals

**Goals:**
- Deliver a hidden-sizing UX where users can keep many machines without manually choosing CPU, RAM, or disk sizes.
- Make CPU and RAM a shared per-user running-state budget instead of a per-created-VM reservation.
- Add explicit stop/start machine lifecycle semantics so retained machines can free compute without being deleted.
- Keep disk and snapshot limits enforceable while making retained stopped machines practical.
- Apply the hidden-sizing model consistently across create, start, fork, restore, UI actions, and diagnostics.
- Keep operator-visible rejection reasons and budget diagnostics clear enough to explain why a machine can or cannot start.

**Non-Goals:**
- Adding a size picker, per-VM tuning panel, or boost/focus mode.
- Building idle auto-stop in the first pass.
- Implementing dynamic CPU/RAM hot-resize after a machine already exists.
- Replacing the current one-box host model with a new multi-host scheduler.
- Building a billing/plans UI or self-serve quota editor.

## Decisions

### 1. Hidden sizing stays fixed at creation time, but compute budgeting becomes running-state-aware

Fresh machines will still be created from one hidden baseline size. Clone and snapshot-restore flows will continue to inherit the source machine's shape rather than exposing a size decision. The major behavior change is quota accounting:
- `CREATING` and `STARTING` machines reserve CPU and RAM
- `RUNNING` machines consume CPU and RAM
- `STOPPING`, `STOPPED`, `FAILED`, and deleted machines do not consume CPU or RAM
- retained machine disks continue to count toward disk usage

Why:
- This preserves the simple "new machine" UX.
- It solves the user's current problem without inventing hidden rebalancing across existing VMs.
- It aligns with how developers think about retained environments: inactive machines should not block active compute.

Alternatives considered:
- Divide the total compute budget evenly across all machines.
  - Rejected because every new machine would implicitly shrink older machines and require resize semantics we do not have.
- Keep current reservation-style accounting and only improve error messaging.
  - Rejected because it does not change the broken UX.
- Base CPU/RAM quotas on instantaneous live utilization.
  - Rejected because RAM especially needs admission-time guarantees, and live utilization is too bursty to be a hard quota source.

### 2. Introduce first-class machine power states and lifecycle operations

The control plane will add explicit user-facing stop/start lifecycle operations and power-state-aware state transitions:
- `CREATING`
- `STARTING`
- `RUNNING`
- `STOPPING`
- `STOPPED`
- `FAILED`
- `DELETING`

Stop will terminate the guest process and host forwarders while preserving machine metadata, root disk, managed env state, and ownership. Start will reuse the persisted machine metadata, re-establish networking/forwarders, and wait for the guest to become honestly usable before transitioning to `RUNNING`.

Why:
- Hidden shared compute only works if users can retain machines without paying active CPU/RAM costs forever.
- The existing runtime already has most of the mechanics needed to start a stopped machine, and its internal stop helper can be promoted into a first-class operation.
- Explicit power states keep the browser and diagnostics honest.

Alternatives considered:
- Auto-stop only, with no manual stop/start.
  - Rejected because users need an immediate and predictable way to manage compute headroom.
- Treat a stopped machine as deleted and recreate it from snapshots.
  - Rejected because that loses the straightforward "pause and resume this VM" mental model.

### 3. Track retained machine disk by actual machine-overlay usage, not full configured disk ceiling

Under hidden sizing, per-machine configured disk ceilings are a poor user-facing quota signal. The new model will keep two disk concepts:
- an internal machine disk ceiling used to provision or grow the qcow2 root disk
- an actual retained machine disk usage value used for per-user disk budgets and diagnostics

Per-user disk usage will become:
- current retained machine overlay bytes for each machine
- plus actual snapshot artifact bytes for retained snapshots

This requires persisting a machine disk-usage field and refreshing it from the host filesystem during lifecycle work and diagnostics/reconcile flows. Host-level free-disk headroom remains a separate hard guardrail.

Why:
- Counting the full configured disk ceiling for every retained machine makes hidden sizing feel fake immediately.
- The runtime already uses qcow2 sparse disks and backing images, so actual retained bytes better match the real product experience.
- Snapshot artifacts already use actual persisted bytes; machine overlays should follow the same user-facing principle.

Alternatives considered:
- Keep counting full configured machine disk allocation.
  - Rejected because users would still hit the disk budget after a handful of retained machines even if those machines barely consume storage.
- Drop user disk budgeting and rely only on host free-disk checks.
  - Rejected because it removes per-user fairness and makes one user's accumulation invisible until the host is nearly full.

### 4. Start admission becomes the compute gate; create admission covers the initial boot only

Create, fork, and restore continue to admit against CPU/RAM because those flows boot a machine immediately. After creation, later reuse of that compute budget is controlled through `start` admission:
- a stopped machine may always remain retained if its disk still fits within the user's disk budget
- starting that machine must succeed against current user CPU/RAM headroom and host capacity
- stopping it releases CPU/RAM budget back to the user pool

Why:
- This is the cleanest mapping from "shared compute pool" to explicit lifecycle operations.
- It avoids hidden background rebalancing or opportunistic overcommit.
- It keeps admission logic consistent across fresh create and later restart.

Alternatives considered:
- Allow users to create an arbitrary number of powered-off machines even if the initial create would not fit compute.
  - Rejected for the first pass because it would require a create-without-boot path and different readiness semantics.

### 5. Shell, fork, snapshot, and routing remain strictly power-state-aware

Machine actions will become power-state-gated:
- `RUNNING`: shell, route resolution, fork, snapshot, delete, and stop are allowed subject to existing ownership and quota checks
- `STOPPED`: start and delete are allowed; shell, route resolution, fork, and snapshot are rejected
- `CREATING`, `STARTING`, `STOPPING`, `DELETING`: user actions are suppressed or rejected except where explicitly safe

Browser UI will not show a manual size model. It will instead expose honest power state and the actions that make sense in that state.

Why:
- Hidden sizing must not leak through inconsistent affordances.
- Snapshot/fork semantics rely on a live source VM today and should stay explicit.
- Preventing shell or route access to stopped machines avoids the same kind of premature readiness bug we already had to fix for fresh creates.

Alternatives considered:
- Allow fork or snapshot from stopped machines.
  - Rejected for the first pass because it complicates the semantics and is not needed to deliver the core UX.

### 6. Diagnostics must separate running compute from retained storage

Per-user diagnostics will report at least:
- configured CPU, RAM, disk, machine-count, and snapshot-count limits
- running CPU/RAM usage
- retained machine disk usage
- retained snapshot disk usage
- running machine count
- stopped machine count
- remaining startable compute headroom

Machine diagnostics will include power state and current retained disk usage. Rejection errors should explain whether the user needs to stop another machine, delete storage, or simply wait for a transitional state to complete.

Why:
- Hidden sizing only works if operators and users can still understand the state machine.
- The system needs clear answers to "why can't I start another VM?" without showing raw sizing controls.

Alternatives considered:
- Keep the current diagnostics shape and only change backend accounting.
  - Rejected because the resulting UX would still be opaque.

## Risks / Trade-offs

- **[Actual machine disk usage is more dynamic than fixed disk ceilings]** -> Persist a dedicated disk-usage field, refresh it during lifecycle transitions and diagnostics/reconcile, and keep host free-disk headroom as the hard safety backstop.
- **[Adding stop/start increases lifecycle-state complexity]** -> Use explicit `STARTING` and `STOPPING` states, keep state transitions narrow, and add end-to-end tests for success, failure, and rollback paths.
- **[Stopped machines can accumulate and surprise users on disk usage]** -> Surface retained machine disk usage clearly in diagnostics and browser messaging, and keep disk rejections explicit.
- **[Start admission can race under concurrent requests]** -> Reuse the existing host-scoped admission lock so concurrent starts and creates cannot over-reserve compute on the one-box host.
- **[Power-state-aware UI changes can regress shell or delete flows]** -> Add browser and API coverage for all machine-card actions across `RUNNING`, `STOPPED`, `CREATING`, `STARTING`, `STOPPING`, and `DELETING`.

## Migration Plan

1. Add persisted machine power-state support and machine disk-usage tracking to the data model.
2. Extend the runtime and host-executor interfaces with a first-class stop operation and reuse the existing start path for stopped machines.
3. Replace reservation-style compute accounting with running-state compute accounting in user-budget and host-admission helpers.
4. Update create/fork/restore/start flows to reserve compute only while a machine is booting or running, and release it on successful stop.
5. Update machine disk accounting to use retained overlay usage plus snapshot artifact usage for user disk budgets.
6. Add start/stop HTTP endpoints and browser actions, then update machine-card UX to reflect power-state-aware actions without showing machine sizes.
7. Update diagnostics, README, and operator tooling to explain hidden sizing, running-state compute budgets, retained storage usage, and the lack of boost mode.
8. Validate locally, then validate on the live host with create, stop, start, shell, fork, snapshot, delete, and budget-edge cases.

Rollback:
- Revert the code and schema changes together.
- If the new machine disk-usage field or power-state logic proves unstable, fall back to the current reservation-style model before re-enabling user creation on the host.

## Open Questions

- What hidden baseline root-disk ceiling should fresh machines use once user-facing disk budgeting is based on retained overlay bytes instead of full configured disk reservation?
- Should the first browser surface show aggregate budget headroom directly, or is diagnostics plus improved rejection copy enough for the first rollout?
