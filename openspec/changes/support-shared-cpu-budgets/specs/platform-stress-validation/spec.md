## ADDED Requirements

### Requirement: Fascinate SHALL validate the shared-CPU target operating point on the live host
Fascinate SHALL maintain validation coverage for the intended one-box operating point of roughly `8` users with about `5` active default VMs each under the shared-CPU model.

#### Scenario: Validation exercises multi-user active VM concurrency
- **WHEN** Fascinate runs live-host stress validation for the shared-CPU model
- **THEN** the validation exercises multiple users creating and running default machines concurrently
- **AND** it confirms that the platform can sustain the target shape without tripping the old hard per-user CPU rejection after two active machines

#### Scenario: Validation confirms the shared CPU ceiling eventually engages
- **WHEN** live-host stress validation pushes the host beyond the intended shared CPU operating point
- **THEN** Fascinate rejects further active machine work gracefully
- **AND** the validation confirms that the rejection is attributed to shared host CPU pressure rather than a stale per-user reserved CPU rule

### Requirement: Fascinate SHALL validate that RAM and retained storage remain hard guardrails
Fascinate SHALL keep validation coverage proving that the new shared-CPU policy does not weaken the existing hard RAM and retained-storage guardrails.

#### Scenario: RAM remains the primary per-user active guardrail
- **WHEN** validation creates or starts active machines for a user until that user's hard RAM limit is reached
- **THEN** Fascinate rejects the next active machine request
- **AND** the validation confirms the rejection is attributed to hard RAM rather than CPU

#### Scenario: Retained storage still blocks accumulation
- **WHEN** validation accumulates retained machines and snapshots until the user's hard retained-storage limit is reached
- **THEN** Fascinate rejects the next retained artifact request
- **AND** the validation confirms that soft shared CPU policy does not weaken retained-storage enforcement
