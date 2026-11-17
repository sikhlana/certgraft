# Spec Delta

## REMOVED Requirements

### Requirement: Roots come from a directory of PEM files
**Reason**: On the host, operators point at certificates they already have; a directory existed only
to mount into a container.
**Migration**: List the files in `roots.files` in `~/.config/certgraft/config.yaml` (or
`CERTGRAFT_ROOTS_FILES`). Nothing is released, so no operator config needs moving.

## ADDED Requirements

### Requirement: Roots come from listed PEM files
certgraft SHALL read its extra roots from the PEM files listed in `roots.files` (in
`~/.config/certgraft/config.yaml`, or the `CERTGRAFT_ROOTS_FILES` env var, comma-separated with
spaces trimmed). It MUST refuse to start when the list is empty, a listed path isn't a file, or a
file holds no PEM certificate, naming the path.

#### Scenario: Valid roots
- **WHEN** `roots.files` lists two files, each with one PEM certificate
- **THEN** both certificates are loaded

#### Scenario: Env form
- **WHEN** `CERTGRAFT_ROOTS_FILES="a.pem, b.pem"` is set and both files hold a certificate
- **THEN** both certificates are loaded

#### Scenario: File without a certificate
- **WHEN** a listed file holds no PEM certificate
- **THEN** loading fails with an error naming that file

#### Scenario: Missing file
- **WHEN** a listed path doesn't exist
- **THEN** loading fails with an error naming that path

#### Scenario: Empty list
- **WHEN** `roots.files` is empty
- **THEN** loading fails with an error saying there are no extra roots
