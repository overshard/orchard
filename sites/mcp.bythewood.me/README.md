# mcp.bythewood.me

Every orchard tool, read only, as one MCP server. Add `https://mcp.bythewood.me/mcp`
as a connector in Claude and it signs in through auth.bythewood.me with the
phone code, then reads status, logging, analytics, cron and repos as you.

## Requirements

Go. It reads no secrets, so there's no `.env`. The tools it reads are reached
by container name, so outside the bridge the tool calls fail and the rest works.

## Running it

```sh
make run      go run, with the database in data/
make build    release binary into ../../bin/mcp.bythewood.me
```

From the repo root `make deploy SITE=mcp.bythewood.me` builds the image and replaces
`orchard-mcp`.

## Layout

`oauth.go` is the sign in: the two metadata documents, `/authorize`, the
return from auth at `/oauth/back` and `/token`. `mcp.go` is the one JSON-RPC
endpoint and `tools.go` the tools, each a read of a tool's markdown report,
JSON API or `/suite/state` over the bridge. `store.go` keeps grants and hashed
tokens in SQLite. `suite/` and `web/` are this site's copies of the shared look
and HTTP layer.

## Things that will bite you

- auth mints each connector a session of its own after a fresh code, and that
  session is what every read is sent as. Revoking it on auth's sessions page
  ends the grant on the agent's next call.
- Only Claude's two client metadata documents may start a sign in, see
  `knownClients`. Another client means adding its URL there.
- Cloudflare blocks every path starting with a dot, so the WAF rule needs an
  exception for this host's `/.well-known/oauth-` paths or no client can find
  the sign in.
- It speaks 2026-07-28 to a client that sends `_meta` and `Mcp-Method`, and the
  older initialize handshake to one that doesn't.

## License

BSD 2-Clause, like the rest of orchard.
