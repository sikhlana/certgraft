# Spec Delta

## MODIFIED Requirements

### Requirement: Patches are cached by what they were made from
certgraft SHALL cache each patch result under (upstream manifest digest, root-set fingerprint, patcher
version) in the cache directory (config key `store.dir`, env `CERTGRAFT_STORE_DIR`, default
`~/.cache/certgraft`), and record the upstream digest each tag last resolved to. A different root set
or patcher version SHALL be a different entry.

#### Scenario: Default location
- **WHEN** certgraft starts with `store.dir` unset
- **THEN** its cache is `~/.cache/certgraft` in the operator's home directory

#### Scenario: Root set changed
- **WHEN** certgraft restarts with a different root set and a previously served tag is requested
- **THEN** the image is patched again and carries the new fingerprint

#### Scenario: Same inputs
- **WHEN** an entry is looked up with the key it was stored under
- **THEN** the stored result comes back, and a key differing only in fingerprint finds nothing
