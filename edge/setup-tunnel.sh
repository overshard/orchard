#!/bin/sh
# Create, inspect and tear down the Cloudflare Tunnel that fronts this repo. Run
# these from the repo root, where the first three are make targets:
#
#   make tunnel-login           once per zone: browser auth, writes cert.pem
#   make tunnel                 create the tunnel, route DNS, write config.yml
#   make tunnel-status          what exists right now
#   sh edge/setup-tunnel.sh down    delete the tunnel and its volume
#
# `down` has no target on purpose, since deleting the tunnel is not something to
# have one keystroke away from `make doctor`.
#
# All cloudflared state lives in a named volume, never a bind mount. The Docker
# CLI here talks to Docker Desktop on the Windows host, whose daemon cannot see
# this filesystem, so a bind mount silently resolves to an empty directory.
#
# Every docker command goes through sudo, because the socket in the webdev
# container is root:root mode 660 and being in the docker group does not help.
# On a host where docker needs no sudo:  make tunnel SUDO=
set -e

# Resolved before the cd below, since $0 is relative to the caller's
# directory and stops resolving the moment we leave it.
SELF="$(cd "$(dirname "$0")" && pwd)/$(basename "$0")"

# Run from this script's own directory, whatever the caller's was, since every
# path below is relative to edge/.
cd "$(dirname "$0")"

SUDO=${SUDO-sudo}
DOCKER_BIN=$(command -v docker) || { echo "no docker on PATH" >&2; exit 1; }
# An absolute path, because `command` is a shell builtin and sudo cannot exec
# one, and because sudo's secure_path is not this shell's PATH.
docker() { ${SUDO} "$DOCKER_BIN" "$@"; }

VOLUME=orchard-cloudflared
TUNNEL=orchard
IMAGE=cloudflare/cloudflared:latest
# Every hostname the tunnel serves, matching the ingress rules in
# cloudflared/config.yml. These span two zones and cert.pem covers one at a time,
# so routing all of them means logging in once per zone and running `up` again.
# The per-host failures in between are expected.
HOSTNAMES="isaacbythewood.com www.isaacbythewood.com bythewood.me www.bythewood.me blog.bythewood.me analytics.bythewood.me status.bythewood.me logging.bythewood.me ntfy.bythewood.me repos.bythewood.me dash.bythewood.me"

# cloudflared's image is distroless with no shell, so anything that needs to
# poke at the volume borrows a plain alpine.
volume_sh() {
	docker run --rm -v "$VOLUME:/etc/cloudflared" alpine:3 sh -c "$1"
}

# Every command except login has to be told where the origin cert is, or it
# looks in $HOME/.cloudflared, reports "cert.pem not found" and tells you to log
# in again on a machine that already has.
cfd() {
	docker run --rm -v "$VOLUME:/etc/cloudflared" "$IMAGE" \
		--origincert /etc/cloudflared/cert.pem "$@"
}

ensure_volume() {
	docker volume inspect "$VOLUME" >/dev/null 2>&1 || docker volume create "$VOLUME" >/dev/null
	# The image runs as uid 65532 and a fresh volume is root-owned. Without this
	# `tunnel login` authenticates and then dies with EACCES writing cert.pem,
	# burning a one-time callback token.
	volume_sh "chown -R 65532:65532 /etc/cloudflared"
}

case "${1:-}" in
login)
	ensure_volume
	# Mounted at the image's HOME rather than /etc/cloudflared, because login
	# ignores --origincert and writes to $HOME/.cloudflared unconditionally.
	# Mount it elsewhere and it reports success while writing the cert into the
	# container layer, taking the one-time callback token with it.
	#
	# No TTY either, since login only prints a URL and polls the callback.
	docker run --rm -v "$VOLUME:/home/nonroot/.cloudflared" "$IMAGE" tunnel login
	volume_sh "chown -R 65532:65532 /etc/cloudflared"
	;;

up)
	ensure_volume
	cfd tunnel create "$TUNNEL" || true

	# The credentials file is named after the tunnel id, so the id reads straight
	# off the volume instead of out of `tunnel list`.
	ID=$(volume_sh "ls /etc/cloudflared" | grep -E '^[0-9a-f-]{36}\.json$' | head -1 | sed 's/\.json$//')
	if [ -z "$ID" ]; then
		echo "could not determine tunnel id, run: make tunnel-status" >&2
		exit 1
	fi
	echo "tunnel id: $ID"

	# ONE ZONE ONLY. cert.pem carries a single zoneID picked in the browser
	# during `login`. Handed a hostname from another zone this does not fail, it
	# treats the whole string as a subdomain and creates
	# "blog.bythewood.me.isaacbythewood.com". Routing a second zone means logging
	# in again to replace cert.pem, which is safe for a running tunnel since that
	# authenticates with the credentials JSON instead.
	#
	# ONE LABEL ONLY. Cloudflare's free Universal SSL signs the apex and a single
	# wildcard level, so a two-label host like next.blog.bythewood.me has no
	# certificate and fails the TLS handshake. Use a hyphen instead.
	for host in $HOSTNAMES; do
		cfd tunnel route dns "$TUNNEL" "$host" || true
	done

	# Seeded over stdin rather than mounted, for the same reason as everything
	# else here. The id appears twice in the template so the substitution is
	# global, and it renders to a temp file first because a sed failure in the
	# middle of a pipeline does not fail the pipeline.
	rendered=$(mktemp)
	trap 'rm -f "$rendered"' EXIT
	sed "s/CHANGEME_TUNNEL_ID/$ID/g" cloudflared/config.yml > "$rendered"
	grep -q CHANGEME_TUNNEL_ID "$rendered" && {
		echo "tunnel id was not substituted into config.yml" >&2
		exit 1
	}
	docker run --rm -i -v "$VOLUME:/etc/cloudflared" alpine:3 \
		sh -c 'cat > /etc/cloudflared/config.yml && chown 65532:65532 /etc/cloudflared/config.yml' \
		< "$rendered"

	echo "config.yml written. now, from the repo root: make edge"
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
	# `tunnel route dns` creates records and cloudflared has no delete
	# counterpart, so the CNAME outlives the tunnel and has to go from the
	# dashboard. Until it does the hostname answers 530, error 1033.
	echo
	echo "NOTE: the CNAME for $HOSTNAMES still exists and must be deleted"
	echo "in the Cloudflare dashboard. Until then it returns 530."
	;;

*)
	awk 'NR>1 && /^#/ { sub(/^#[ ]?/, ""); print; next } NR>1 { exit }' "$SELF"
	exit 1
	;;
esac
