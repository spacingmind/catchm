# Plan: local MCP server

- Status: in progress
- Created: 2026-11-27
- Branch: `feat/cloud-mode` (rides with PR #44)

## Why

Meetings are already agent-readable files. A local MCP server lets
Claude Desktop, Claude Code, Cursor and other MCP clients search and
read them as tools, without copying files around. The landing page has
promised "MCP server: coming soon".

## Decisions (owner, 2026-11-27)

- **Two transports:**
  - **stdio:** the MCP client runs `catchm-desktop mcp` (or
    `catchm mcp`) itself. It reads `~/.catchm` directly, opens no port,
    and works while the app is closed.
  - **Local HTTP:** Streamable HTTP at `http://127.0.0.1:<port>/mcp`,
    for clients that only take a URL. The running app starts it when
    the user turns it on. It's guarded by a bearer token and a
    loopback-only Host/Origin check (DNS rebinding).
- **Tools are read-only by default.** A separate "Allow changes" switch
  adds the write tools.
- **SDK:** the official `github.com/modelcontextprotocol/go-sdk` v1.

## Tools

| Tool | Kind | What |
|---|---|---|
| `list_meetings` | read | Newest first; `limit` (default 20), optional `project` |
| `search_meetings` | read | Full-text `query` over titles, summaries, transcripts |
| `get_meeting` | read | Title, times, languages, project, tags, summary, decisions, action items, notes |
| `read_transcript` | read | `source`: best (default), live or refined; one line per utterance with time, speaker, text and translation |
| `list_action_items` | read | Across meetings; `open_only` (default true), optional `project` |
| `list_projects` | read | Name, meeting count, last meeting |
| `add_note` | write | Append a note to a meeting |
| `rename_meeting` | write | Change a meeting's title |
| `set_meeting_tags` | write | Replace a meeting's tags |

## Config and API

```yaml
mcp:
  enabled: false        # off by default; stdio refuses to start while off
  allow_writes: false
  http:
    enabled: false
    port: 4655
    token: <random, generated on first enable>
```

- `GET /api/mcp` returns:
  - the switches;
  - HTTP status (`running`, `error`, `url`, `token`). The token is shown
    to the local UI, since it's needed to configure a client;
  - the stdio command (`os.Executable()` + `mcp`).
- `PUT /api/mcp` takes a partial update.
- `POST /api/mcp/token` makes a new token.
- The HTTP listener restarts on any change.

## UI

Settings → **MCP** has these parts:

- switches for Enable, Allow changes and Local HTTP (with port);
- copy-paste setup for:
  - Claude Desktop (`claude_desktop_config.json`);
  - Claude Code (`claude mcp add catchm -- <path> mcp`);
  - Cursor (`.cursor/mcp.json`);
  - HTTP (URL + `Authorization: Bearer <token>`);
- a regenerate-token button;
- a warning that writes change meeting files.

## Acceptance criteria

1. **stdio.** With MCP enabled, `catchm mcp` / `catchm-desktop mcp`
   answer `initialize` and `tools/list` over stdio. Disabled, they exit
   with a message naming the setting.
2. **Write switch.** Write tools are listed only when Allow changes is on.
3. **HTTP.** With Local HTTP on:
   - a request without the right token gets 401;
   - a non-loopback Origin or Host gets 403;
   - a request with the right token can list tools.
   Turning it off closes the port.
4. **Tools.** Each tool returns the documented data for a fixture store
   (unit tests through an in-memory client/server pair).
5. **Checks.** `task lint` and `task test` pass.
