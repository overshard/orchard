#!/bin/sh
# Create and inspect the two ntfy accounts the alert path runs on. Run these
# from the repo root, where each one is a make target:
#
#   make ntfy          create both accounts and their topic access
#   make ntfy-token    mint the publishers' token, into their .env files
#   make ntfy-status   what exists right now
#   make ntfy-passwd   change the reading account's password
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
# On a host where docker needs no sudo:  make ntfy SUDO=
set -e

# Resolved before the cd below, since $0 is relative to the caller's
# directory and stops resolving the moment we leave it.
SELF="$(cd "$(dirname "$0")" && pwd)/$(basename "$0")"

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

user_exists() {
	ntfy user list 2>/dev/null | grep -q "^user $1 "
}

# 32 characters of /dev/urandom in groups of eight. Long enough that nobody is
# going to type it by hand, grouped so it can be read back off a screen.
gen_password() {
	LC_ALL=C tr -dc 'A-Za-z0-9' < /dev/urandom \
		| head -c 32 \
		| sed 's/.\{8\}/&-/g; s/-$//'
	echo
}

case "${1:-}" in
up)
	require_running

	# These passwords exist nowhere else, so they are generated and printed
	# rather than asked for. An account that already exists keeps the one it
	# has, because ntfy will not hand a password back and printing a new one
	# that was never applied is worse than printing nothing.
	made=""
	for user in "$READER" "$WRITER"; do
		if user_exists "$user"; then
			echo "$user exists already, and its password is untouched"
			continue
		fi
		pw=$(gen_password)
		# Over stdin, not NTFY_PASSWORD. sudo runs with env_reset and strips
		# the variable, and forwarding it through sudo would put the password
		# in the command line for anyone running ps.
		printf '%s\n%s\n' "$pw" "$pw" | ntfy user add "$user" >/dev/null
		made="$made$user $pw
"
	done

	for topic in $TOPICS; do
		ntfy access "$READER" "$topic" read-only
		ntfy access "$WRITER" "$topic" write-only
	done

	if [ -n "$made" ]; then
		echo
		echo "created, and these are the only copies:"
		echo
		printf '%s' "$made" | while read -r u p; do
			printf '  %-10s %s\n' "$u" "$p"
		done
		echo
		echo "$READER is the account the phone logs in with. put both in 1Password."
	fi

	echo
	echo "now mint the publishers' token:"
	echo
	echo "  make ntfy-token"
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
	awk 'NR>1 && /^#/ { sub(/^#[ ]?/, ""); print; next } NR>1 { exit }' "$SELF"
	exit 1
	;;
esac
