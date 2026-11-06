# unione-mcp

The official [Model Context Protocol](https://modelcontextprotocol.io) server for
UniOne (https://docs.unione.io). It lets AI assistants — Claude, ChatGPT, Cursor, Windsurf, VS Code,
Cline and any other MCP client — work with your UniOne account: send email, manage sender
domains, templates, webhooks, suppressions, event dumps, tags and projects. 32 tools,
each one a call of the public UniOne Web API.

Two ways to use it:

- **Remote** — the hosted server at `https://mcp.unione.io/mcp`. Nothing to install: add the URL to your
  client and sign in to UniOne through OAuth.
- **Local** — the server runs on your machine over stdio (npx, Docker or a binary) and acts with
  the API key you put into `UNIONE_API_KEY`.

## Remote server (OAuth)

URL: **`https://mcp.unione.io/mcp`**

The client opens the UniOne sign-in page on first use; you approve the access and the client
keeps the token. Revoke it at any time in your UniOne account.

**Claude Code**

```bash
claude mcp add --transport http unione https://mcp.unione.io/mcp
```

**Claude Desktop / claude.ai** — Settings → Connectors → Add custom connector, URL
`https://mcp.unione.io/mcp`.

**Cursor, Windsurf, Cline, VS Code** and other clients with an `mcpServers` config:

```json
{
  "mcpServers": {
    "unione": {
      "type": "http",
      "url": "https://mcp.unione.io/mcp"
    }
  }
}
```

A client that cannot do OAuth can send an API key instead: add the header
`Authorization: Bearer <UniOne API key>`.

## Local server (API key)

Get an API key in your UniOne account: an account key reaches the whole account, a project
key only its project. The server reads it from `UNIONE_API_KEY`.

**npx** (Node.js 18+; downloads the binary for your platform on first run):

```json
{
  "mcpServers": {
    "unione": {
      "command": "npx",
      "args": ["-y", "unione-mcp"],
      "env": { "UNIONE_API_KEY": "<your API key>" }
    }
  }
}
```

**Docker:**

```json
{
  "mcpServers": {
    "unione": {
      "command": "docker",
      "args": ["run", "-i", "--rm", "-e", "UNIONE_API_KEY", "ghcr.io/unione-repo/unione-mcp", "stdio"],
      "env": { "UNIONE_API_KEY": "<your API key>" }
    }
  }
}
```

**Binary** — download `unione-mcp-<os>-<arch>` from [Releases](https://github.com/unione-repo/unione-mcp/releases),
make it executable and put it on your `PATH` (or `go install github.com/unione-repo/unione-mcp/cmd/unione-mcp@latest`):

```json
{
  "mcpServers": {
    "unione": {
      "command": "unione-mcp",
      "args": ["stdio"],
      "env": { "UNIONE_API_KEY": "<your API key>" }
    }
  }
}
```

Claude Code, in one line:

```bash
claude mcp add unione --env UNIONE_API_KEY=<your API key> -- npx -y unione-mcp
```

Check the connection by asking *"show my UniOne account"*: the assistant calls
`get_account`.

## Tools

Every tool has a class, sent to the client as MCP annotations:

- **read** — reads only (`readOnlyHint`);
- **write** — creates or changes something, nothing is lost (`destructiveHint: false`);
- **dangerous** — deletes or overwrites for good, or cannot be taken back (`destructiveHint: true`).
  Clients ask you before such a call.

| Area | Tool | What it does | Class |
|---|---|---|---|
| Account | `get_account` | Get UniOne account | read |
| Domains | `list_domains` | List sender domains | read |
| Domains | `get_dns_records` | Add domain / get its DNS records | write |
| Domains | `validate_domain` | Check domain DNS records | read |
| Domains | `delete_domain` | Delete sender domain | **dangerous** |
| Domains | `get_sandbox_domain` | Get sandbox domain | write |
| Confirmed emails | `list_confirmed_emails` | List confirmed emails | read |
| Confirmed emails | `add_confirmed_email` | Add confirmed email | write |
| Confirmed emails | `delete_confirmed_email` | Delete confirmed email | **dangerous** |
| Sending | `send_email` | Send email (billed) | **dangerous** |
| Templates | `list_templates` | List templates | read |
| Templates | `get_template` | Get template | read |
| Templates | `set_template` | Create or overwrite template | **dangerous** |
| Templates | `delete_template` | Delete template | **dangerous** |
| Webhooks | `list_webhooks` | List webhooks | read |
| Webhooks | `get_webhook` | Get webhook | read |
| Webhooks | `set_webhook` | Create or overwrite webhook | **dangerous** |
| Webhooks | `delete_webhook` | Delete webhook | **dangerous** |
| Suppressions | `get_suppression` | Get suppression of an address | read |
| Suppressions | `list_suppressions` | List suppressions | read |
| Suppressions | `set_suppression` | Block an address | **dangerous** |
| Suppressions | `delete_suppression` | Unblock an address | **dangerous** |
| Validation | `validate_email` | Validate an email address (billed) | write |
| Event dumps | `request_event_dump` | Request event dump | write |
| Event dumps | `get_event_dump` | Get event dump | read |
| Event dumps | `delete_event_dump` | Delete event dump | **dangerous** |
| Tags | `list_tags` | List tags | read |
| Tags | `delete_tag` | Delete tag | **dangerous** |
| Projects | `list_projects` | List projects | read |
| Projects | `create_project` | Create project | write |
| Projects | `update_project` | Update project | **dangerous** |
| Projects | `delete_project` | Delete project | **dangerous** |

`send_email` sends real mail and cannot be recalled. `validate_email` is billed per
address. The tools that reach beyond your account — `send_email` and
`add_confirmed_email` mail an address, `set_webhook` points UniOne at a URL,
`validate_email` probes a mailbox — say so with `openWorldHint: true`.

### Read-only and no-send modes

Two switches narrow the tool list of a local server:

| Variable | Effect |
|---|---|
| `MCP_READ_ONLY=1` | only the **read** tools are listed: the assistant cannot change the account |
| `MCP_DISABLE_SEND=1` | `send_email` is not listed: the assistant cannot send mail |

```json
"env": { "UNIONE_API_KEY": "<your API key>", "MCP_READ_ONLY": "1" }
```

With Docker, `env` reaches only the `docker` process: pass the switch into the container with
its own `-e`, before the image name.

```json
"args": ["run", "-i", "--rm", "-e", "UNIONE_API_KEY", "-e", "MCP_READ_ONLY", "ghcr.io/unione-repo/unione-mcp", "stdio"],
"env": { "UNIONE_API_KEY": "<your API key>", "MCP_READ_ONLY": "1" }
```

They guard against mistakes of the model, not against the key's owner: the same key works in
the Web API directly. For a hard limit use a project API key.

## Configuration

Everything is an environment variable; a `.env` file in the working directory is read too.
[`.env.example`](./.env.example) lists them all. Every value is validated at start-up.

| Variable | Purpose |
|---|---|
| `UNIONE_API_KEY` | local mode: the API key every tool acts with |
| `UNIONE_API_URL` | Web API base, default `https://api.unione.io/en/transactional/api/v1` |
| `MCP_TRANSPORT` | `http` (default) or `stdio`; `unione-mcp stdio` sets it too |
| `MCP_READ_ONLY`, `MCP_DISABLE_SEND` | see [above](#read-only-and-no-send-modes) |
| `AUTH_MODE` | self-hosted http mode: `api_key` (default) or `oauth` |
| `AUTHORIZATION_SERVER_URL`, `OAUTH_SCOPES`, `OAUTH_TOKEN_PREFIX`, `MCP_PUBLIC_URL` | self-hosted oauth mode. `OAUTH_TOKEN_PREFIX` defaults to `uoat_`. |
| `LISTEN_HOST`, `PORT` | self-hosted http mode listener (default: every interface, first free port from 3000) |
| `UPSTREAM_TIMEOUT`, `UPSTREAM_MAX_BODY_BYTES`, `MCP_MAX_REQUEST_BYTES` | Web API call limits and the `/mcp` body cap |
| `LOG_LEVEL`, `LOG_FORMAT` | `debug`, `info`, `warn`, `error`; `text` or `json`, always to stderr |

The API key, the `Authorization` header and Web API response bodies are never logged.

## Documentation

- UniOne API: https://docs.unione.io
- Installing with an AI agent: [llms-install.md](./llms-install.md)
- Reporting a vulnerability: [SECURITY.md](./SECURITY.md)
- Issues and pull requests: [CONTRIBUTING.md](./CONTRIBUTING.md)

## Development

```bash
go build -o bin/unione-mcp ./cmd/unione-mcp
go test ./internal/... ./cmd/...                     # unit tests, no network

# acceptance: the real binary over HTTP and stdio against a WireMock stand-in of the API
docker run -d --rm -p 8081:8081 wiremock/wiremock:3.4.1 --local-response-templating --port 8081
E2E_WIREMOCK_URL=http://localhost:8081 go test -tags=integration ./tests/...
```

## License

[MIT](./LICENSE)
