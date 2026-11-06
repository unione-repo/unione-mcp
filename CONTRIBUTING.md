# Contributing

Thank you for helping to improve unione-mcp.

## Issues

Bug reports, questions and feature requests are welcome in
[Issues](https://github.com/unione-repo/unione-mcp/issues). Please include the version (release tag), how you run the
server (remote, npx, Docker or binary), your MCP client, and what happened versus what you
expected. Never post an API key or an access token. Security problems go to
[SECURITY.md](./SECURITY.md), not to issues.

## Pull requests

This repository is a published copy of the code maintained in the UniOne internal repository.
The maintainers review every issue and pull request here, but **external pull requests are not
merged into this repository directly**: an accepted change is carried over into the internal
repository and comes back here with the next release, after which the pull request is closed.

So that a change can be carried over:

- keep a pull request to one change, with a test that shows it;
- run `go vet ./...` and `go test ./internal/... ./cmd/...` before you open it;
- describe why the change is needed, not only what it does.

By opening a pull request you agree that your contribution is licensed under the
[MIT License](./LICENSE).
