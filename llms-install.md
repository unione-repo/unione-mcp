# Installing unione-mcp — instructions for an AI agent

You are installing the MCP server for UniOne (transactional email API (https://docs.unione.io)) into the
user's MCP client. Follow the steps in order.

## 1. Choose how to connect

Ask the user which they prefer, and default to **remote** when they have no preference:

- **Remote** (recommended): nothing to install, the user signs in to UniOne in the browser.
- **Local**: the server runs on the user's machine and needs a UniOne API key.

## 2a. Remote

Add this server to the client's MCP settings (for Cline: `cline_mcp_settings.json`):

```json
{
  "mcpServers": {
    "unione": {
      "type": "streamableHttp",
      "url": "https://mcp.unione.io/mcp"
    }
  }
}
```

Clients other than Cline use `"type": "http"`. On the first tool call the client opens the
UniOne sign-in page; tell the user to sign in and approve the access.

## 2b. Local

1. Ask the user for their UniOne API key (from the UniOne account settings). Do not print it back, do
   not write it anywhere except the MCP settings file, and never commit it.
2. Check that Node.js 18 or newer is installed: `node --version`. If it is not, use the Docker
   variant below instead, or ask the user to install Node.js.
3. Add the server to the MCP settings:

```json
{
  "mcpServers": {
    "unione": {
      "command": "npx",
      "args": ["-y", "unione-mcp"],
      "env": { "UNIONE_API_KEY": "<the user's API key>" }
    }
  }
}
```

Docker variant (needs Docker running):

```json
{
  "mcpServers": {
    "unione": {
      "command": "docker",
      "args": ["run", "-i", "--rm", "-e", "UNIONE_API_KEY", "ghcr.io/unione-repo/unione-mcp", "stdio"],
      "env": { "UNIONE_API_KEY": "<the user's API key>" }
    }
  }
}
```

If the user wants the assistant not to change the account, add `"MCP_READ_ONLY": "1"` to `env`;
to forbid sending mail only, add `"MCP_DISABLE_SEND": "1"`. With Docker, `env` reaches only the
`docker` process, so also add `"-e", "MCP_READ_ONLY"` (or `"-e", "MCP_DISABLE_SEND"`) to `args`
before the image name:

```json
"args": ["run", "-i", "--rm", "-e", "UNIONE_API_KEY", "-e", "MCP_READ_ONLY", "ghcr.io/unione-repo/unione-mcp", "stdio"],
"env": { "UNIONE_API_KEY": "<the user's API key>", "MCP_READ_ONLY": "1" }
```

## 3. Verify

Reload the MCP servers, then call the tool `get_account` with no arguments. A result
with `"status": "success"` and the account's email means the installation works.

- An error about a missing or rejected API key: re-check `UNIONE_API_KEY` (local) or ask the user
  to sign in again (remote).
- `npx` cannot download the binary: the machine may have no access to GitHub; use Docker.

## 4. Tell the user

Say that UniOne is connected, which way (remote or local), and that tools marked dangerous
(deleting, sending) will ask for confirmation. `send_email` sends real mail.
