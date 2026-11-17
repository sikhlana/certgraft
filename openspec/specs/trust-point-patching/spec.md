# trust-point-patching Specification

## Purpose
Turns an upstream image into one that trusts the extra roots, without running anything in it: it reads
the merged filesystem, appends one layer that rewrites each trust point, and points language runtimes
at the system bundle. Images it can't fully patch are served unpatched and say so. Carried today by:
internal/patch.

## Requirements
### Requirement: Patching never runs the image
certgraft SHALL patch by reading the image's layers as archives, honouring whiteouts and opaque
directories, and appending exactly one layer plus config edits. It MUST NOT execute image content, so
every platform is patched the same way.

#### Scenario: Whiteout over the bundle
- **WHEN** an upper layer deletes the system bundle (file whiteout, opaque directory, or a deleted parent)
- **THEN** the patched bundle holds only the extra roots, not the deleted bundle's

#### Scenario: One layer
- **WHEN** an image is patched
- **THEN** it has the upstream layers plus one

### Requirement: System bundles gain the extra roots
certgraft SHALL append every missing extra root to `/etc/ssl/certs/ca-certificates.crt` and
`/etc/pki/tls/certs/ca-bundle.crt` where they exist, following symlinks and keeping them. It SHALL
also write each root to `/usr/local/share/ca-certificates/` (Debian and Alpine families) or
`/etc/pki/ca-trust/source/anchors/` (RHEL family), so rebuilding the bundle in the image keeps them.

#### Scenario: Debian bundle
- **WHEN** a Debian image with a bundle is patched
- **THEN** the bundle holds the upstream roots followed by the extra roots, and `/usr/local/share/ca-certificates/certgraft-1.crt` holds the first extra root

#### Scenario: Amazon Linux bundle through a symlink
- **WHEN** `/etc/pki/tls/certs/ca-bundle.crt` is a symlink to the extracted bundle
- **THEN** the link's target ends with the extra roots, the link stays a link, and the anchors directory holds each extra root

### Requirement: A bundle is created only where something reads it
An image without a bundle SHALL get `/etc/ssl/certs/ca-certificates.crt` holding only the extra roots
when it has an OS userland (`/bin/sh` or an `os-release` file) or a Node or Python runtime. An image
with neither and no other trust point SHALL be passed through.

#### Scenario: Busybox
- **WHEN** an image with `/bin/sh` and no bundle is patched
- **THEN** the new bundle holds exactly the extra roots

#### Scenario: Lone binary
- **WHEN** an image holding only one binary is patched
- **THEN** it is passed through

### Requirement: Image-defined settings win
certgraft MUST NOT overwrite an env var the image sets. When `SSL_CERT_FILE`, `REQUESTS_CA_BUNDLE`,
`PIP_CERT`, `NODE_EXTRA_CA_CERTS` or `CURL_CA_BUNDLE` names a file in the image, the missing extra
roots SHALL be appended to that file.

#### Scenario: Upstream SSL_CERT_FILE
- **WHEN** an image sets `SSL_CERT_FILE=/opt/certs/bundle.pem`
- **THEN** the patched config sets it once, unchanged, and that file ends with the extra roots

### Requirement: JVM trust stores gain the extra roots
certgraft SHALL add every missing extra root to each `lib/security/cacerts` (under `JAVA_HOME` or
anywhere in the image), keeping its format (JKS or PKCS12) and password (`changeit`, or password-less
for PKCS12). JKS entries keep their aliases; PKCS12 entries keep their certificates but get aliases
derived from their subjects. A store that already holds every root is left unchanged.

#### Scenario: PKCS12 store
- **WHEN** a PKCS12 store protected by `changeit`, or password-less, is patched
- **THEN** it decodes with the same password and holds the upstream and extra roots

#### Scenario: JKS store
- **WHEN** a JKS store is patched
- **THEN** it holds the upstream and extra roots and the upstream entry keeps its alias

### Requirement: Node points at the system bundle
For an image whose config sets `NODE_VERSION`, certgraft SHALL set `NODE_EXTRA_CA_CERTS` to the
system bundle.

#### Scenario: Node
- **WHEN** an image setting `NODE_VERSION` is patched
- **THEN** its config sets `NODE_EXTRA_CA_CERTS=/etc/ssl/certs/ca-certificates.crt`

### Requirement: Python points at the system bundle and drops strict X.509
For an image with Python (`PYTHON_VERSION`, or `python3` on `PATH`), certgraft SHALL set
`SSL_CERT_FILE`, `REQUESTS_CA_BUNDLE` and `PIP_CERT` to the system bundle and `UV_SYSTEM_CERTS=1`. It
SHALL write a `sitecustomize.py` that clears Python's strict X.509 flag into the purelib directory,
unless one is already there, in which case the image is labelled
`io.github.sikhlana.certgraft.python-strict=skipped`.

#### Scenario: Python env and sitecustomize
- **WHEN** an image setting `PYTHON_VERSION=3.13.1` with `/usr/local/lib/python3.13/site-packages` is patched
- **THEN** its config sets the four variables and that directory holds the strict-X.509 `sitecustomize.py`

#### Scenario: Existing sitecustomize
- **WHEN** a Python image already has `sitecustomize.py` in its purelib
- **THEN** the file is unchanged and the image carries the `python-strict=skipped` label

### Requirement: Patched images are labelled
Every patched image SHALL carry `io.github.sikhlana.certgraft.patched` set to the root-set fingerprint.
When a trust point can't be patched, the image SHALL be served with only that label changed, set to
`failed`, and certgraft SHALL log one line naming the image and the trust point.

#### Scenario: Undecodable trust store
- **WHEN** an image's `cacerts` isn't a keystore
- **THEN** it is served with its upstream layers, labelled `failed`, and one log line names the image and `java trust store /<path>`
