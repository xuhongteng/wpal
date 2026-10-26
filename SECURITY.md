# Security Policy

## Supported Versions

| Version | Supported |
|---|---|
| 1.0.x | ✅ |
| < 1.0 | ❌ |

Security fixes are released for the latest minor version only.

## Reporting a Vulnerability

**Please do not open a public issue for security problems.**

Report privately via GitHub's security advisory form:

> https://github.com/wpal/wpal/security/advisories/new

or email `security@wpal.dev`.

Please include:

- a description of the vulnerability and its impact,
- the affected version, platform and configuration,
- steps to reproduce,
- any proof-of-concept code.

### What to expect

| Stage | Target |
|---|---|
| Acknowledgement | within 72 hours |
| Initial assessment | within 7 days |
| Fix release | coordinated with the reporter; 90 days maximum |

We will keep you informed throughout, credit you in the release notes unless you
prefer otherwise, and are happy to discuss the fix before it ships.

## Scope

Wpal is a **read-only monitoring tool**: it reads OS metrics and serves
them over HTTP. Reports about the following are in scope:

- remote code execution or arbitrary file read/write through the HTTP/WS surface,
- path traversal in the static file handler,
- denial of service via unbounded memory or file growth,
- leakage of sensitive information (credentials, other users' command lines
  beyond what the running user can already read, etc.).

**Out of scope:**

- Wpal deliberately runs with the privileges of the invoking user and
  reads the same OS interfaces that user can read. Reports that "the agent can
  read `/proc`" are not vulnerabilities.
- The dashboard is unauthenticated by default and binds to loopback. Exposing it
  to an untrusted network without a reverse proxy providing authentication is a
  deployment mistake, not a vulnerability — but see "Hardening" below.
- Denial of service caused by a machine that is already fully saturated.

## Hardening

Wpal binds to `127.0.0.1` by default. When exposing it publicly:

1. Put it behind nginx/Caddy/IIS and terminate TLS there.
2. Add authentication at the proxy (HTTP basic auth, OAuth, IP allow-list…).
3. Keep the service running as an unprivileged user (`wpal` on Linux,
   `LocalService` on Windows).
4. Keep the data directory readable only by that user.

## Threat model in one paragraph

Wpal reads host metrics and publishes them to whoever can reach its HTTP
port. The interesting asset is **the metrics themselves** (process lists,
command lines, network activity). A deployment is safe when the port is not
reachable by untrusted parties. The bundled systemd unit runs as an
unprivileged user with a hardened sandbox; the Debian/RPM packages create that
user for you.
