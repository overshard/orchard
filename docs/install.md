# Installing and running orchard

## From nothing

A fresh clone on a machine where none of this exists, or a machine where it all
went down at once:

```sh
make install
```

That opens a browser to authorise a Cloudflare zone, creates the tunnel and
routes every hostname to it, writes a `.env` for each site that needs one,
brings the edge and every site up, creates the three ntfy accounts, mints the
publishers' tokens into the `.env` files that use them, hands them to the
running sites, creates the login account and prints its recovery codes, and ends
on `doctor`.

Nothing asks you to invent a secret. The ntfy passwords, the publishers' tokens
and the ten recovery codes the first sign in uses are all generated and printed
as it goes. Those are the only copies, so put them in Bitwarden while they are on
screen. `make password` prints another suggestion whenever you want one.

Signing in is on `auth.bythewood.me`, a username and then a six digit code pushed
to a phone over ntfy. The other sites hold no password of their own and ask it
whether the cookie on a request is a live session, so a session revoked there is
gone everywhere on the next request.

`cert.pem` covers a single Cloudflare zone, and the hostnames here span two, so
the DNS routes for the second zone fail the first time through. Run
`make tunnel-login` and `make tunnel` again, pick the other zone in the browser,
and the rest are created.

Then point the ntfy Android client at `https://ntfy.bythewood.me` with the
reading account it printed, and subscribe to `status`, `logging` and `auth`.

Each step is a target of its own, for a run that stopped halfway:

```sh
make tunnel-login   # browser auth, one Cloudflare zone at a time
make tunnel         # create the tunnel, route DNS, write config.yml
make env            # a .env per site that ships a .env.example
make up             # the edge, then every site, ending in doctor
make ntfy           # the three ntfy accounts
make ntfy-token     # the publishers' tokens, into the .env files that use them
make up             # hand the tokens to the sites that publish with them
make auth-init      # the login account, and its recovery codes
```

`up` has to come before `ntfy`, since the accounts are created inside a running
container. Everything else is safe to re-run at any time, and `make env` never
touches a `.env` that already exists.

A different domain means editing three things before the tunnel step, the
`HOSTNAMES` line in `edge/setup-tunnel.sh`, the ingress rules in
`edge/cloudflared/config.yml`, and the site blocks in `edge/caddy/Caddyfile`.
Each site's own hostname is a constant in its `site.go`.

## After you change something

```sh
make deploy SITE=blog.bythewood.me
```

That's the only command that rebuilds. Nothing needs exporting first, since
the sites with secrets read them from a `.env` beside their compose file.
Editing anything in `edge/` needs `make edge` instead, because those configs are
baked into images and `make up` doesn't rebuild.

## When something is broken

```sh
make doctor
```

Read only. It prints the tunnel credentials, the network, the edge containers,
every site, and the SQLite volumes with their sizes, and puts the command that
fixes it next to anything wrong. A container that's stopped or unhealthy
usually wants `make up`, and one serving the wrong thing wants `make deploy`.

## sudo

The development targets touch no Docker. Everything else goes through `sudo`,
because the socket in the webdev container is root-owned mode 660. On a host
where docker needs no sudo, pass `SUDO=`, like `make up SUDO=`.
