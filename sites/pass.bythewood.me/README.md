# pass.bythewood.me

My own password server. Vaultwarden answers the official Bitwarden apps, and a
small Go page beside it reads Vaultwarden's database to show every sign in,
failed password and device, and pushes anything odd to my phone. The vault is
encrypted on each device, so neither of them ever sees a password.

## Requirements

Go for the page and Docker for Vaultwarden. Vaultwarden comes out of its
upstream image by digest and runs on `debian:trixie-slim` like everything else
here. `.env` holds the ntfy and logging tokens and `PASS_SIGNUPS`.

## Running it

```sh
make run      the page on :8000, reading PASS_VAULT (data/db.sqlite3 by default)
make build    release binary into ../../bin/pass.bythewood.me
```

From the repo root `make deploy SITE=pass.bythewood.me` builds both images and
replaces `orchard-pass` and `orchard-pass-vw`.

To make the account, set `PASS_SIGNUPS=true` in `.env`, deploy, sign up from a
Bitwarden app with `https://pass.bythewood.me` as the self-hosted server, then set
it back to `false` and deploy again. The page warns while it's on.

To update Vaultwarden, change the tag and digest on the `upstream` stage in the
`Dockerfile` and deploy. The page pushes when a new release is out.

## Layout

`vault.go` reads Vaultwarden's database, `watch.go` turns its event log and
device list into log records and pushes and checks the release, and `pages.go`
is the overview, logins and devices. `suite/`, `ntfy/` and `web/` are this
site's copies of the shared packages. Caddy sends `/api`, `/identity`, `/icons`,
`/notifications`, `/events`, `/attachments` and `/alive` to Vaultwarden and the
rest here.

## Things that will bite you

- Sign ins and failures only reach the database with `ORG_EVENTS_ENABLED`, and
  a wrong email isn't recorded at all, since there's no account to put it on.
- A token refresh isn't a sign in, so the apps' everyday use shows as a
  device's last active time and never as an event.
- The volume is mounted writable into the page because SQLite needs the `-shm`
  file to read a WAL database. The page opens it read only.
- There's no admin page and no web vault. Signing a device out is done from a
  Bitwarden app, and changing the master password signs out every device.
- The nightly dump copies `orchard-pass-data` like every other volume, which
  is the database, `rsa_key.pem` and attachments. Losing the key only signs
  every device out.

## License

BSD 2-Clause, like the rest of orchard.
