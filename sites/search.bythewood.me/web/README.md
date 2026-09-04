# web

Search's own copy of the shared pieces, the same way every other site in orchard
carries one. Only `session.go` is here so far, because search does not use the
Vite manifest, renderer or static handler the other sites do.

A fix in one copy has to be made in the others. That cost was accepted on
2026-08-28 when the root module was split.
