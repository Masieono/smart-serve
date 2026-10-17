# Security Policy

## Reporting a vulnerability

Please **do not** open a public issue for a security problem. Use GitHub's
[private vulnerability reporting](https://docs.github.com/en/code-security/security-advisories/guidance-on-reporting-and-writing-information-about-vulnerabilities/privately-reporting-a-security-vulnerability)
("Report a vulnerability" under the **Security** tab of the repository).

Please say what you did, what happened, and what you expected — a proof of concept against a
throwaway container is ideal. This is a single-maintainer project rather than a company with a
rota, so expect an acknowledgement within a few days.

## What is in scope

- The Go server, the embedded web UI, and the JSON/SSE API.
- The container image and the `Dockerfile`.
- Anything that lets a request read or write more than it should, or that lets a page on
  another origin drive the app. CSRF protection is enforced for exactly that reason
  (`ADR-015`).

## What is not in scope

Three things are deliberate decisions rather than defects, and reports about them will be
closed with a pointer here:

- **No authentication.** Smart-Serve ships no login and no user management; access control is
  delegated to a reverse proxy (`ADR-016`). An unauthenticated instance reachable from an
  untrusted network is a misconfiguration, and `README.md` and `docs/DEPLOYMENT.md` say so.
- **The container's privileges.** Reading S.M.A.R.T. requires access to raw device nodes, so
  the reference deployment opens the device cgroup and grants `CAP_SYS_ADMIN` and
  `CAP_SYS_RAWIO`. That is a real privilege grant, it is the operator's decision, and the
  least-privilege options are documented in `docs/DEPLOYMENT.md`.
- **The bundled `smartmontools`.** `smartctl` is GPLv2 and runs as a separate process
  (`ADR-012`), so vulnerabilities in it belong upstream. The image is bumped when there is a
  fix, and reports that a newer `smartctl` exists are welcome as ordinary issues.

## Supported versions

Only the most recent release is supported. Fixes land on `main` and are released as a new
patch version.
