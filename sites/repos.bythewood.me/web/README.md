This package is repos.bythewood.me's own copy of the small HTTP layer every
site here needs: the Vite manifest reader, request logging, panic recovery,
security headers, the static cache policy, graceful shutdown, and the log
shipper.

Every site carries its own copy instead of importing a shared module, so a site
can be lifted out into its own repository and its Docker build context is just
its own directory. The cost is that a fix here has to be made six times, so if a
change belongs in all six then change all six.

`shipper.go` is the only file that talks to another site. It installs a
`slog.Handler` that tees, so stdout is written exactly as before and a copy is
queued for logging.bythewood.me and flushed by a background goroutine. Nothing
in it can block a caller and every failure path drops rather than waits, because
stdout stays the source of truth.
