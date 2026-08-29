# Dispatch to a site, so spinning one up does not mean remembering where its
# Makefile lives:
#
#   make run SITE=isaacbythewood.com
#   make build SITE=isaacbythewood.com
#   make deploy SITE=isaacbythewood.com
#
# And for the whole repo:
#
#   make check      fmt, vet and build every site
#   make test       every site's tests
#   make edge-up    bring up the shared tunnel and Caddy
#
# Each site is its own Go module and there is no module at this level, which is
# why every loop below runs its command inside the site directory rather than
# reaching across one module from here. go.work exists so these repo-wide
# targets and an editor can see all four at once; nothing depends on it.

SITE ?= isaacbythewood.com
SITE_DIR = sites/$(SITE)
SITES = $(notdir $(wildcard sites/*))

.PHONY: run build deploy check fmt vet test edge-up edge-down edge-status sites

run:
	$(MAKE) -C $(SITE_DIR) run

build:
	$(MAKE) -C $(SITE_DIR) build

deploy:
	cd $(SITE_DIR) && docker compose up --build --detach

# -o build/ matters. A bare `go build ./...` writes each site's executable into
# the working directory, which is how a 14MB binary once got committed and why
# .gitignore still carries rules for it. Sending it to the gitignored build/
# leaves the source directory as it was found.
check: fmt vet
	@for s in $(SITES); do echo "build $$s"; 		(cd sites/$$s && mkdir -p build && go build -o build/ ./...) || exit 1; done

fmt:
	gofmt -l -w sites

vet:
	@for s in $(SITES); do echo "vet $$s"; (cd sites/$$s && go vet ./...) || exit 1; done

test:
	@for s in $(SITES); do echo "test $$s"; (cd sites/$$s && go test ./...) || exit 1; done

edge-up:
	cd edge && docker compose up --build --detach

edge-down:
	cd edge && docker compose down

edge-status:
	cd edge && sh setup-tunnel.sh status

sites:
	@ls -1 sites/
