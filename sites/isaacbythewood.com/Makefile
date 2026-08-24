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

# Regenerate the responsive image ladder. Only needed when a pour is added or
# replaced, which is why it is not part of `build`: the originals are archival
# masters that do not live in this repo.
#   make images SRC=~/pictures/acrylic-pours
images:
	@test -n "$(SRC)" || (echo "usage: make images SRC=/path/to/originals" && false)
	python3 frontend/scripts/generate-images.py "$(SRC)"

clean:
	rm -rf dist frontend/node_modules
