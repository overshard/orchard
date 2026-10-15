# chat.bythewood.me

A conversation with the local model behind llm.bythewood.me, with tools for
what it can't know (the web, news, weather, markets, an offline Wikipedia, the
other orchard sites and house lookups out of public records), attachments,
pictures, history in SQLite and an incognito mode that writes nothing down.
It's behind auth.bythewood.me.

## Requirements

Go, and poppler's `pdftotext` to read encrypted PDFs. The `.env` carries
`LLM_KEY`, which `make chat-key` at the root mints and compose won't start
without, and `FBI_API_KEY` from api.data.gov, without which house reports have
no crime figures. `data/property.json` is the personal side of the house
lookups, gitignored and shaped like `property.example.json`, and `make wiki`
downloads the 12.5GB snapshot `orchard-wiki` serves.

## Running it

```sh
make run      go run, on :8000
make build    release binary into ../../bin/chat.bythewood.me
make clean    delete that binary
```

## Layout

`chat.go` is the turn, `tools/` every tool the model can call, `property/` the
house lookups (with the detail in `CLAUDE.md`), `history.go` and `memory.go`
what gets kept, and `attach.go` and `picture.go` files in and pictures out.
`templates/` and `static/` are written by hand, with no Vite.

## Things that will bite you

- One turn runs at a time, since there's one card behind the gateway, so a
  second tab waits in `queue.go`.
- A dev build reads templates and static files off disk and can check sessions
  against `-verify <url>` instead of auth. The shipped build can't do either.
- Without poppler the pure Go reader takes over, and it can't open the
  encrypted PDFs most bank statements are.
