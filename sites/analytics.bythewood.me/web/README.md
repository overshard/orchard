This package is analytics.bythewood.me's own copy of the small HTTP layer every site here needs:
the Vite manifest reader, request logging, panic recovery, security headers,
the static cache policy and graceful shutdown.

It was a single shared `internal/web` under one module at the repo root before
being split per site, so that a site can be built and lifted into its own
repository without dragging a parent module along, and so its Docker build
context is this directory rather than the whole monorepo.

The cost is that a fix here has to be made four times. If a change belongs in
all four, change all four.
