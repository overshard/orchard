.PHONY: run build frontend serve images clean

# Vite watches and rebuilds dist/ while Go serves it from disk. Templates are
# embedded, so a template change needs a Go restart; CSS and JS changes only
# need a browser reload.
run:
	cd frontend && bun run dev & \
	sleep 2 && go run .

frontend:
	cd frontend && bun install --frozen-lockfile && bun run build

build: frontend
	go build -trimpath -ldflags="-s -w" -o ../../bin/isaacbythewood.com .

serve:
	go run .

# Regenerate the responsive image ladder from frontend/images/. Part of
# `build`, so it cannot be forgotten; here as its own target for when only the
# images changed.
images:
	cd frontend && bun run images

clean:
	rm -rf dist frontend/node_modules
