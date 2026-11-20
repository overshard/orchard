# mcp.bythewood.me

Every orchard tool as one MCP server, read only except `gate_propose`, which
adds a gate rule that only watches until you turn it on. Add `https://mcp.bythewood.me/mcp`
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
JSON API or `/suite/state` over the bridge. `store.go` keeps grants, hashed
tokens and every call in SQLite. `stats.go` sums each tool up for `/tools` and
folds errors into causes, and `raw.go` rebuilds a call's JSON-RPC request and
reply from what's kept for the raw view on `/calls`. `suite/` and `web/` are this site's copies of the shared look
and HTTP layer.

## Things that will bite you

- auth mints each connector a session of its own after a fresh code, and that
  session is what every read is sent as. Revoking it on auth's sessions page
  ends the grant on the agent's next call.
- Only Claude's two client metadata documents may start a sign in, see
  `knownClients`. Another client means adding its URL there.
- gate refuses every path starting with a dot except `/.well-known/oauth-`,
  so a new `.well-known` route here needs gate's `dotfiles` rule changed too.
- The raw view is rebuilt, not recorded. Only the id, the arguments (cut at
  2,000 characters) and the first 600 characters of the answer are kept, and
  the docs page says so, so keeping more means changing that note too.
- A connector's session on auth lasts 90 days from when it was allowed and
  doesn't slide, which is where the expiry on the overview comes from.
- It speaks 2026-07-28 to a client that sends `_meta` and `Mcp-Method`, and the
  older initialize handshake to one that doesn't.

## License

BSD 2-Clause, like the rest of orchard.
