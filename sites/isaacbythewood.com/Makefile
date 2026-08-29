.PHONY: run build frontend serve images resume og clean

# Geist ships .ttf inside the bun package, the only form Typst reads;
# `make frontend` is what puts it on disk.
GEIST = frontend/node_modules/geist/dist/fonts/geist-sans

RESUME_DIR = resume
RESUME_OUT = frontend/public/pdfs/resume-isaac-bythewood.pdf

# Vite watches and rebuilds build/dist while Go serves it from disk. Templates
# are embedded, so a template change needs a Go restart; CSS and JS changes only
# need a browser reload. A dev build never embeds the bundle, which is the
# whole reason that reload works.
run:
	cd frontend && bun run dev & \
	sleep 2 && go run .

frontend:
	cd frontend && bun install --frozen-lockfile && bun run build

# The resume PDF, compiled out of resume/ at the repo root into publicDir so
# Vite copies it into dist alongside the favicons. Skipped with a warning
# rather than a failure when typst is absent, which matches how the blog treats
# its post PDFs: a dev checkout without typst serves a 404 for that one file
# and everything else works.
resume:
	@if command -v typst >/dev/null 2>&1; then \
	  mkdir -p $(dir $(RESUME_OUT)); \
	  typst compile --root $(RESUME_DIR) $(RESUME_DIR)/resume.typ $(RESUME_OUT); \
	  echo "resume -> $(RESUME_OUT)"; \
	else \
	  echo "typst not installed, skipping the resume PDF"; \
	fi

# The social card, compiled into publicDir so Vite copies it into dist beside
# the favicons and the resume. Its own target for when only the card changed.
og:
	mkdir -p frontend/public/og
	typst compile --format png --ppi 72 --font-path $(GEIST) typst/card.typ frontend/public/og/card.png

# The release binary. -tags embed swaps assets_disk.go for assets_embed.go, so
# the Vite bundle is compiled into the executable and ../../bin/<site> is the
# whole deployable: one file, nothing beside it, no SITE_DIST to set. It
# depends on `frontend` because the embed directive reads build/dist at compile
# time, so the order is not a convenience here, it is a requirement.
build: resume og frontend
	go build -tags embed -trimpath -ldflags="-s -w" -o ../../bin/isaacbythewood.com .

serve:
	go run .

# Regenerate the responsive image ladder from frontend/images/. Part of
# `build`, so it cannot be forgotten; here as its own target for when only the
# images changed.
images:
	cd frontend && bun run images

# build/ is everything this site generates; frontend/public/og and
# frontend/public/pdfs are the exception, and stay where they are because Vite
# copies publicDir into the bundle and will only look inside frontend/.
clean:
	rm -rf build frontend/node_modules frontend/public/og frontend/public/pdfs
