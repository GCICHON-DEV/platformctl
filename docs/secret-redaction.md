# Secrets and diagnostic redaction

Keep passwords/tokens/private keys out of platform.yaml, manifests, generated
files and command arguments. Use an inherited process environment or an external
secret store read directly by the tool. Do not interpolate secrets into a
preview. Inputs marked `sensitive: true` reject nonempty values/default/example;
they are documentation of a forbidden inline input, not a hidden-password field.
Secret-bearing template URL userinfo/query parameters are rejected before use.

State contains preview/input/target digests and checkpoints, not secret values.
Redaction changes presentation only: the approved digest still covers the exact
arguments passed to the executable. A redacted display is not permission to
store credentials in a manifest or source-control history.

The shared redactor covers:

- Known nonempty environment values whose names contain password, passwd,
  secret, token, credential, api_key, access_key or private_key (case insensitive,
  `_`/`-` variants), including raw, JSON-escaped and URL-encoded forms.
- Recognized `--password value`, `--token=value`, secret/key flag variants,
  assignments such as `password=value`, JSON string fields and URL userinfo.
- Recognized argument secrets become canaries for the corresponding child's
  stdout/stderr as well, even if printed without labels.
- CLI JSON is sanitized structurally, retaining its JSON types. Human plans use
  the same command redactor. Errors, verbose causes and workflow streams pass
  through the diagnostic boundary.

Diagnostic streams buffer one line, including split writes, and flush a trailing
line at process completion. A line exceeding 64 KiB is discarded in full and
replaced by a marker; its prefix is never emitted. Stream writes are serialized.
Non-sensitive lines retain their diagnostic content. This may delay progress
output that does not end with a newline.

This is a documented defensive filter, not complete protection against arbitrary
process output. Unknown unlabeled secrets, unrelated field names, transformed/
base64 values, terminal escape tricks and credentials written directly to files
or child-controlled network endpoints are outside the guarantee. Do not pass
secrets to untrusted commands. Known multiline secrets are also matched by their
nonempty line fragments, which can over-redact short/common text. Existing user
logs/files are not rewritten or silently deleted.

Local checks include canaries in split stdout/stderr, flags, URL/JSON strings,
plan JSON, verbose execution failure and saved state. Synthetic tests do not
contact a real credential provider, install global tools or deploy resources.
Rollback means the previous binary; it cannot erase previously exposed secrets
or undo infrastructure. If a real credential was logged, stop using it and
follow its owner's rotation process separately.
