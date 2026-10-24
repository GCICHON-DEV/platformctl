# Remote template trust and offline operation

Remote templates now require an explicit digest lock before init, plan,
preflight, apply or destroy can load them. Local template files/directories
retain the existing explicit local-trust contract and preview snapshots.
No template command is a sandbox: inspect the manifest and its commands first.

```sh
platformctl template lock --source platformctl/local-kind-standard --version v1.1.0
platformctl init --template platformctl/local-kind-standard --version v1.1.0 --project demo
platformctl plan
platformctl plan --offline
```

For an existing platform.yaml, `platformctl template lock` reads its source.
`--source` accepts an HTTPS URL or registry reference; `--version` selects the
registry ref. Prefer an immutable registry commit. The lock also binds the exact
SHA256 bytes, so moving a tag is detected. `template lock` is an explicit trust
operation (trust on first use), not signature verification or proof of publisher
identity. Review the printed digest against the intended source independently.

`.platformctl/template.lock.json` contains the resolved source URL, SHA256 and
cached manifest bytes, in one atomically published file (mode 0600). Keep it with
the workspace, protect it like configuration, and back it up when preserving a
workspace. It is not the workflow state format and is not automatically
committed by Git. Manifest/source URLs must contain no secrets; userinfo and
recognized secret query parameters are rejected. Credential checks should use
external credentials. A local user able to rewrite both lock and configuration
is outside this integrity boundary.

Online loads always download and compare against the approved digest. A changed
source, 404, redirect error or unavailable network fails; there is no automatic
fallback to older cached content. Use `--offline` explicitly to use only the
checked lock/cache bytes. Offline mode appears in source metadata and affects
the preview digest: review a fresh plan when changing modes. Offline execution
still runs trusted workflow commands, which may themselves require a network.

Updates are separate from execution:

```sh
platformctl template update
platformctl plan
```

`template update --source ... --version ...` can deliberately select a new
source. It never runs workflow steps. Failed download/validation preserves the
previous lock. A successful update can also repair corrupted cached bytes;
review its new digest, regenerate the preview, and reapprove without resume.
`template lock` refuses to replace an existing lock; `--offline` cannot lock or
update. A corrupt cache or source mismatch is rejected even in offline mode.

Responses and local manifests are limited to 1 MiB; lock files to 2 MiB. Downloads
have a 20-second total timeout, at most three redirects, HTTPS at every hop, no
userinfo/fragments, and require HTTP 200. Cache/lock writes use a synced temporary
file and atomic rename/link under the workspace workflow lock. Power-loss
recovery, hostile same-user directory swaps, signed publisher trust and recursive
local source pinning are not claimed. Existing apply/resume snapshot checks
continue to bind rendered source files and commands.

Rollback selects the previous CLI and source pins as a pair and reviews a new
plan. It does not undo resources or restore workflow/Terraform state. Never
silently edit a recorded digest to make an unexpected download pass.
