# dns.bythewood.me

A private DNS resolver for my own devices. Each device gets a token, which is
its name for DNS over TLS and the path of its DNS over HTTPS URL, and anything
without one gets nothing back. Ads, trackers and malware are blocked from
hagezi's lists, every lookup is logged by the device that asked, and the rest
is resolved by unbound recursing from the root itself.

## Requirements

Go and Docker. For DNS over TLS on port 853 it needs `CLOUDFLARE_TOKEN` in
`.env`, a Cloudflare API token with Zone DNS Edit and Zone Read on
bythewood.me. It writes the `*.dns.bythewood.me` A record and the TXT record
that proves the Let's Encrypt wildcard. DNS over HTTPS works without it.
`make env` fills in `DNS_BRIDGE_PASSWORD`, which the site and the front share.

## Running it

```sh
make run      go run against 1.1.1.1, with the database and lists in data/
make build    release binary into ../../bin/dns.bythewood.me
```

From the repo root `make deploy SITE=dns.bythewood.me` builds and replaces
four containers. `orchard-dns` is the site, DoH and the log, `orchard-dns-dot`
is the same binary run with `-dot` and is the only container with a published
port, `orchard-dns-acme` runs it with `-acme`, holds the Cloudflare token and
listens on nothing, and `orchard-dns-unbound` is unbound from Debian on its own
network with the site.

## Layout

`resolve.go` decides and answers a lookup and batches the log. `lists.go`
fetches the lists and packs them into one sorted table. `doh.go` is `/q/<token>`
and the `/internal/` routes the front uses. `dot.go` is the front, which holds
the certificate and nothing else, asks gate about each connection and hands
every query to `orchard-dns` over the bridge. `acme.go` gets the certificate
through Cloudflare. `store.go` is SQLite, `pages.go` and `templates/` the
pages, `report.go` the JSON mcp reads, `metrics.go` `/internal/metrics` and
unbound's stats. `unbound/` is its image and config.

## Things that will bite you

- The token is sent in plaintext in the TLS server name, so the network a
  device is on can read it. It only grants lookups, and a new one is a button.
- Android's Private DNS doesn't fall back. With the box down the phone has no
  DNS until it's switched to Automatic.
- A list refresh that shrinks by half or grows threefold is refused and the
  copy on disk kept. The lists page says when that happened.
- The `pro` list is hagezi's Pro++, which blocks `doubleclick.net` and
  `googleadservices.com`, so sponsored search results and some Gmail ad links
  won't open. Pro leaves those alone.
- Windows wants the server's address beside the DoH template, and those are
  Cloudflare's for this name. The device page shows them as they are now.

## License

BSD 2-Clause, like the rest of orchard.
