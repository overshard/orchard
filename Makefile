# orchard
#
# Three commands cover the running system:
#
#   make up                              bring everything up, from nothing or from broken
#   make deploy SITE=blog.bythewood.me   rebuild one site and replace it
#   make edge                            rebuild and replace the edge itself
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

EDGE_COMPOSE = cd edge && $(DOCKER) compose

# Secrets come from a .env file in the site's own directory, which compose reads
# by itself because that directory is the project directory. Nothing is
# forwarded through this Makefile and nothing is read from the deploying shell.
#
# That is a reversal of how this repo used to work, and the reason is worth
# stating. Passwords were taken from the deploying shell so they were never
# written next to the repo, and the cost was paid on every deploy: sudo runs
# with env_reset, so an exported password was stripped before compose ever saw
# it, and the ${VAR:?} guard then aborted the deploy complaining about the shell
# you had just set it in. The value had to be forwarded as a sudo-level
# assignment to work around that, and forgetting was the most common way a
# deploy failed here.
#
# .env is in .gitignore by bare name, so it is ignored at any depth. This is a
# public repository: verify with `git check-ignore -v sites/<name>/.env` rather
# than trusting it. Each site that needs one commits a .env.example listing the
# keys with no values.
COMPOSE      = $(DOCKER) compose
COMPOSE_DOWN = $(DOCKER) compose

.DEFAULT_GOAL := help
.PHONY: help up up-one deploy edge doctor down down-one run build check fmt vet test \
	require-site require-env require-tunnel

help:
	@echo "running system"
	@echo "  make up                    bring everything up; safe to re-run, and the repair command"
	@echo "  make deploy SITE=<site>    rebuild one site and replace it"
	@echo "  make edge                  rebuild and replace caddy, ntfy and the tunnel"
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
	$(EDGE_COMPOSE) up --detach
	for s in $(SITES); do \
		$(MAKE) --no-print-directory up-one SITE=$$s || exit 1; \
	done
	@echo ""
	$(MAKE) --no-print-directory doctor

up-one: require-site require-env
	cd $(SITE_DIR) && $(COMPOSE) up --detach

deploy: require-site require-env
	cd $(SITE_DIR) && $(COMPOSE) up --build --detach
	@echo ""
	@echo "$(SITE) rebuilt and replaced as $(CONTAINER)"

# The edge equivalent of deploy, and it exists because every config file in
# edge/ is baked into an image rather than mounted: the Caddyfile, ntfy's
# server.yml both need a rebuild to take effect, and `make up` deliberately does
# not pass --build. Editing one of them and running
# `make up` is the quiet failure this target prevents.
#
# cloudflared's config is the exception and is seeded into a volume instead;
# changing it means `sh edge/setup-tunnel.sh up`.
# cloudflared is restarted explicitly rather than left to compose. Its config
# comes from a volume rather than from its image, so compose sees nothing
# changed and leaves it running on the old ingress: after `setup-tunnel.sh up`
# adds a hostname, the tunnel keeps serving the previous rule set with no sign
# anything is stale, and the new hostname 404s from a Cloudflare edge that has
# nowhere to send it. That is the difference between "rebuilt" and "reloaded".
edge: require-tunnel
	$(EDGE_COMPOSE) up --build --detach
	$(DOCKER) restart orchard-cloudflared
	@echo ""
	@echo "edge rebuilt: caddy, ntfy, cloudflared"

down:
	for s in $(SITES); do \
		$(MAKE) --no-print-directory down-one SITE=$$s || exit 1; \
	done
	$(EDGE_COMPOSE) down

down-one: require-site
	cd $(SITE_DIR) && $(COMPOSE_DOWN) down

# Read only. Every line is either fine or carries the command that fixes it.
# That is why the alerts section reads ntfy's health endpoint rather than
# publishing a test message: a doctor that sends a notification every time it
# runs is a doctor nobody runs. To prove the path end to end, on purpose:
#
#   sudo docker run --rm --network container:orchard-ntfy curlimages/curl \
#     -d "test" http://127.0.0.1:8000/status
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
	probe orchard-ntfy        "-> make up"; \
	echo ""; \
	echo "alerts"; \
	if $(DOCKER) ps --filter "name=^orchard-ntfy$$" --format '{{.Names}}' 2>/dev/null | grep -q .; then \
		if $(DOCKER) run --rm --network container:orchard-ntfy curlimages/curl:latest \
			-s --max-time 5 http://127.0.0.1:8000/v1/health 2>/dev/null | grep -q '"healthy":true'; then \
			printf '  %-22s answering, topics status and logging\n' "ntfy"; \
		else \
			printf '  %-22s %-22s %s\n' "ntfy" "NOT ANSWERING" "-> docker logs orchard-ntfy"; \
		fi; \
	fi; \
	echo ""; \
	echo "sites"; \
	for s in $(SITES); do \
		probe "orchard-$$(echo $$s | cut -d. -f1)" "-> make deploy SITE=$$s"; \
	done; \
	echo ""; \
	echo "ingest"; \
	if $(DOCKER) ps --filter "name=^orchard-logging$$" --format '{{.Names}}' 2>/dev/null | grep -q .; then \
		$(DOCKER) exec orchard-logging /app -healthcheck >/dev/null 2>&1 && \
		out=$$($(DOCKER) run --rm --network container:orchard-logging curlimages/curl:latest \
			-s --max-time 5 'http://127.0.0.1:8000/healthz?verbose' 2>/dev/null); \
		if [ -n "$$out" ]; then \
			age=$$(echo "$$out" | sed -n 's/.*"newest_record_age_s": *\([0-9]*\).*/\1/p'); \
			failed=$$(echo "$$out" | sed -n 's/.*"failed": *\([0-9]*\).*/\1/p'); \
			queued=$$(echo "$$out" | sed -n 's/.*"queued": *\([0-9]*\).*/\1/p'); \
			if [ "$${failed:-0}" -gt 0 ]; then \
				printf '  %-22s %-22s %s\n' "writes" "$$failed DISCARDED" "-> docker logs orchard-logging"; \
			elif [ -n "$$age" ] && [ "$$age" -gt 900 ]; then \
				printf '  %-22s %-22s %s\n' "freshness" "$${age}s STALE" "-> nothing has shipped in 15 min; check the sites"; \
			else \
				printf '  %-22s newest record %ss old, %s queued, 0 discarded\n' "logging" "$${age:-?}" "$${queued:-?}"; \
			fi; \
		else \
			printf '  %-22s %-22s %s\n' "logging" "unreachable" "-> make deploy SITE=logging.bythewood.me"; \
		fi; \
	else \
		printf '  %-22s %-22s %s\n' "logging" "not created" "-> make deploy SITE=logging.bythewood.me"; \
	fi; \
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

# A site needs a .env exactly when it ships a .env.example, so adding a secret
# to a site is one committed example file and nothing here. Compose would catch
# the missing value too, but it reports it as an unset variable, which reads
# like a bug in the compose file rather than a file you have not created yet.
require-env:
	@if [ -f "$(SITE_DIR)/.env.example" ] && [ ! -f "$(SITE_DIR)/.env" ]; then \
		echo "$(SITE) has no .env. it is gitignored and machine-local, so a" >&2; \
		echo "fresh checkout never has one. copy the example and fill it in:" >&2; \
		echo "" >&2; \
		echo "  cp $(SITE_DIR)/.env.example $(SITE_DIR)/.env" >&2; \
		echo "" >&2; \
		echo "the values are in 1Password." >&2; \
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
