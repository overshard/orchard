This package is blog.bythewood.me's own copy of the small HTTP layer every site here needs:
the Vite manifest reader, request logging, panic recovery, security headers,
the static cache policy, graceful shutdown, and the log shipper.

It was a single shared `internal/web` under one module at the repo root before
being split per site, so that a site can be built and lifted into its own
repository without dragging a parent module along, and so its Docker build
context is this directory rather than the whole monorepo.

`shipper.go` is the newest piece and the only one that talks to another site.
It installs a `slog.Handler` that tees: stdout is written exactly as before and
a copy is queued for logging.bythewood.me, flushed by a background goroutine
over the internal Docker bridge. Nothing in it can block a caller, and every
failure path drops rather than waits, because stdout remains the source of
truth. See `code/memory/decisions/0015-logging-site-and-per-site-shipping.md`.

The cost of the split is that a fix here has to be made five times. If a change
belongs in all five, change all five.
