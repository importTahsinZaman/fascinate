## MODIFIED Requirements

### Requirement: Fascinate SHALL capture updated session-state auth from running VMs
For supported session-state adapters, Fascinate SHALL capture updated auth state from a running VM and replace the user’s canonical stored profile with the captured state only when the capture point is authoritative for that profile.

#### Scenario: First login becomes available to a later VM
- **WHEN** a user logs into a supported session-state tool in one Fascinate VM and Fascinate reaches a persistence checkpoint
- **THEN** Fascinate stores the updated auth profile for that user/tool/auth-method
- **AND** a later VM for the same user restores that login state automatically

#### Scenario: Logout propagates to later VMs from an authoritative checkpoint
- **WHEN** a user logs out of a supported session-state tool in one Fascinate VM and Fascinate captures the updated state from an authoritative capture point
- **THEN** Fascinate replaces the stored auth profile with the logged-out state
- **AND** later VMs for that user no longer restore the previous login

#### Scenario: Opportunistic empty sync does not clobber valid auth
- **WHEN** a background or pre-create sync captures an empty or missing session-state bundle from one running VM while the user still has a valid stored auth profile for that same tool and auth method
- **THEN** Fascinate preserves the existing valid stored profile
- **AND** it does not silently replace it with the opportunistic empty capture

## ADDED Requirements

### Requirement: Fascinate SHALL expose operator-visible tool-auth diagnostics
Fascinate SHALL expose operator-visible diagnostics when tool-auth capture or restore fails so those failures can be investigated during stress validation and live operations.

#### Scenario: Restore failure exposes the affected tool and method
- **WHEN** Fascinate cannot restore a supported auth profile during machine provisioning
- **THEN** Fascinate records an operator-visible failure that identifies the affected tool and auth method
- **AND** the machine can still fall back to a usable logged-out state

#### Scenario: Capture failure exposes the affected tool and checkpoint
- **WHEN** Fascinate cannot capture auth state from a running VM at a persistence checkpoint
- **THEN** Fascinate records an operator-visible failure that identifies the affected tool, auth method, and capture path
- **AND** one adapter failure does not prevent unrelated adapters from being captured or restored
