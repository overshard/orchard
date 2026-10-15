# The edge

One cloudflared and one Caddy serve the whole repo. Traffic is outbound only,
so there's no port forwarding or dynamic DNS and the origin IP is never
published. Every container listens on 8000 internally and nothing is published
to the host.

Adding a hostname takes five changes, and `CLAUDE.md` has them in order along
with the reseed that's easy to forget.

Every `reverse_proxy` imports `hold`, which sets `lb_try_duration` so Caddy
keeps retrying the container for up to 40 seconds instead of answering 502. A
deploy stops the old container before the new one starts, so without it a
deploy answers 502 until the new one is up, and with it the same requests wait
and then succeed.

Caddy also writes its access log to `logging.bythewood.me`, over a plain socket
on port 9001 rather than through the shipper every site uses, since Caddy can't
carry a Go handler. It keeps writing the same lines to stderr as well, so
`docker logs orchard-caddy` is unchanged. cloudflared and ntfy don't ship
anywhere, because neither can write its log to a network address and pointing
either at a file takes its stdout away.

ntfy runs in the edge and is how anything here reaches a phone. status publishes
outage transitions, logging publishes silence transitions and auth publishes
login codes, all over the internal bridge. Reading is over the tunnel with a
read only account.

An alerter running on this machine can't tell you this machine lost power. A
tunnel that drops is covered from the other side, since Cloudflare sends its own
tunnel health notification.
