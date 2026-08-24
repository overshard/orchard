.PHONY: run build frontend serve clean

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

clean:
	rm -rf dist frontend/node_modules
