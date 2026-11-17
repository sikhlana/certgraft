# patch-cache Specification

## Purpose
Keeps what certgraft made, so each upstream image is patched once per root set, survives restarts, and
can be trimmed. Only patched layers, configs and manifests are stored; upstream blobs never are.
Carried today by: internal/store, internal/registry.

## Requirements
### Requirement: Patches are cached by what they were made from
certgraft SHALL cache each patch result under (upstream manifest digest, root-set fingerprint, patcher
version) in the cache directory (config key `store.dir`, env `CERTGRAFT_STORE_DIR`, default
`/var/lib/certgraft`), and record the upstream digest each tag last resolved to. A different root set
or patcher version SHALL be a different entry.

#### Scenario: Root set changed
- **WHEN** certgraft restarts with a different root set and a previously served tag is requested
- **THEN** the image is patched again and carries the new fingerprint

#### Scenario: Same inputs
- **WHEN** an entry is looked up with the key it was stored under
- **THEN** the stored result comes back, and a key differing only in fingerprint finds nothing

### Requirement: The cache can be pruned
`certgraft prune --days N` (default 30, at least 1) SHALL remove entries and tag records not requested
for more than N days, then every stored blob no remaining entry lists that wasn't written within N days,
and print `pruned <n> image(s), <size>`.

#### Scenario: Old and new entries
- **WHEN** one entry was last requested 40 days ago and another today, sharing a blob, and `prune --days 30` runs
- **THEN** the old entry and its own blob are removed, the shared and new blobs remain, and it prints `pruned 1 image(s), 1.5 kB`

#### Scenario: Zero days
- **WHEN** `prune --days 0` runs
- **THEN** it fails with `--days must be at least 1`
