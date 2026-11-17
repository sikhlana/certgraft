# Spec Delta

## REMOVED Requirements

### Requirement: Listeners
**Reason**: certgraft runs on the host. The registry moves to loopback port 5555, and the health
server goes: nothing on a workstation probes it, and `certgraft status` checks the registry itself.
**Migration**: Point clients at `127.0.0.1` on the configured registry port (default 5555); the
source policy uses `host.docker.internal` on the same port, which the runtimes forward there.
Nothing is released, so no operator is affected.

## ADDED Requirements

### Requirement: The registry listens on loopback
certgraft SHALL serve the registry on `127.0.0.1:5555` by default, with a 30-minute write timeout,
both configurable (`registry.server.host`, `registry.server.port`), and SHALL open no other listener.

#### Scenario: Defaults
- **WHEN** certgraft loads its config with nothing set
- **THEN** the registry listens on 127.0.0.1:5555 with a 30-minute write timeout

#### Scenario: Loopback only
- **WHEN** `certgraft serve` is running with the default config
- **THEN** port 5555 accepts connections on 127.0.0.1 and refuses them on the machine's LAN address
