# Security Policy

## Supported versions

Security fixes are provided for the latest released version and the current `main` branch when feasible. If you are using an older release, please verify whether the issue reproduces on the latest version.

## Reporting a vulnerability

Please report suspected security vulnerabilities privately.

- Preferred: open a **private security advisory** on GitHub for this repository (Security tab → "Report a vulnerability"), if enabled.
- If private advisories are not available: contact the maintainers through the repository owner/contact listed on GitHub and include `[SECURITY]` in the subject. Do not disclose details in a public issue, pull request, discussion, or commit message.

When reporting, include:

- affected version/commit and platform (OS/arch)
- component/area (e.g. policy/executor, TUI terminal, model download, proxy, provider integration)
- clear reproduction steps or proof of concept
- expected vs actual behavior and impact
- any suggested mitigation, if known

## What to expect

- We will acknowledge receipt as soon as practical.
- We will investigate, assess impact/severity, and work on a fix or mitigation.
- We may ask for additional details to reproduce or validate the fix.
- Please allow reasonable time for remediation before public disclosure.

## Security notes for users

- `dmsh` can execute shell commands. Safety policy, confirmations, allowlist rules, and dry-run/preview modes are important controls, but they are not a substitute for reviewing commands.
- Keep secrets (API keys, proxy credentials) in environment variables or protected config locations; avoid sharing logs/config/history/audit files without redaction.
- Use `dmsh doctor` and debug logs carefully when filing issues; redact sensitive data even though dmsh attempts to redact known secrets.

## Verifying a release

Every release ships `SHA256SUMS`, `SHA256SUMS.asc` (an ASCII-armored detached GPG signature of the checksums) and a CycloneDX SBOM. Verify the archive you downloaded before running it:

```bash
sha256sum --check SHA256SUMS
gpg --verify SHA256SUMS.asc SHA256SUMS
```

The signing key is checked into the repository at
`doc/releases/dmsh-release-signing-key.asc` (fingerprint
`11DD0F0B86E656772FBC8A8F22CA72A8491AE508`). Import it once before verifying:

```bash
gpg --import doc/releases/dmsh-release-signing-key.asc
```

On macOS use `shasum -a 256 --check SHA256SUMS`. The release workflow fails if the checksums, the SBOM or the signature are missing, so an unsigned artifact must never appear in a release.
