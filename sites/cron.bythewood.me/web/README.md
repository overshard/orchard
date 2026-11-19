# web

cron's own copy of the shared pieces, the same way every other site in orchard
carries one. The renderer, static handler and Vite manifest are absent, because
cron parses its templates itself and serves assets out of `assets_embed.go` and
`assets_disk.go`, the way llm does.

`server.go` is the common copy. The rest are byte for byte the same as everywhere
else, and a fix in one copy has to be made in the others.
