.PHONY: run build frontend clean refresh

# Compose hands the container its .env and nothing does that for a dev run, so the
# FBI key was silently missing and crime quietly reported nothing.
define dotenv
set -a; [ -f .env ] && . ./.env; set +a;
endef

# Vite watches and rebuilds build/dist while Go serves it from disk. Templates are
# embedded, so a template change needs a Go restart and CSS or JS only needs a
# browser reload.
#
# Go waits for the manifest rather than sleeping a guessed number of seconds: the
# server treats a missing manifest as fatal, so the startup order matters.
run:
	$(dotenv) \
	cd frontend && bun run dev & \
	i=0; while [ ! -f build/dist/.vite/manifest.json ]; do \
		i=$$((i+1)); \
		if [ $$i -gt 120 ]; then echo "vite did not produce a manifest in 60s" >&2; exit 1; fi; \
		sleep 0.5; \
	done; \
	go run .

frontend:
	cd frontend && bun install --frozen-lockfile && bun run build

# One pass of the pipeline, printing what it did. Needs no Vite build, since it
# serves nothing.
refresh:
	$(dotenv) go run . -refresh

build: frontend
	go build -tags embed -trimpath -ldflags="-s -w" -o ../../bin/house.bythewood.me .

clean:
	rm -rf build frontend/node_modules
