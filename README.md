# certgraft

Graft your corporate CA onto any container image, with no Dockerfile changes.

## What

Behind a TLS-intercepting proxy (Zscaler, Cisco Umbrella, Netskope and friends), `RUN` steps in
upstream base images fail with `x509: certificate signed by unknown authority`, because the image
doesn't trust the proxy's root. certgraft is a small local registry that sits between your builds
and the public registries. It serves every image with your extra CA roots added:

- the system CA bundle, and the distro's local-roots directory, so `update-ca-certificates` keeps them;
- the JVM trust store, in its own format and password;
- Node (`NODE_EXTRA_CA_CERTS`) and Python (`SSL_CERT_FILE` and friends, plus strict X.509 relaxed);
- every platform of a multi-arch image, without running anything inside it.

Tags track upstream, digest pins keep their meaning, and images with nothing to patch come through
byte-for-byte. Patched images carry the label `io.github.sikhlana.certgraft.patched`.

## Why

The usual fixes are a `COPY corp-root.crt` in every Dockerfile, a rebuilt base image per language
and version, or turning verification off. The first two don't scale past a handful of images, and
the third is worse than the problem. certgraft fixes it once, on your machine, for every image.

## How

Pre-release: installing with Homebrew and wiring it into Docker Desktop and OrbStack is in progress
(`openspec/changes/`). Building from source needs only Docker: `make cache-init install check`.

Don't push images built through certgraft: they carry your corporate root. Push from CI.

## Licence

MIT.
