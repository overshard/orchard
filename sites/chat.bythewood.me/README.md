# chat.bythewood.me

A conversation with Claude Haiku 5.5, reached through the gateway at
llm.bythewood.me, with tools for what it can't know (the web, news, weather,
markets, the other orchard sites and house lookups out of public records),
attachments, history in SQLite and an incognito mode that writes nothing down.
It's behind auth.bythewood.me.

## Requirements

Go, and poppler's `pdftotext` to read encrypted PDFs. The `.env` carries
`LLM_KEY`, which `make chat-key` at the root mints and compose won't start
without, and `FBI_API_KEY` from api.data.gov, without which house reports have
no crime figures. `data/property.json` is the personal side of the house
lookups, gitignored and shaped like `property.example.json`.

## Running it

```sh
make run      go run, on :8000
make build    release binary into ../../bin/chat.bythewood.me
make clean    delete that binary
```

`PROMPT_SMOKE=1 LLM_KEY=... go test -run TestPromptsSmoke .` drives two real
turns through the gateway, which costs a few calls.

## Layout

`chat.go` is the turn, `llm.go` the gateway client, `tools/` every tool the
model can call, and `internal/` the engines behind them, `property/` the house
lookups (with the detail in `CLAUDE.md`), `history/` and `memory/` what gets
kept, and `pictures/` the pictures attached to a conversation. `attach.go`
reads the files in, and `templates/` and `static/` are written by hand, with no
Vite.

## Things that will bite you

- Every round of a turn resends the whole history, and a prompt past 100K
  tokens bills at five times the rate, so `compact.go` summarises early and
  `chat.go` caps what tool results can add in one turn.
- The model can't see an attached picture, since the gateway passes text only.
  It's kept and shown on the message and the model is told its name and size.
- A dev build reads templates and static files off disk and can check sessions
  against `-verify <url>` instead of auth. The shipped build can't do either.
- Without poppler the pure Go reader takes over, and it can't open the
  encrypted PDFs most bank statements are.
