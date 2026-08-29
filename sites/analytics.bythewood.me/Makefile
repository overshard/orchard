.PHONY: run build frontend maps seed clean

# Vite watches and rebuilds build/dist while Go serves it from disk. Templates are
# embedded, so a template change needs a Go restart; CSS and JS changes only
# need a browser reload.
#
# Go waits for the manifest rather than sleeping a guessed number of seconds.
# The server treats a missing manifest as fatal, so the startup order matters,
# and this site's first Vite build is the slowest in the repo: Bootstrap SCSS
# plus Chart.js, d3 and topojson.
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

# Per-country admin-1 topojson from Natural Earth, about 10MB of output. Its own
# target rather than part of `build`, because the input is a fixed dataset and
# regenerating it every time would add a minute for nothing. The Dockerfile runs
# it in its own layer.
maps:
	cd frontend && bun run build:maps

# The release binary. -tags embed swaps assets_disk.go for assets_embed.go, so
# the Vite bundle and the topojson are compiled into the executable. Both
# prerequisites are requirements rather than ordering: the embed directives read
# build/ at compile time and fail if it is not there.
build: frontend build/static_maps
	go build -tags embed -trimpath -ldflags="-s -w" -o ../../bin/analytics.bythewood.me .

# A directory target, so this runs once and then never again. A release build
# cannot proceed without the maps, but the input is a fixed dataset, so
# existence is the right check rather than a rebuild.
build/static_maps:
	$(MAKE) maps

# Realistic fake traffic in a "Seed Test" property, so the dashboard can be
# looked at without waiting for a real site to accumulate months of events.
# Override with `make seed SESSIONS=2000 DAYS=90`.
SESSIONS ?= 500
DAYS ?= 60
seed:
	go run . -seed -seed-sessions $(SESSIONS) -seed-days $(DAYS)

clean:
	rm -rf build frontend/node_modules
