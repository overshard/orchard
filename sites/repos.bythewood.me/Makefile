.PHONY: run build frontend clean

# Vite watches and rebuilds build/dist while Go serves it from disk. Templates
# are embedded, so a template change needs a Go restart; CSS and JS changes only
# need a browser reload.
#
# Go waits for the manifest rather than sleeping a guessed number of seconds:
# the server treats a missing manifest as fatal, so the startup order matters.
#
# REPOS_MIRROR=0 by default here. A dev run that clones the whole GitHub account
# before it will serve a page is not a dev run.
run:
	cd frontend && bun run dev & \
	i=0; while [ ! -f build/dist/.vite/manifest.json ]; do \
		i=$$((i+1)); \
		if [ $$i -gt 120 ]; then echo "vite did not produce a manifest in 60s" >&2; exit 1; fi; \
		sleep 0.5; \
	done; \
	REPOS_MIRROR=$${REPOS_MIRROR:-0} REPOS_PASSWORD=$${REPOS_PASSWORD:-dev} go run .

frontend:
	cd frontend && bun install --frozen-lockfile && bun run build

# The release binary. -tags embed swaps assets_disk.go for assets_embed.go, so
# the Vite bundle is compiled in and ../../bin/repos.bythewood.me is the whole
# server. The repositories it serves stay outside it, on a volume.
build: frontend
	go build -tags embed -trimpath -ldflags="-s -w" -o ../../bin/repos.bythewood.me .

# build/repos holds real git repositories in dev. Removing it is removing data,
# so clean leaves it alone and says so.
clean:
	rm -rf build/dist frontend/node_modules
	@echo "left build/repos and build/data alone; they hold real repositories"
