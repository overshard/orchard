This package is analytics.bythewood.me's own copy of the small HTTP layer every site here
needs: the Vite manifest reader, request logging, panic recovery, security
headers, the static cache policy and graceful shutdown.

It used to be one shared `internal/web` under a single module at the repo
root. It was split per site deliberately: a site is meant to be its own thing,
buildable and liftable to its own repository without dragging a parent module
along, and the Docker build context is this directory rather than the whole
monorepo because of it.

The cost is real and worth stating: a fix here has to be made four times. That
is the trade that was chosen. If a change belongs in all four, change all four.
