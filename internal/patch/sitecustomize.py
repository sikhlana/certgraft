"""Dev-only: clear VERIFY_X509_STRICT from every default TLS context (Python 3.13+ sets it).

The chain is still verified against the trusted roots; only RFC 5280 strictness (e.g. a required
Authority Key Identifier, which Cisco Umbrella's re-signed chain lacks) is dropped.
"""

import ssl

_create_default_context = ssl.create_default_context


def _relaxed_default_context(*args, **kwargs):
    ctx = _create_default_context(*args, **kwargs)
    ctx.verify_flags &= ~getattr(ssl, "VERIFY_X509_STRICT", 0)
    return ctx


ssl.create_default_context = _relaxed_default_context
ssl._create_default_https_context = _relaxed_default_context
