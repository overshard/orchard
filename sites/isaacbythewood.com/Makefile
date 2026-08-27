.PHONY: run build frontend serve images resume clean

RESUME_DIR = ../../resume
RESUME_OUT = frontend/public/pdfs/resume-isaac-bythewood.pdf

# Vite watches and rebuilds dist/ while Go serves it from disk. Templates are
# embedded, so a template change needs a Go restart; CSS and JS changes only
# need a browser reload.
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

build: resume frontend
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
