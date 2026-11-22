# auth.bythewood.me

The front door for every site here. There's one account, signing in is a
username and then a six digit code pushed to a phone over ntfy, and the session
is an opaque cookie on `.bythewood.me` that the other sites check by asking
`/verify` on every request. Ten recovery codes get you in when ntfy can't.

## Requirements

Go and bun. The `.env` from `make env` at the repo root carries
`AUTH_NTFY_TOKEN`, the write token for the `auth` topic that `make ntfy-token`
mints, and empty means only recovery codes work. It also carries
`SESSION_DOMAIN`, which is `.bythewood.me` in production and empty on localhost.

## Running it

```sh
make run      vite watch + go run, on :8000
make build    release binary into ../../bin/auth.bythewood.me
make clean    delete build/ and node_modules
```

From the repo root `make auth-init` creates the account and prints its first
recovery codes, and `make auth-recovery` replaces them from the machine.

## Layout

`login.go` and `otp.go` are the sign in, `device.go` the known device cookie, `session.go` the cookie, `handlers.go`
the account pages and `/verify`, `overview.go` the checkup tiles, new place
flags and activity filters those pages share, `connect.go` the consent page, `credentials.go` the Argon2id hashing,
`ntfy.go` what gets pushed and `ntfy/` the push, and `init.go` the `-init`,
`-check` and `-recovery` modes.
State is `data/db.sqlite3`, or the `orchard-auth-data` volume in production.

## Things that will bite you

- With `SESSION_DOMAIN` set locally the login reports success and then doesn't
  stick, since a `.bythewood.me` cookie is never sent to localhost.
- A new place is a city or country with no sign in in the 30 days before, so a
  fresh database or a wiped events table calls nothing new until it has history.
- Connector call counts live on mcp. auth only knows the days a connector's
  session was used and when it was last checked, to within five minutes.
- The day's lockout and the one outstanding code rule only hold back a browser
  without the known device cookie, which a sign in here sets for a year. A
  stranger's wrong codes can't lock out a browser you've used before, and that
  browser gets its own lockout and hourly ceiling counted from its own kind.
- Caddy refuses `/verify` on the public hostname, so only the bridge reaches it.
- With this container down every gated site treats you as signed out.
