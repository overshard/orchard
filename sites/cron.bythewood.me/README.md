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
`POST /api/jobs/{id}/run`, `POST /api/jobs/{id}/try`,
`GET /api/runs?job=&result=&limit=` and `GET /api/preview?when=`, `?cron=` or
`?at=`. A job is `name`, `kind` (`notify` or `prompt`), `when`, `at` or `cron`,
`message`, and optionally `title`, `priority` (ntfy's min to urgent) and `link`.
`when` takes plain words like `every weekday at 7:30am`, `mon and thu 8pm`,
`on the 1st every 3 months`, `in 2 hours` or `dec 24 at 9am`, and a cron line or
a timestamp too. A cron weekday can be `thu#4` for the 4th Thursday of the month
or `mon#l` for the last Monday. A run's `result` is pushed, quiet, failed or missed.

## Layout

`cronexpr.go` parses a cron line and finds its next time on a wall clock,
`when.go` reads plain words into a cron line or a one-off, `scheduler.go` is one goroutine that sleeps until the soonest job, fires it and
writes the run, `model.go` asks llm for a check, `api.go` is the JSON API and
the validation the form shares, `pages.go` and `view.go` the pages and the week,
`actions.go` try, move and bulk pause, and `db.go` the jobs, runs, keys and
snooze tokens. Templates and `static/` are written by hand, with
no Vite.

## Things that will bite you

- A one-shot is deleted once it lands, and its run row keeps the name. Any
  push that can't reach ntfy is tried every five minutes, six times, and then
  dropped with an error in the log. A repeating job goes back to its own
  schedule after that, or sooner if its next time comes first.
- A repeating job missed by over an hour while the container was down skips to
  its next time instead of firing late, and writes a missed run for each fire
  it skipped, at most twelve a job. One that runs less than daily gets a
  seventh of its period instead, so a yearly birthday down for a day still
  fires, late and saying so. A push given up on is a dropped run.
- Try it runs a job without sending it. It's built on a type with no publisher,
  so it can't reach ntfy, write a run or mint a snooze, and a test holds it to
  that.
- Only a one-off can be dragged to another day in the week. A repeating job's
  days are its cron line.
- Each push carries a snooze button that posts back to
  `https://cron.bythewood.me/snooze/<token>` from the phone. The token is the
  only credential, so it's single use and gone in a week.
- A time sent with no offset is read as Eastern, and a cron line is matched on
  the Eastern clock, so a 2:30am job skips the day the clocks go forward.

## License

BSD 2-Clause, see `LICENSE.md` at the repository root.
