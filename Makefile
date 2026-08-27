# Dispatch to a site, so spinning one up does not mean remembering where its
# Makefile lives:
#
#   make run SITE=isaacbythewood.com
#   make build SITE=isaacbythewood.com
#   make deploy SITE=isaacbythewood.com
#
# And for the whole repo:
#
#   make check      vet and build every package
#   make edge-up    bring up the shared tunnel and Caddy

SITE ?= isaacbythewood.com
SITE_DIR = sites/$(SITE)

.PHONY: run build deploy check fmt test resume edge-up edge-down edge-status sites

run:
	$(MAKE) -C $(SITE_DIR) run

build:
	$(MAKE) -C $(SITE_DIR) build

deploy:
	cd $(SITE_DIR) && docker compose up --build --detach

check: fmt
	go vet ./...
	go build ./...

fmt:
	gofmt -l -w .

test:
	go test ./...

edge-up:
	cd edge && docker compose up --build --detach

edge-down:
	cd edge && docker compose down

edge-status:
	cd edge && sh setup-tunnel.sh status

sites:
	@ls -1 sites/

# The resume PDF, compiled from resume/ into the site's publicDir. Its own
# target because the resume is a document of its own, not part of any site's
# frontend; `make build SITE=isaacbythewood.com` runs it too.
resume:
	$(MAKE) -C sites/isaacbythewood.com resume
