# pass.bythewood.me

My own password server. Vaultwarden answers the official Bitwarden apps, and a
small Go page beside it reads Vaultwarden's database to show every sign in,
failed password and device, and pushes anything odd to my phone. The vault is
encrypted on each device, so neither of them ever sees a password.

## Requirements

Go for the page and Docker for Vaultwarden. Vaultwarden comes out of its
upstream image by digest and runs on `debian:trixie-slim` like everything else
here. `.env` holds the ntfy and logging tokens and the admin token with its
hash, see `.env.example`.

## Running it

```sh
make run      the page on :8000, reading PASS_VAULT (data/db.sqlite3 by default)
make build    release binary into ../../bin/pass.bythewood.me
```

From the repo root `make deploy SITE=pass.bythewood.me` builds both images and
replaces `orchard-pass` and `orchard-pass-vw`.

To make the account, switch signups on from the account page and sign up from
a Bitwarden app with `https://pass.bythewood.me` as the self-hosted server.
Signups switch themselves off once the account exists. Two step login is set
up on the same page with a QR code for Aegis or any authenticator app.

To update Vaultwarden, change the tag and digest on the `upstream` stage in the
`Dockerfile` and deploy. The page pushes when a new release is out.

## Layout

`vault.go` reads Vaultwarden's database and writes the authenticator setting,
`admin.go` switches signups and signs devices out through Vaultwarden's admin
API over the bridge, `totp.go` is the authenticator codes, `watch.go` turns its event log and
device list into log records and pushes and checks the release, and `pages.go`
is the overview, logins and devices, with the account page in `account.go`. `suite/`, `ntfy/` and `web/` are this
site's copies of the shared packages. Caddy sends `/api`, `/identity`, `/icons`,
`/notifications`, `/events`, `/attachments` and `/alive` to Vaultwarden and the
rest here.

## Things that will bite you

- Sign ins and failures only reach the database with `ORG_EVENTS_ENABLED`, and
  a wrong email isn't recorded at all, since there's no account to put it on.
- A token refresh isn't a sign in, so the apps' everyday use shows as a
  device's last active time and never as an event.
- There's no web vault, which is where the apps send you for two step login.
  The account page does it instead, writing the same row Vaultwarden's own API
  writes.
- Vaultwarden is on its own `orchard-pass` network, which the edge compose
  creates, with only Caddy and this page on it beside it. So `make up`
  has to have run the edge first. Caddy never routes `/admin`, and signups
  switched there are saved in `config.json` in the volume, which wins over
  `SIGNUPS_ALLOWED` in compose.
- The apps' paths skip gate, since its scanner rule matched `/api/config` and a
  ban would lock the phone out. Vaultwarden's own limit of five sign ins a
  minute per address is what's left, and every miss pushes.
- The page mounts the whole volume writable, so a bug in it could touch
  `rsa_key.pem` or attachments. It only writes the twofactor row and events.
- A Vaultwarden upgrade migrates its database one way, so `make deploy`
  rolling the images back can't undo it. Run `orchard-dump` first.
- The page asks gate for each address's network and country, which shows in
  gate's traffic as requests to `/__pass/where`.
- The nightly dump copies `orchard-pass-data` like every other volume, which
  is the database, `rsa_key.pem` and attachments. Losing the key only signs
  every device out.

## License

BSD 2-Clause, like the rest of orchard.
