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
set -e

VOLUME=orchard-cloudflared
TUNNEL=orchard
IMAGE=cloudflare/cloudflared:latest
HOSTNAMES="next.isaacbythewood.com"

# cloudflared's image is distroless with no shell, so anything that needs to
# poke at the volume borrows a plain alpine.
volume_sh() {
	docker run --rm -v "$VOLUME:/etc/cloudflared" alpine:3 sh -c "$1"
}

ensure_volume() {
	docker volume inspect "$VOLUME" >/dev/null 2>&1 || docker volume create "$VOLUME" >/dev/null
	# The image runs as uid 65532 and a fresh volume is root-owned. Skipping
	# this makes `tunnel login` authenticate successfully, receive the cert,
	# then die with EACCES writing cert.pem, burning a one-time callback token.
	volume_sh "chown -R 65532:65532 /etc/cloudflared"
}

case "${1:-}" in
login)
	ensure_volume
	# No TTY on purpose. login only prints a URL and polls the callback, so
	# `docker run -it` fails with "the input device is not a TTY" when run
	# from a non-interactive context.
	docker run --rm -v "$VOLUME:/etc/cloudflared" "$IMAGE" tunnel login
	;;

up)
	ensure_volume
	docker run --rm -v "$VOLUME:/etc/cloudflared" "$IMAGE" tunnel create "$TUNNEL" || true

	ID=$(docker run --rm -v "$VOLUME:/etc/cloudflared" "$IMAGE" tunnel list --output json \
		| tr ',' '\n' | grep -A0 '"id"' | head -1 | sed 's/.*"id":"//;s/".*//')
	if [ -z "$ID" ]; then
		echo "could not determine tunnel id; run '$0 status'" >&2
		exit 1
	fi
	echo "tunnel id: $ID"

	for host in $HOSTNAMES; do
		docker run --rm -v "$VOLUME:/etc/cloudflared" "$IMAGE" \
			tunnel route dns "$TUNNEL" "$host" || true
	done

	# Seeded over stdin rather than mounted, same reason as everything else.
	sed "s/CHANGEME_TUNNEL_ID/$ID/" cloudflared/config.yml \
		| docker run --rm -i -v "$VOLUME:/etc/cloudflared" alpine:3 \
			sh -c 'cat > /etc/cloudflared/config.yml && chown 65532:65532 /etc/cloudflared/config.yml'

	echo "config.yml written. now: docker compose up --build --detach"
	;;

status)
	echo "--- volume ---"
	volume_sh "ls -la /etc/cloudflared" || echo "no volume"
	echo "--- tunnels ---"
	docker run --rm -v "$VOLUME:/etc/cloudflared" "$IMAGE" tunnel list || true
	echo "--- containers ---"
	docker ps --filter name=orchard --format '{{.Names}}\t{{.Status}}'
	;;

down)
	docker compose down --remove-orphans || true
	docker run --rm -v "$VOLUME:/etc/cloudflared" "$IMAGE" tunnel delete -f "$TUNNEL" || true
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
	sed -n '2,12p' "$0"
	exit 1
	;;
esac
