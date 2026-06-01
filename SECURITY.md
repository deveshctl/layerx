# Security Policy

## Supported versions

Only the latest released minor version receives security fixes. Older tags are not patched.

## Reporting a vulnerability

Please **do not** open a public GitHub issue for security problems.

Report privately via GitHub's [private vulnerability reporting](https://github.com/deveshctl/layerx/security/advisories/new) on this repository, or email the maintainer at the address listed on the GitHub profile of [@deveshctl](https://github.com/deveshctl).

Include:

- A short description of the issue and its impact.
- Steps to reproduce, or a minimal proof-of-concept image / archive / config.
- The layerx version (`layerx --version`) and OS.

You should get an acknowledgement within **3 business days**. We aim to ship a fix or mitigation within **30 days** for high-severity issues; lower-severity issues may take longer.

## Scope

In scope:

- Code execution, path traversal, or unbounded memory / disk use triggered by a crafted image, archive, or `.layerx.yaml`.
- Cache directory or extracted-file write paths escaping their intended root.
- Any way `layerx ci` can be made to exit `0` on an image that should fail its configured rules.

Out of scope:

- Vulnerabilities in Docker Engine, the Go toolchain, or third-party dependencies — report those upstream. We will pull in fixed versions.
- Issues that require an attacker to already control the host running layerx.

## Hardening notes for users

- `layerx` reads tar entries with a hard 2 GiB-per-entry cap; a crafted size header cannot exhaust memory.
- Cache writes use temp file + fsync + atomic rename; a partial write cannot leave a corrupted cache entry.
- File extraction (`x` in the TUI) writes only into the current directory and validates paths before opening files.

If any of these guarantees are observably broken, that's a security bug — please report it.
