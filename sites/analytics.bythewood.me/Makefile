.PHONY: run build frontend maps seed serve clean

# Vite watches and rebuilds dist/ while Go serves it from disk. Templates are
# embedded, so a template change needs a Go restart; CSS and JS changes only
# need a browser reload.
#
# Go waits for the manifest rather than sleeping a guessed number of seconds.
# The server treats a missing manifest as fatal on purpose, which makes the
# startup order load bearing, and this site's first Vite build is the slowest
# in the repo: Bootstrap SCSS plus Chart.js, d3 and topojson.
run:
	cd frontend && bun run dev & \
	i=0; while [ ! -f dist/.vite/manifest.json ]; do \
		i=$$((i+1)); \
		if [ $$i -gt 120 ]; then echo "vite did not produce a manifest in 60s" >&2; exit 1; fi; \
		sleep 0.5; \
	done; \
	go run .

frontend:
	cd frontend && bun install --frozen-lockfile && bun run build

# Per-country admin-1 topojson from Natural Earth, about 10MB of output. Its
# own target and not part of `build`, because the input is a fixed dataset that
# has not changed in years and regenerating it on every build would add a
# minute for nothing. The Dockerfile runs it in its own layer.
maps:
	cd frontend && bun run build:maps

build: frontend
	go build -trimpath -ldflags="-s -w" -o ../../bin/analytics.bythewood.me .

# Realistic fake traffic in a "Seed Test" property, so the dashboard can be
# looked at without waiting for a real site to accumulate months of events.
# Override with `make seed SESSIONS=2000 DAYS=90`.
SESSIONS ?= 500
DAYS ?= 60
seed:
	go run . -seed -seed-sessions $(SESSIONS) -seed-days $(DAYS)

serve:
	go run .

clean:
	rm -rf dist static_maps frontend/node_modules
