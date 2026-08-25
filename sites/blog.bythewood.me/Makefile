.PHONY: run build frontend pdfs serve clean

# Vite watches and rebuilds dist/ while Go serves it from disk. Templates are
# embedded, so a template change needs a Go restart; CSS and JS changes only
# need a browser reload.
run:
	cd frontend && bun run dev & \
	sleep 2 && go run .

frontend:
	cd frontend && bun install --frozen-lockfile && bun run build

# Compile every post to a PDF. Part of `build`, so it cannot be forgotten, and
# its own target for when only a post changed.
#
# Needs the typst CLI on PATH. The Docker build fetches a pinned release; a
# local checkout without it just serves 404 on the PDF route, which is the
# right thing for a dev server.
pdfs:
	go run . -pdfs ./pdfs -typst-root .

build: frontend pdfs
	go build -trimpath -ldflags="-s -w" -o ../../bin/blog.bythewood.me .

serve:
	go run .

clean:
	rm -rf dist pdfs frontend/node_modules
