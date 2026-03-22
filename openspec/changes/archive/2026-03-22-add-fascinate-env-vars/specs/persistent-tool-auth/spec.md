## ADDED Requirements

### Requirement: Fascinate SHALL teach guest agents to prefer Fascinate env vars for machine identity
Fascinate SHALL update the VM-injected `AGENTS.md` / `CLAUDE.md` instructions so coding agents know to use managed Fascinate env vars instead of hardcoded machine hostnames when configuring apps.

#### Scenario: Guest instructions point agents at Fascinate env vars
- **WHEN** a new, restored, or cloned machine is provisioned
- **THEN** the injected guest instructions mention the managed env files under `/etc/fascinate/`
- **AND** they tell agents to prefer `FASCINATE_PUBLIC_URL` and related built-ins over hardcoded `https://<machine>.<base-domain>` literals

#### Scenario: Guest instructions describe clone-safe config behavior
- **WHEN** an agent configures an app that needs an external URL or machine metadata
- **THEN** the injected guest instructions tell the agent to source or reference Fascinate env vars where the app supports environment-based configuration
- **AND** the instructions explain that this is the clone-safe way to configure machine identity
