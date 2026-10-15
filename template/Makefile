.PHONY: run build frontend clean

# Vite watches and rebuilds build/dist while Go serves it from disk. Templates
# are embedded, so a template change needs a Go restart, and CSS and JS changes
# only need a browser reload.
#
# Go waits for the manifest rather than sleeping, since the server treats a
# missing manifest as fatal and a cold build takes longer than any guess.
run:
	cd frontend && bun install --frozen-lockfile && bun run dev & \
	i=0; while [ ! -f build/dist/.vite/manifest.json ]; do \
		i=$$((i+1)); \
		if [ $$i -gt 120 ]; then echo "vite did not produce a manifest in 60s" >&2; exit 1; fi; \
		sleep 0.5; \
	done; \
	go run .

frontend:
	cd frontend && bun install --frozen-lockfile && bun run build

# -tags embed swaps assets_disk.go for assets_embed.go, so the Vite bundle is
# compiled in and the embed directive needs build/dist there at compile time.
build: frontend
	go build -tags embed -trimpath -ldflags="-s -w" -o ../../bin/__SITE__ .

clean:
	rm -rf build frontend/node_modules
