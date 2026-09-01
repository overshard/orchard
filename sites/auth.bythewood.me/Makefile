.PHONY: run build frontend clean

# Vite watches and rebuilds build/dist while Go serves it from disk. Templates
# are embedded, so a template change needs a Go restart; CSS and JS changes only
# need a browser reload.
#
# Go waits for the manifest rather than sleeping a guessed number of seconds.
# The server treats a missing manifest as fatal, so the startup order matters
# and a cold build takes longer than any number worth hardcoding.
#
# SESSION_DOMAIN is left unset here so the cookie is host-only and development
# on localhost works. Setting it to .bythewood.me locally means a login that
# reports success and then does not stick.
run:
	cd frontend && bun run dev & \
	i=0; while [ ! -f build/dist/.vite/manifest.json ]; do \
		i=$$((i+1)); \
		if [ $$i -gt 120 ]; then echo "vite did not produce a manifest in 60s" >&2; exit 1; fi; \
		sleep 0.5; \
	done; \
	go run .

frontend:
	cd frontend && bun install --frozen-lockfile && bun run build

# The release binary. -tags embed swaps assets_disk.go for assets_embed.go, so
# the Vite bundle is compiled into the executable. The prerequisite is a
# requirement rather than ordering: the embed directive reads build/ at compile
# time and fails if it is not there.
build: frontend
	go build -tags embed -trimpath -ldflags="-s -w" -o ../../bin/auth.bythewood.me .

clean:
	rm -rf build frontend/node_modules
