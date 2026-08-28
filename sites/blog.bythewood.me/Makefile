.PHONY: run build frontend pdfs og serve clean

# Geist ships as .ttf inside the bun package, which is the only form Typst can
# read; `make frontend` is what puts it there.
GEIST = frontend/node_modules/geist/dist/fonts/geist-sans

# Vite watches and rebuilds dist/ while Go serves it from disk. Templates are
# embedded, so a template change needs a Go restart; CSS and JS changes only
# need a browser reload.
#
# Go waits for the manifest rather than sleeping a guessed number of seconds.
# The server treats a missing manifest as fatal, on purpose: serving a page
# whose script tag points at a file that was never built is worse than
# refusing to start. That makes the startup order load bearing, and this
# site's first Vite build takes about six seconds because of the Bootstrap
# SCSS, so any fixed sleep short enough to be pleasant is also short enough to
# lose the race on a cold dist/.
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

# Compile every post to a PDF. Part of `build`, so it cannot be forgotten, and
# its own target for when only a post changed.
#
# Needs the typst CLI on PATH. The Docker build fetches a pinned release; a
# local checkout without it just serves 404 on the PDF route, which is the
# right thing for a dev server.
pdfs:
	go run . -pdfs ./pdfs -typst-root . -typst-fonts $(GEIST)

# The social cards, one PNG per post plus the site card. Same compiler, same
# font and the same build time reasoning as the PDFs.
og:
	go run . -og ./og -typst-root . -typst-fonts $(GEIST)

build: frontend pdfs og
	go build -trimpath -ldflags="-s -w" -o ../../bin/blog.bythewood.me .

serve:
	go run .

clean:
	rm -rf dist pdfs og frontend/node_modules
