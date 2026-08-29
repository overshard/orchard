.PHONY: run build frontend lighthouse preview clean

# Vite watches and rebuilds build/dist while Go serves it from disk. Templates are
# embedded, so a template change needs a Go restart; CSS and JS changes only
# need a browser reload.
#
# Go waits for the manifest rather than sleeping a guessed number of seconds.
# The server treats a missing manifest as fatal, so the startup order matters,
# and this site's first Vite build is slow: Bootstrap SCSS plus Chart.js and
# the Monaspace font.
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
# because it is a program the Go binary execs rather than a frontend dependency.
# lighthouse.go looks for it at ./node_modules/.bin/lighthouse.
#
# Its own target rather than part of `build`: it is a large install that changes
# rarely, and a local checkout that skips it still runs, recording "lighthouse
# CLI not installed" against each property.
lighthouse:
	bun install --frozen-lockfile

# The release binary. -tags embed swaps assets_disk.go for assets_embed.go, so
# the Vite bundle is compiled into the executable. `frontend` is a requirement
# rather than ordering: the embed directive reads build/dist at compile time and
# fails if it is not there.
build: frontend
	go build -tags embed -trimpath -ldflags="-s -w" -o ../../bin/status.bythewood.me .

# Render an ntfy notification to stdout without waiting for something to
# break.
preview:
	go run . -preview-alert down
	@echo
	go run . -preview-alert recovery

clean:
	rm -rf build frontend/node_modules node_modules data
