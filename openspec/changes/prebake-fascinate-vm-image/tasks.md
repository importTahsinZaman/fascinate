## 1. Build And Seal Images

- [x] 1.1 Replace the current base-image builder with a versioned Fascinate image build flow that boots a disposable builder VM and provisions the default guest toolchain there.
- [x] 1.2 Add explicit build inputs and a guest image manifest that records the published image version and resolved toolchain versions.
- [x] 1.3 Implement the image sealing step so published artifacts are scrubbed of machine-specific identity, cloud-init instance state, SSH host keys, and persisted tool-auth/session state.

## 2. Validate And Promote Images

- [x] 2.1 Add a candidate-image validation flow that boots a temporary VM and verifies SSH readiness, default toolchain presence, Docker availability, and absence of leaked auth/session state.
- [x] 2.2 Add promotion and rollback support for versioned image artifacts, including a stable "current image" reference for fresh machine creation and a retained previous image for rollback.
- [x] 2.3 Extend ops verification and smoke coverage so image validation and promotion failures are caught before a bad image becomes the fresh-create default.

## 3. Refactor Fresh VM Bootstrap

- [x] 3.1 Split the current runtime bootstrap logic into build-time provisioning and boot-time machine finalization, keeping cloud-init limited to hostname, network, managed env, guest instructions, and other machine-specific state.
- [x] 3.2 Update fresh machine creation to consume the promoted Fascinate image artifact without performing live package or toolchain installation during per-machine boot.
- [x] 3.3 Adjust guest readiness checks so they validate a booted prebaked image rather than waiting for the removed first-boot installer path.

## 4. Remove Legacy Provisioning Path

- [x] 4.1 Delete the obsolete first-boot toolchain installation path and any related runtime/config plumbing once the prebaked image flow is authoritative.
- [x] 4.2 Remove or rewrite tests and scripts that assume fresh machines install the default toolchain during guest boot.

## 5. Document And Operationalize

- [x] 5.1 Document the new image build, validation, promotion, and rollback workflow for operators in the relevant docs and active OpenSpec artifacts.
- [x] 5.2 Document the new fresh-machine contract so future work treats snapshots as restore semantics and prebaked images as fresh-create semantics.
