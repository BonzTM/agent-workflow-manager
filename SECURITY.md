# Security Policy

## Reporting a Vulnerability

If you discover a security vulnerability in awm, please report it responsibly.

**Do not open a public GitHub issue for security vulnerabilities.**

Instead, please use GitHub's private vulnerability reporting feature:

1. Go to the [Security tab](https://github.com/BonzTM/agent-workflow-manager/security) of this repository.
2. Click "Report a vulnerability".
3. Provide a description of the vulnerability, steps to reproduce, and any potential impact.

We will acknowledge receipt within 72 hours and aim to provide a fix or mitigation plan within 30 days.

## Supported Versions

| Version | Supported |
|---------|-----------|
| 1.0.x   | Yes       |

## Scope

The following areas are in scope for security reports:

- Input validation and injection vulnerabilities in CLI, MCP, or web surfaces.
- SQL injection in SQLite or Postgres adapters.
- Path traversal in file operations (init, sync, verify).
- Credential exposure through logs, error messages, or environment variable handling.
- Web dashboard (`awm-web`) vulnerabilities including XSS or information disclosure.

## Out of Scope

- Denial of service through large but valid inputs (awm is a local/single-tenant tool).
- Vulnerabilities in upstream dependencies — please report those to the relevant projects directly.
