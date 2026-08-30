#!/bin/sh
# Create and inspect the two ntfy accounts the alert path runs on.
#
#   sh setup-ntfy.sh up       create both accounts and their topic access
#   sh setup-ntfy.sh token    mint the publishers' token, into their .env files
#   sh setup-ntfy.sh status   what exists right now
#   sh setup-ntfy.sh passwd   change the reading account's password
#
# ntfy is deny-all and there are two accounts: isaac reads status and logging
# from the phone, orchard writes to them from the sites. ntfy cannot restrict
# an account by source address, so Caddy also refuses the publish routes on the
# public hostname.
#
# All ntfy state lives in the orchard-ntfy-data volume, never a bind mount. The
# Docker CLI here talks to Docker Desktop on the Windows host, whose daemon
# cannot see this filesystem, so a bind mount silently resolves to an empty
# directory.
#
# Every docker command goes through sudo, because the socket in the webdev
# container is root:root mode 660 and being in the docker group does not help.
# On a host where docker needs no sudo:  SUDO= sh setup-ntfy.sh up
set -e

# Run from this script's own directory, whatever the caller's was, since the
# token step writes into ../sites/*/.env.
cd "$(dirname "$0")"

SUDO=${SUDO-sudo}
DOCKER_BIN=$(command -v docker) || { echo "no docker on PATH" >&2; exit 1; }
# An absolute path, because `command` is a shell builtin and sudo cannot exec
# one, and because sudo's secure_path is not this shell's PATH.
docker() { ${SUDO} "$DOCKER_BIN" "$@"; }

CONTAINER=orchard-ntfy
READER=isaac
WRITER=orchard
TOPICS="status logging"

# ntfy reads auth-file out of the config baked into the image, so every command
# here runs inside the container rather than against the volume directly.
running() {
	docker ps --filter "name=^${CONTAINER}$" --format '{{.Names}}' 2>/dev/null | grep -q .
}

require_running() {
	running || {
		echo "$CONTAINER is not running. from the repo root:" >&2
		echo "" >&2
		echo "  make up" >&2
		exit 1
	}
}

ntfy() { docker exec -i "$CONTAINER" ntfy "$@"; }

case "${1:-}" in
up)
	require_running
	ntfy user add --ignore-exists "$READER"
	ntfy user add --ignore-exists "$WRITER"

	for topic in $TOPICS; do
		ntfy access "$READER" "$topic" read-only
		ntfy access "$WRITER" "$topic" write-only
	done

	echo
	echo "accounts created. now mint the publishers' token:"
	echo
	echo "  sh edge/setup-ntfy.sh token"
	;;

token)
	require_running
	# Written straight into the .env files rather than printed, so the token
	# never gets pasted between terminals and into a shell history.
	token=$(ntfy token add -l "orchard site publishers" "$WRITER" \
		| grep -o 'tk_[A-Za-z0-9]*' | head -1)
	if [ -z "$token" ]; then
		echo "no token came back; is $CONTAINER healthy?" >&2
		exit 1
	fi

	for site in ../sites/status.bythewood.me ../sites/logging.bythewood.me; do
		if [ ! -f "$site/.env" ]; then
			echo "no $site/.env yet; create it from .env.example first" >&2
			exit 1
		fi
		# Passed to awk as a variable, so the token never appears on a
		# command line where `ps` could read it.
		tmp="$site/.env.tmp"
		awk -v tok="$token" \
			'/^NTFY_TOKEN=/ { print "NTFY_TOKEN=" tok; found=1; next } { print }
			 END { if (!found) print "NTFY_TOKEN=" tok }' \
			"$site/.env" > "$tmp"
		chmod 600 "$tmp"
		mv "$tmp" "$site/.env"
		echo "wrote NTFY_TOKEN into $(basename "$site")/.env"
	done

	echo
	echo "now, from the repo root, to hand it to the running sites:"
	echo
	echo "  make up"
	;;

status)
	require_running
	echo "--- access ---"
	ntfy access
	echo "--- tokens ---"
	ntfy token list
	;;

passwd)
	require_running
	ntfy user change-pass "$READER"
	echo "changed. update the password in the ntfy app on the phone."
	;;

*)
	sed -n '2,8p' "$0"
	exit 1
	;;
esac
