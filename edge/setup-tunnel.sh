#!/bin/sh
# Create, inspect and tear down the Cloudflare Tunnel that fronts this repo.
#
#   sh setup-tunnel.sh login    once per machine: browser auth, writes cert.pem
#   sh setup-tunnel.sh up       create the tunnel, route DNS, write config.yml
#   sh setup-tunnel.sh status   what exists right now
#   sh setup-tunnel.sh down     delete the tunnel and its volume
#
# All cloudflared state lives in a named volume, never a bind mount: the Docker
# CLI in the webdev container talks to Docker Desktop on the Windows host, whose
# daemon cannot see this filesystem, and a bind mount silently resolves to an
# empty directory rather than failing.
#
# Every docker command goes through sudo, because the socket in the webdev
# container is root:root mode 660 and being in the docker group does not help.
# On a host where docker needs no sudo:  SUDO= sh setup-tunnel.sh up
set -e

SUDO=${SUDO-sudo}
DOCKER_BIN=$(command -v docker) || { echo "no docker on PATH" >&2; exit 1; }
# An absolute path, because `command` is a shell builtin and sudo cannot exec
# one, and because sudo's secure_path is not this shell's PATH.
docker() { ${SUDO} "$DOCKER_BIN" "$@"; }

VOLUME=orchard-cloudflared
TUNNEL=orchard
IMAGE=cloudflare/cloudflared:latest
# Every hostname the tunnel serves, matching the ingress rules in
# cloudflared/config.yml. These span two zones, and cert.pem only covers one at
# a time (see the note in `up`), so routing all of them means logging in once
# per zone and running `up` again; the per-host failures are tolerated.
HOSTNAMES="isaacbythewood.com www.isaacbythewood.com bythewood.me www.bythewood.me blog.bythewood.me analytics.bythewood.me status.bythewood.me"

# cloudflared's image is distroless with no shell, so anything that needs to
# poke at the volume borrows a plain alpine.
volume_sh() {
	docker run --rm -v "$VOLUME:/etc/cloudflared" alpine:3 sh -c "$1"
}

# Every command except login has to be told where the origin cert is. Its
# default is $HOME/.cloudflared, and the volume lives at /etc/cloudflared for
# everything else, so without this they report "cert.pem not found" and suggest
# logging in again on a machine that already has.
cfd() {
	docker run --rm -v "$VOLUME:/etc/cloudflared" "$IMAGE" \
		--origincert /etc/cloudflared/cert.pem "$@"
}

ensure_volume() {
	docker volume inspect "$VOLUME" >/dev/null 2>&1 || docker volume create "$VOLUME" >/dev/null
	# The image runs as uid 65532 and a fresh volume is root-owned. Without this
	# `tunnel login` authenticates, receives the cert, then dies with EACCES
	# writing cert.pem, burning a one-time callback token.
	volume_sh "chown -R 65532:65532 /etc/cloudflared"
}

case "${1:-}" in
login)
	ensure_volume
	# Mounted at the image's HOME rather than /etc/cloudflared, because login
	# ignores --origincert and writes to $HOME/.cloudflared unconditionally.
	# Mounting elsewhere means it reports success, writes the cert into the
	# container layer and exits, taking the one-time callback token with it.
	#
	# No TTY either: login only prints a URL and polls the callback, so
	# `docker run -it` fails with "the input device is not a TTY" from a
	# non-interactive context.
	docker run --rm -v "$VOLUME:/home/nonroot/.cloudflared" "$IMAGE" tunnel login
	volume_sh "chown -R 65532:65532 /etc/cloudflared"
	;;

up)
	ensure_volume
	cfd tunnel create "$TUNNEL" || true

	# The credentials file is named after the tunnel id, so the id can be read
	# straight off the volume instead of parsing JSON out of `tunnel list`.
	ID=$(volume_sh "ls /etc/cloudflared" | grep -E '^[0-9a-f-]{36}\.json$' | head -1 | sed 's/\.json$//')
	if [ -z "$ID" ]; then
		echo "could not determine tunnel id; run '$0 status'" >&2
		exit 1
	fi
	echo "tunnel id: $ID"

	# ONE ZONE ONLY. cert.pem carries a single zoneID, chosen in the browser
	# during `login`, and this command can only write into that zone. Handed a
	# hostname from another zone it does not fail: it treats the whole string as
	# a subdomain and creates "blog.bythewood.me.isaacbythewood.com".
	#
	# ONE LABEL ONLY, too. Cloudflare's free Universal SSL signs the apex and a
	# single wildcard level, so a two-label host like next.blog.bythewood.me has
	# no certificate and fails the TLS handshake at the edge. Use a hyphen
	# instead.
	#
	# Routing a second zone means logging in again and picking it, which
	# replaces cert.pem. That is safe for a running tunnel, which authenticates
	# with the credentials JSON rather than this, but only one zone can be
	# managed from here at a time.
	for host in $HOSTNAMES; do
		cfd tunnel route dns "$TUNNEL" "$host" || true
	done

	# Seeded over stdin rather than mounted, same reason as everything else.
	# The id appears twice in the template, as the tunnel and in the credentials
	# filename, so this substitution has to be global.
	sed "s/CHANGEME_TUNNEL_ID/$ID/g" cloudflared/config.yml \
		| docker run --rm -i -v "$VOLUME:/etc/cloudflared" alpine:3 \
			sh -c 'cat > /etc/cloudflared/config.yml && chown 65532:65532 /etc/cloudflared/config.yml'

	echo "config.yml written. now, from the repo root: make up"
	;;

status)
	echo "--- volume ---"
	volume_sh "ls -la /etc/cloudflared" || echo "no volume"
	echo "--- tunnels ---"
	cfd tunnel list || true
	echo "--- containers ---"
	docker ps --filter name=orchard --format '{{.Names}}\t{{.Status}}'
	;;

down)
	docker compose down --remove-orphans || true
	cfd tunnel delete -f "$TUNNEL" || true
	docker volume rm "$VOLUME" || true
	# `tunnel route dns` creates records but cloudflared has no delete
	# counterpart, so the CNAME outlives the tunnel and has to go from the
	# dashboard. Until it does, the hostname answers 530 (error 1033), which
	# is the signature of a disconnected tunnel.
	echo
	echo "NOTE: the CNAME for $HOSTNAMES still exists and must be deleted"
	echo "in the Cloudflare dashboard. Until then it returns 530."
	;;

*)
	sed -n '2,8p' "$0"
	exit 1
	;;
esac
