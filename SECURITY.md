# Security policy

## Reporting a vulnerability

Please **do not open a public issue** for a security problem.

Report it privately to **bounty@unione.io**, or through
[GitHub private vulnerability reporting](https://github.com/unione-repo/unione-mcp/security/advisories/new).

Include what you found, how to reproduce it, the version (release tag or commit) and what an
attacker could do with it. We will acknowledge the report and keep you informed while we work on
it.

## Supported versions

Only the latest release gets security fixes. The hosted server at `https://mcp.unione.io/mcp` always runs a
supported version.

## Scope

This repository: the MCP server, its OAuth handling and its release artifacts (binaries, the
`ghcr.io/unione-repo/unione-mcp` image, the `unione-mcp` npm package).

A problem in the UniOne API or account itself is in scope of UniOne security too — report it to
the same contact.

## Handling of secrets

The server keeps no state: it stores neither API keys nor OAuth tokens and never logs them or the
`Authorization` header. Each call goes to the UniOne API with the caller's own credential.
