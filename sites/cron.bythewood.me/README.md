# cron.bythewood.me

Reminders and scheduled checks, pushed to a phone over the `reminders` ntfy
topic. A job is either a reminder, which sends its message as written, or a
check, which hands its message to the model gateway with web search and pushes
whatever comes back, or nothing if the model says there's nothing to tell. Jobs
run once at a time or on a cron line, and every time is Eastern. The pages are
behind auth.bythewood.me and the API takes a key.

## Requirements

Go, and a `.env` from `.env.example`. `NTFY_TOKEN` comes from `make ntfy-token`
and `LLM_KEY` from `make cron-key`. Without the first every push is refused and
without the second checks fail, and reminders don't need it.

## Running it

```sh
make run      go run, on :8000
make build    release binary into ../../bin/cron.bythewood.me
make clean    delete that binary
```

An API key comes from the Keys page, or `docker exec orchard-cron /app -newkey
<name>` when the page can't be reached.

```sh
curl -H "Authorization: Bearer $CRON_KEY" https://cron.bythewood.me/api/jobs \
  -d '{"name": "Furnace filter", "cron": "0 9 1 */3 *", "message": "Change it"}'
```

`GET /api/jobs`, `POST /api/jobs`, `GET`, `PATCH` and `DELETE /api/jobs/{id}`,
`POST /api/jobs/{id}/run`, `GET /api/runs?job=&limit=` and
`GET /api/preview?cron=` or `?at=`. A job is `name`, `kind` (`notify` or
`prompt`), `at` or `cron`, `message`, and optionally `title`, `priority` (ntfy's
min to urgent) and `link`.

## Layout

`cronexpr.go` parses a cron line and finds its next time on a wall clock,
`scheduler.go` is one goroutine that sleeps until the soonest job, fires it and
writes the run, `model.go` asks llm for a check, `api.go` is the JSON API and
the validation the form shares, `pages.go` the pages, and `db.go` the jobs,
runs, keys and snooze tokens. Templates and `static/` are written by hand, with
no Vite.

## Things that will bite you

- A one-shot is deleted once it lands, and its run row keeps the name. One
  that can't reach ntfy is tried every five minutes, six times, and then
  dropped with an error in the log.
- A repeating job missed by over an hour while the container was down skips to
  its next time instead of firing late.
- Each push carries a snooze button that posts back to
  `https://cron.bythewood.me/snooze/<token>` from the phone. The token is the
  only credential, so it's single use and gone in a week.
- A time sent with no offset is read as Eastern, and a cron line is matched on
  the Eastern clock, so a 2:30am job skips the day the clocks go forward.

## License

BSD 2-Clause, see `LICENSE.md` at the repository root.
