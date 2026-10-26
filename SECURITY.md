# Security policy

## Reporting a vulnerability

Please report vulnerabilities privately, through
[GitHub's private reporting](https://github.com/workflowproj/workflow/security/advisories/new),
not in a public issue. Include what an attacker can do, the steps to reproduce it, and the version
or commit you tested.

You'll get an acknowledgement within a few days. Once a fix is ready, it's released with a
security advisory that credits you, unless you'd rather not be named.

## Supported versions

Security fixes go into the latest release. Workflow hasn't reached 1.0 yet, so upgrade to the
latest release to receive them.

## How the code is checked

Every change runs through the [Security workflow](.github/workflows/security.yml): reachable Go
vulnerabilities (govulncheck), Go static analysis (gosec), CodeQL for Go, TypeScript and the
workflows, secret scanning over the whole history (gitleaks), the UI's npm dependencies, and
Trivy over the dependencies, the Dockerfile, the Helm chart and the built image. Release images
are signed with Cosign and ship with an SBOM. [CONTRIBUTING.md](CONTRIBUTING.md#continuous-integration)
describes each check.
