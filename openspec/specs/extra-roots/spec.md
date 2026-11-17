# extra-roots Specification

## Purpose
Loads the operator's extra CA roots, the certificates a TLS-intercepting proxy signs with, so every
patched image gains them and certgraft's own upstream calls trust them. The set is identified by a
fingerprint that every cached patch is keyed on. Carried today by: internal/roots, internal/upstream.

## Requirements
### Requirement: Roots come from a directory of PEM files
certgraft SHALL read its extra roots from every file in the configured roots directory (config key
`roots.dir`, env `CERTGRAFT_ROOTS_DIR`, default `/etc/certgraft/roots`). It MUST refuse to start when
a file holds no PEM certificate or the directory has no files, naming the file or directory.

#### Scenario: Valid roots
- **WHEN** the roots directory holds two files, each with one PEM certificate
- **THEN** both certificates are loaded

#### Scenario: File without a certificate
- **WHEN** a file in the roots directory holds no PEM certificate
- **THEN** loading fails with an error naming that file

#### Scenario: Empty directory
- **WHEN** the roots directory has no files
- **THEN** loading fails with an error saying there are no extra roots

### Requirement: The root set has an order-independent fingerprint
certgraft SHALL identify the root set by the hex SHA-256 of its roots' sorted SHA-256 digests (over
each certificate's DER), ignoring order and duplicates. Patched images carry this fingerprint.

#### Scenario: Same roots, different order
- **WHEN** the same certificates are loaded in a different order, with one repeated
- **THEN** the fingerprint is the same, and the duplicate is dropped

#### Scenario: Different roots
- **WHEN** one certificate is removed from the set
- **THEN** the fingerprint changes

### Requirement: Upstream calls trust the extra roots
certgraft's own connections to upstream registries SHALL trust the system roots plus the extra roots,
because they pass through the same intercepting proxy, and MUST NOT disable verification.

#### Scenario: Proxy-signed upstream
- **WHEN** an upstream answers with a certificate signed by one of the extra roots
- **THEN** certgraft fetches from it successfully

#### Scenario: Root missing
- **WHEN** the extra roots don't include the root that signed the upstream's certificate
- **THEN** the request fails with a certificate error, returned to the client as 502
