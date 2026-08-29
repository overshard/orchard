.PHONY: run build frontend lighthouse serve preview clean

# Vite watches and rebuilds build/dist while Go serves it from disk. Templates are
# embedded, so a template change needs a Go restart; CSS and JS changes only
# need a browser reload.
#
# Go waits for the manifest rather than sleeping a guessed number of seconds.
# The server treats a missing manifest as fatal on purpose, which makes the
# startup order load bearing, and this site's first Vite build is slow:
# Bootstrap SCSS plus Chart.js and the Monaspace font.
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

# The lighthouse CLI, installed at the site root rather than in frontend/,
# because it is not a frontend dependency: it is a program the Go binary
# execs. lighthouse.go looks for it at ./node_modules/.bin/lighthouse.
#
# Its own target and not part of `build`: it is a large install that changes
# rarely, and a local checkout that skips it still runs, it just records
# "lighthouse CLI not installed" against each property.
lighthouse:
	bun install --frozen-lockfile

# The release binary. -tags embed swaps assets_disk.go for assets_embed.go so
# the Vite bundle is compiled into the executable. `frontend` is not just
# ordering here, it is a requirement: the embed directive reads build/dist at
# compile time and fails the build if it is not there.
build: frontend
	go build -tags embed -trimpath -ldflags="-s -w" -o ../../bin/status.bythewood.me .

serve:
	go run .

# Render an ntfy notification to stdout without waiting for something to
# break. Replaces the Rust version's `status preview-email`.
preview:
	go run . -preview-alert down
	@echo
	go run . -preview-alert recovery

clean:
	rm -rf build frontend/node_modules node_modules data
