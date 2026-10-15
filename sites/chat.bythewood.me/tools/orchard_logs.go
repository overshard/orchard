package tools

import (
	"context"
	"net/url"
	"strconv"
)

type orchardLogsArgs struct {
	Hours    float64
	Errors   float64
	Source   string
	Contains string
}

func (a *orchardLogsArgs) params() []param {
	return []param{
		numArg(&a.Hours, "hours", "how far back to look, default 24, up to 720"),
		numArg(&a.Errors, "errors", "how many kinds of error to return, default 20"),
		strArg(&a.Source, "source", "one site by name, optional, such as search or repos or chat"),
		strArg(&a.Contains, "contains", "only errors whose message, reason or path contains this, optional"),
	}
}

var OrchardLogs = Define(Tool{
	Name:   "orchard_logs",
	Traits: Private | Estate,
	Description: "Read Isaac's own log aggregation at logging.bythewood.me: how many records and " +
		"errors each of his sites produced, every kind of error with the reason it gives, and the " +
		"busiest paths with their p95 latency. Use it for anything about whether his sites are " +
		"misbehaving, what is erroring, or what is slow. The refused section is the one to read for " +
		"anything about suspicious traffic, scanners or probing: errors will be zero on a day when the " +
		"edge turned away thousands of them, because a refused request is not an error anywhere. " +
		"direct_hits there counts requests that reached the origin without passing Cloudflare. " +
		"Each error comes back grouped, so count " +
		"and first_seen say how big it is and how long it has run, and the details field carries " +
		"the reason, which is where the actual cause is rather than in the message. Call it a " +
		"second time with source or contains to narrow down on one thing. Read only.",
}, func(ctx context.Context, d *Deps, a orchardLogsArgs) (any, error) {
	q := url.Values{}
	if h := int(a.Hours); h > 0 {
		q.Set("hours", strconv.Itoa(h))
	}
	if e := int(a.Errors); e > 0 {
		q.Set("errors", strconv.Itoa(e))
	}
	if v := a.Source; v != "" {
		q.Set("source", v)
	}
	if v := a.Contains; v != "" {
		q.Set("contains", v)
	}
	var out any
	err := estateGet(ctx, d, loggingBase+"/api/summary?"+q.Encode(), &out)
	return out, err
})
