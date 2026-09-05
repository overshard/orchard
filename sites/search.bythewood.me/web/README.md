# web

Search's own copy of the shared pieces, the same way every other site in orchard
carries one. The renderer, static handler and Vite manifest are still absent,
because search builds its templates itself and serves assets out of `assets.go`.

`server.go` is the copy without a write bound, shared with dash, since both
serve an event stream that has to stay open. The rest are byte for byte the
same as everywhere else.

A fix in one copy has to be made in the others. That cost was accepted on
2026-08-28 when the root module was split.
