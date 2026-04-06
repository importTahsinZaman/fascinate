## Context

Fascinate now has hidden VM sizing, explicit power states, and retained-storage accounting, but active CPU is still enforced as a hard reserved-per-VM budget. On the current OVH host, that means two default `1 vCPU / 2 GiB` machines exhaust a user's `2 vCPU` budget even though the machine is intended to feel like a shared-pool developer environment rather than a strict per-VM reservation product.

The live host we are sizing for today has:
- `24` logical CPUs
- about `125 GiB` RAM
- about `877 GiB` usable disk
- a configured host free-disk floor of `150 GiB`

The target operating point for this design is roughly:
- `8` concurrent users
- each user able to keep up to `25` retained VMs and `5` retained snapshots
- each user able to run about `5` default machines concurrently without tripping a hard per-user CPU wall

The recent hidden-sizing refactor already gives us useful primitives:
- explicit machine power states and start/stop flows
- retained machine disk accounting
- per-user hard RAM and disk budgets
- host-scoped admission locks
- diagnostics and smoke coverage around honest readiness and lifecycle transitions

The remaining mismatch is CPU policy. Today the control plane still treats CPU like a hard reserved quantity for every active VM. That is too conservative for the economics and UX we want on a single box full of developer workloads that are bursty rather than uniformly saturated.

Important constraints:
- preserve hidden sizing; no size picker and no boost mode
- keep RAM and retained storage as hard, understandable limits
- keep the current one-box deployment safe under noisy-neighbor conditions
- avoid a redesign of the VM runtime architecture
- there are no active users to preserve, so persisted quota semantics can change cleanly

## Goals / Non-Goals

**Goals:**
- Replace hard per-user CPU reservation with a shared CPU pool model that allows materially higher active-VM concurrency.
- Keep hard per-user RAM, retained-storage, machine-count, and snapshot-count limits.
- Size defaults so the current host can comfortably support about `8` users with about `5` active default VMs each.
- Preserve existing hidden VM sizing and stop/start semantics while changing only the admission and diagnostics model around CPU.
- Make CPU pressure understandable through diagnostics and operator tooling instead of surprising users with opaque hard rejections.
- Update validation so the platform is explicitly tested against the new shared-CPU target shape.

**Non-Goals:**
- Changing the hidden baseline VM size in this refactor.
- Introducing CPU hot-resize, memory ballooning, or dynamic in-place VM resizing.
- Building a billing/plans UI or self-serve quota editor.
- Implementing full cgroup-style CPU fairness inside the guest runtime in the first pass.
- Making RAM soft in the same change.

## Decisions

### 1. CPU becomes a host-shared soft pool; RAM and retained storage remain hard

The control plane will stop rejecting create/start/fork/restore solely because the user's active VMs sum to more than a hard per-user CPU cap. Instead:
- per-user RAM remains a hard admission limit
- per-user retained storage remains a hard admission limit
- per-user machine count and snapshot count remain hard admission limits
- CPU is admitted against a host-level shared active-CPU ceiling rather than a strict per-user reserved CPU sum

The new CPU accounting model will still compute nominal CPU demand as the sum of hidden machine CPU for `CREATING`, `STARTING`, `RUNNING`, and `STOPPING` VMs. The difference is that this nominal demand is compared to a host-wide soft-CPU ceiling, not a per-user hard CPU budget.

Why:
- This preserves a simple control-plane model while changing the part that is economically too conservative.
- RAM and disk are the safer hard fairness controls.
- CPU contention is acceptable for bursty developer workloads as long as the platform exposes clear guardrails.

Alternatives considered:
- Remove CPU admission entirely.
  - Rejected because one user could overfill the box with active VMs and destroy latency for everyone else.
- Keep hard per-user CPU and only raise the user default.
  - Rejected because it does not solve the fundamental UX problem and still leaves margins tight.
- Make both CPU and RAM soft.
  - Rejected because RAM overcommit is materially riskier on a single host.

### 2. Host CPU capacity will use an explicit overcommit ceiling

The host will advertise both:
- physical CPU threads (`24` on the current host)
- a shared active-CPU ceiling derived from an operator-configured overcommit ratio

For the current target, the default ceiling will be sized to roughly `40` nominal active vCPU on the host, which is about `1.67x` overcommit on `24` threads. That is the operating point that allows roughly `8` users times `5` active default `1 vCPU` machines.

Implementation-wise, the control plane can express this as either:
- `host_soft_cpu_ratio` (for example `1.67`)
- or a derived `host_active_cpu_capacity`

The design will prefer a ratio in config and derived capacity in diagnostics so the system generalizes if the host shape changes later.

Why:
- This gives operators one explicit knob for platform aggressiveness.
- It makes the business target legible in diagnostics and validation.
- It avoids pretending the host should be scheduled only up to physical thread count when the workloads are bursty.

Alternatives considered:
- Hardcode `40` active vCPU for the current host.
  - Rejected because it couples the policy too tightly to one machine shape.
- Use instantaneous host CPU utilization for admission.
  - Rejected because it makes admission nondeterministic and vulnerable to short-lived bursts.

### 3. Per-user CPU changes from hard quota to soft entitlement

Users will continue to have a persisted CPU-related field, but its meaning changes:
- it becomes an entitlement/diagnostic value rather than a hard create/start rejection threshold
- defaults are retuned around the target product shape, likely `5` nominal active vCPU per user on the current plan

Admission rules will not reject a user merely because their nominal active CPU demand exceeds their entitlement if:
- hard RAM/disk/count limits still fit
- host soft CPU capacity still fits

Diagnostics will show:
- the user's soft CPU entitlement
- the user's current nominal active CPU demand
- whether the user is above or below their entitlement
- remaining host shared CPU headroom

Why:
- We still need a per-user notion of "what this plan is supposed to feel like."
- Turning CPU into a diagnostic entitlement preserves product clarity without reintroducing the current hard wall.
- It gives later billing or plan work a stable concept to build on.

Alternatives considered:
- Remove per-user CPU from the data model entirely.
  - Rejected because plans and diagnostics still need a user-level CPU concept.
- Keep per-user CPU as a hard limit but add an exception path under host slack.
  - Rejected because mixed hard/conditional semantics would be harder to explain and test.

### 4. Hidden baseline machine size stays unchanged in this refactor

This refactor will keep the existing hidden default VM size for fresh machines. The point of this change is to make the platform economics and concurrency match the current product story before revisiting a richer baseline machine size.

That means the target operating point is intentionally built around today's default hidden machine shape rather than a larger "premium dev box" baseline.

Why:
- It isolates the CPU-policy refactor from the separate product question of VM baseline generosity.
- It lets us hit the `8 users * 5 active VMs` target on the current box.
- It avoids confounding two big product-policy changes in one rollout.

Alternatives considered:
- Increase the hidden machine baseline in the same change.
  - Rejected because it would erase most of the concurrency gains immediately.

### 5. Hard RAM defaults will be retuned to support five active default machines per user

With the current hidden machine baseline at `2 GiB`, a user needs at least `10 GiB` of hard RAM budget to run `5` active default machines. The new default user RAM budget will therefore be increased from `8 GiB` to a value in the `10-12 GiB` range, with `10 GiB` as the minimum viable target and `12 GiB` as the more comfortable operator choice.

Disk defaults will also be revisited upward enough that retained stopped machines and snapshots stay practical under the new concurrency story, but disk remains a strict hard cap.

Why:
- Soft CPU alone does not solve the product target if hard RAM still caps users at four active default VMs.
- RAM is the clean hard fairness boundary for this model.

Alternatives considered:
- Keep RAM at `8 GiB`.
  - Rejected because it contradicts the stated goal of roughly five active default VMs per user.

### 6. Admission logic remains lock-based and deterministic

Create, start, fork, and restore will continue to run under the host-scoped admission lock. Under that lock, the control plane will check:
- user hard RAM budget
- user hard retained-storage budget
- user hard machine-count and snapshot-count limits
- host hard RAM capacity
- host hard free-disk floor
- host shared soft CPU ceiling

This keeps admission deterministic while still allowing more concurrency than the current model.

Why:
- The control plane already has a safe host-scoped locking pattern.
- Deterministic admission is easier to reason about than reactive throttling after boot.
- It limits the scope of the refactor to policy, not scheduler architecture.

Alternatives considered:
- Introduce a background CPU queue for pending starts/creates.
  - Rejected for the first pass because it changes user experience and API semantics significantly.

### 7. Diagnostics and stress validation must expose the new economic model explicitly

Operator-visible surfaces will gain explicit concepts for:
- physical host CPU
- shared active-CPU capacity
- current nominal active-CPU demand
- per-user soft CPU entitlement
- per-user hard RAM budget and current active RAM usage
- retained machine and snapshot storage usage

Stress validation will add scenarios that prove the current host can sustain the target operating shape and that RAM/disk remain the hard brakes.

Why:
- Without clear visibility, soft CPU just looks like "sometimes the platform rejects me and sometimes it doesn't."
- The point of this refactor is as much economic and operational as it is technical.

Alternatives considered:
- Hide the soft CPU model entirely and keep only generic error strings.
  - Rejected because operators need to understand platform pressure to trust the model.

## Risks / Trade-offs

- **[Soft CPU means latency can degrade before hard rejection happens]** → Keep CPU soft only up to an explicit host overcommit ceiling and surface the ceiling clearly in diagnostics.
- **[One noisy user can consume more than their nominal fair share]** → Keep RAM hard per user, keep CPU entitlement visible in diagnostics, and leave room for a later fairness throttle if real usage shows abuse.
- **[The target depends on current hidden machine size staying small]** → Treat baseline machine sizing as a separate later decision and do not silently enlarge the hidden default in this change.
- **[Raising RAM and disk budgets too far could erase the margin gains]** → Choose defaults against the live host numbers, preserve the host free-disk floor, and validate the target shape on the actual OVH box.
- **[CPU semantics become harder to explain than today's hard wall]** → Add diagnostics, README/operator docs, and explicit rejection language that distinguishes soft CPU policy from hard RAM/disk policy.

## Migration Plan

1. Add the new shared-CPU config fields and retuned default user budget values.
2. Update the control-plane budget model so per-user CPU is treated as soft entitlement while host shared CPU remains an admission gate.
3. Keep hard RAM/disk/count logic intact, but retune RAM/disk defaults for the new target operating point.
4. Update diagnostics and host reporting to expose physical CPU, shared active-CPU capacity, nominal active demand, and entitlement posture.
5. Update API/browser error handling and copy so CPU is described as shared capacity rather than a strict per-user hard wall.
6. Extend stress and smoke tooling to validate the target `8 users * 5 active VMs` shape on the live host.
7. Roll out on the current host with live validation against the new target and rollback to the prior CPU policy if host responsiveness degrades materially.

Rollback:
- Revert the control-plane and config changes together.
- Restore the previous hard per-user CPU admission logic.
- Keep the hidden-sizing and power-state work in place; only the CPU-policy layer needs to roll back.

## Open Questions

- Should the default user hard RAM budget land at `10 GiB` exactly for a clean "5 active default VMs" story, or `12 GiB` for more operational slack?
- Do we want to preserve the current `MaxCPU` field name in config and storage while changing its semantics, or rename it to an explicit soft-entitlement concept now that there are no active users to preserve?
