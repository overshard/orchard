# gate.bythewood.me

The firewall in front of every orchard site. Caddy asks gate about each request
with `forward_auth` before a site sees it, and gate answers from rules kept in
SQLite and held in memory, so a rule added on the page or proposed by an agent
applies on the next request with no Caddyfile edit and no edge restart.

## Requirements

Go. No secrets, the key that signs challenge cookies is made on first boot and
kept in the database. It downloads DB-IP's free country and ASN databases
(CC BY 4.0, from db-ip.com) into its data directory on boot and once a month.

## Running it

```sh
make run      go run, with the database and the geo files in data/
make build    release binary into ../../bin/gate.bythewood.me
```

From the repo root `make deploy SITE=gate.bythewood.me` builds the image and
replaces `orchard-gate`. Every site goes through it, so a deploy holds requests
for the few seconds it takes, the same way any other deploy does.

## Layout

`engine.go` is `/check`, what Caddy calls, and the order rules run in.
`rules.go` parses and matches a rule's condition lines. `pow.go` is the proof
of work page and `/__gate/solve`. `stats.go` counts in memory and `store.go`
flushes it to SQLite every 15 seconds. `seed.go` is the rules an empty
database starts with. `api.go` is the markdown reports and the propose
endpoint the mcp connector uses. `suite/` and `web/` are this site's copies of
the shared look and HTTP layer.

## Things that will bite you

- With gate down Caddy can't ask, and every site answers 502 until it's back.
  The settings page has a watch only mode and an off mode for a rule that
  refuses the wrong thing.
- `/check` skips `web.Logged`, since a log line per check would
  double every request in logging. Caddy's access log already has the request.
- Requests from private ranges, loopback and the addresses on the settings page
  are never checked. The box's own public address belongs there, or status'
  probes of the public hostnames are checked like a stranger's.
- Anyone signed in to orchard skips every rule, but auth is only asked once
  something would have refused them, so a normal request costs no call.
- A field gate doesn't know fails its line either way. With no country for an
  address, `not country is US` doesn't match, so the sign in rule lets it
  through to auth's own per account ceiling.
- The host comes from the Host header. Caddy keeps the visitor's on the way to
  `/check`, and `X-Forwarded-Host` is passed through from the client, so it
  can't be trusted.
- The scanner rule's regex is the edge's list moved over whole, and some clauses
  look wrong and aren't. `/.git`, `/.aws`, `/.ssh` and `/.vscode` keep their
  slash or `/orchard.git/info/refs` matches and clone and push break. `.env` is
  bare and only safe since repos has its own rule that skips repository pages.
  `/media/system/` keeps the segment since blog serves `/media/images/`.
  `Dockerfile` and the archive extensions are end anchored, and the config
  names are capped at two path segments so a hashed asset under `/static/`
  isn't taken.
- A pass cookie is tied to the host and the user agent, so a browser update
  sends the visitor through the challenge once more.

## License

BSD 2-Clause, like the rest of orchard.
