# orchard
#
# Three commands cover the running system:
#
#   make up                              bring everything up, from nothing or from broken
#   make deploy SITE=blog.bythewood.me   rebuild one site and replace it
#   make doctor                          what is running, what is not, what to type
#
# And four for development, none of which touch Docker at all:
#
#   make run SITE=blog.bythewood.me      vite watch + go run, on :8000
#   make build SITE=blog.bythewood.me    release binary into bin/
#   make check                           gofmt, then vet and build every site
#   make test                            every site's tests
#
# Every docker command goes through sudo, because the socket in the webdev
# container is root:root mode 660 and being in the docker group does not help.
# On a host where docker needs no sudo, turn it off:  make up SUDO=
#
# Each site is its own Go module and there is no module at this level, which is
# why every loop below runs its command inside the site directory rather than
# reaching across one module from here. go.work exists so these repo-wide
# targets and an editor can see all four at once; nothing depends on it.

SUDO   ?= sudo
DOCKER ?= $(SUDO) docker

SITES    = $(notdir $(wildcard sites/*))
SITE_DIR = sites/$(SITE)

# orchard-blog, orchard-analytics, and so on. Every compose file names its
# container after the site's first label, prefixed to match the edge's
# orchard-caddy and orchard-cloudflared, so the mapping needs no table and
# `docker ps --filter name=orchard` shows the whole system.
CONTAINER = orchard-$(firstword $(subst ., ,$(SITE)))

# sudo runs with env_reset, so a password exported in the deploying shell is
# stripped before compose ever sees it, and the ${VAR:?} guard in the compose
# file then aborts the deploy complaining about the shell you just set it in.
# These two are forwarded as a sudo-level assignment instead. Make echoes a
# recipe before the shell expands it, so the value itself is never printed.
PASSWORD_analytics.bythewood.me = ANALYTICS_PASSWORD
PASSWORD_status.bythewood.me    = STATUS_PASSWORD
PASSWORD = $(PASSWORD_$(SITE))

COMPOSE = $(SUDO) $(if $(PASSWORD),$(PASSWORD)="$$$(PASSWORD)") docker compose

# Compose interpolates ${VAR:?} on every subcommand, `down` included, so
# stopping a site needs a value even though nothing will ever read it.
COMPOSE_DOWN = $(SUDO) $(if $(PASSWORD),$(PASSWORD)=unused) docker compose

.DEFAULT_GOAL := help
.PHONY: help up up-one deploy doctor down down-one run build check fmt vet test \
	require-site require-password require-tunnel

help:
	@echo "running system"
	@echo "  make up                    bring everything up; safe to re-run, and the repair command"
	@echo "  make deploy SITE=<site>    rebuild one site and replace it"
	@echo "  make doctor                what is running, what is broken, what to type"
	@echo "  make down                  stop everything"
	@echo ""
	@echo "development, no docker involved"
	@echo "  make run SITE=<site>       vite watch + go run, on :8000"
	@echo "  make build SITE=<site>     release binary into bin/"
	@echo "  make check                 gofmt, then vet and build every site"
	@echo "  make test                  every site's tests"
	@echo ""
	@echo "once per machine, before the first up"
	@echo "  sh edge/setup-tunnel.sh login"
	@echo "  sh edge/setup-tunnel.sh up"
	@echo ""
	@echo "sites"
	@for s in $(SITES); do echo "  $$s"; done

# ---------------------------------------------------------------- the system

# Idempotent, and the repair command as much as the first-run command: compose
# starts whatever is missing and leaves whatever is already correct alone. It
# deliberately does not pass --build, so an image that exists is reused. Use
# `deploy` when the code changed.
up: require-tunnel
	cd edge && $(DOCKER) compose up --detach
	for s in $(SITES); do \
		$(MAKE) --no-print-directory up-one SITE=$$s || exit 1; \
	done
	@echo ""
	$(MAKE) --no-print-directory doctor

up-one: require-site require-password
	cd $(SITE_DIR) && $(COMPOSE) up --detach

deploy: require-site require-password
	cd $(SITE_DIR) && $(COMPOSE) up --build --detach
	@echo ""
	@echo "$(SITE) rebuilt and replaced as $(CONTAINER)"

down:
	for s in $(SITES); do \
		$(MAKE) --no-print-directory down-one SITE=$$s || exit 1; \
	done
	cd edge && $(DOCKER) compose down

down-one: require-site
	cd $(SITE_DIR) && $(COMPOSE_DOWN) down

# Read only. Every line is either fine or carries the command that fixes it.
# `ps -a` rather than `ps`, so a container that exited reads as stopped rather
# than vanishing from the report entirely.
#
# The state section covers the two SQLite volumes, which hold the only thing in
# this repo that cannot be rebuilt from source. A doctor that skipped them would
# call a system healthy while its data was gone. The names are read out of the
# compose files rather than listed here, so a site that gains state gets picked
# up without anyone remembering to edit this.
doctor:
	@probe() { \
		st=$$($(DOCKER) ps -a --filter "name=^$$1$$" --format '{{.Status}}' 2>/dev/null | head -1); \
		case "$$st" in \
		"")          printf '  %-22s %-22s %s\n' "$$1" "not created" "$$2" ;; \
		*unhealthy*) printf '  %-22s %-22s %s\n' "$$1" "unhealthy"   "$$2" ;; \
		Up*)         printf '  %-22s %s\n'       "$$1" "$$st" ;; \
		*)           printf '  %-22s %-22s %s\n' "$$1" "stopped"     "$$2" ;; \
		esac; \
	}; \
	if $(DOCKER) volume inspect orchard-cloudflared >/dev/null 2>&1; then \
		echo "tunnel   credentials present"; \
	else \
		echo "tunnel   NOT SET UP           -> sh edge/setup-tunnel.sh login, then up"; \
	fi; \
	if $(DOCKER) network inspect orchard-edge >/dev/null 2>&1; then \
		echo "network  orchard-edge up"; \
	else \
		echo "network  MISSING              -> make up, the edge stack owns it"; \
	fi; \
	echo ""; \
	echo "edge"; \
	probe orchard-caddy       "-> make up"; \
	probe orchard-cloudflared "-> make up"; \
	echo ""; \
	echo "sites"; \
	for s in $(SITES); do \
		probe "orchard-$$(echo $$s | cut -d. -f1)" "-> make deploy SITE=$$s"; \
	done; \
	echo ""; \
	echo "state"; \
	sizes=$$($(DOCKER) system df -v 2>/dev/null | awk '/^VOLUME NAME/{v=1;next} v && NF==3{print $$1"="$$3}'); \
	for s in $(SITES); do \
		for vol in $$(awk '/^volumes:/{v=1;next} /^[a-z]/{v=0} v && /name:/{print $$2}' sites/$$s/docker-compose.yml); do \
			if $(DOCKER) volume inspect $$vol >/dev/null 2>&1; then \
				size=$$(echo "$$sizes" | sed -n "s/^$$vol=//p"); \
				printf '  %-22s %s\n' "$$vol" "ok, $${size:-size unknown}"; \
			else \
				printf '  %-22s %-22s %s\n' "$$vol" "MISSING" "-> make deploy SITE=$$s"; \
			fi; \
		done; \
	done

# ----------------------------------------------------------------- the guards

# There is no default SITE. There used to be, and a bare `make deploy` then
# meant rebuilding and replacing the portfolio, which is a lot to trigger by
# forgetting an argument.
require-site:
	@test -n "$(SITE)" || { \
		echo "SITE is not set. one of:" >&2; \
		for s in $(SITES); do echo "  make $(firstword $(MAKECMDGOALS)) SITE=$$s" >&2; done; \
		exit 1; \
	}
	@test -d "$(SITE_DIR)" || { \
		echo "there is no site called '$(SITE)'. one of:" >&2; \
		for s in $(SITES); do echo "  $$s" >&2; done; \
		exit 1; \
	}

# Compose would catch this too, but it reports it as a variable the deploying
# shell did not set, which reads like a bug in the compose file rather than a
# missing argument.
require-password:
	@if [ -n "$(PASSWORD)" ] && [ -z "$${$(PASSWORD)}" ]; then \
		echo "$(SITE) will not start without $(PASSWORD). it comes from the" >&2; \
		echo "deploying shell rather than a file, so it is never written next" >&2; \
		echo "to the repo. the value is in 1Password:" >&2; \
		echo "" >&2; \
		echo "  $(PASSWORD)=... make $(firstword $(MAKECMDGOALS)) SITE=$(SITE)" >&2; \
		exit 1; \
	fi

# The tunnel's credentials live in a named volume that setup-tunnel.sh creates.
# Without it compose fails with "external volume not found", which is true and
# does not say what to do about it.
require-tunnel:
	@$(DOCKER) volume inspect orchard-cloudflared >/dev/null 2>&1 || { \
		echo "the tunnel is not set up on this machine yet. once, in order:" >&2; \
		echo "" >&2; \
		echo "  sh edge/setup-tunnel.sh login" >&2; \
		echo "  sh edge/setup-tunnel.sh up" >&2; \
		exit 1; \
	}

# ------------------------------------------------------------------- the code

run: require-site
	$(MAKE) -C $(SITE_DIR) run

build: require-site
	$(MAKE) -C $(SITE_DIR) build

# -o build/ matters. A bare `go build ./...` writes each site's executable into
# the working directory, which is how a 14MB binary once got committed and why
# .gitignore still carries rules for it. Sending it to the gitignored build/
# leaves the source directory as it was found.
check: fmt vet
	for s in $(SITES); do echo "build $$s"; \
		(cd sites/$$s && mkdir -p build && go build -o build/ ./...) || exit 1; done

fmt:
	gofmt -l -w sites

vet:
	for s in $(SITES); do echo "vet $$s"; (cd sites/$$s && go vet ./...) || exit 1; done

test:
	for s in $(SITES); do echo "test $$s"; (cd sites/$$s && go test ./...) || exit 1; done
