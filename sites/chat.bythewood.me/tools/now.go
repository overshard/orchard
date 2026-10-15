package tools

import (
	"context"
	"fmt"
	"time"
)

var Now = Tool{
	Name: "now",
	Description: "The current date and time. Use it whenever the answer depends on what day it is. " +
		"Pass until for how long it is to a time or a date, and give the remaining it returns rather than working it out.",
	Schema: obj(map[string]any{
		"timezone": str("IANA name, default America/New_York"),
		"until":    str("a time or date to count down to, like \"10am\", \"friday 5pm\", \"October 4\" or \"christmas\""),
	}),
	Run: func(ctx context.Context, d *Deps, a map[string]any) (any, error) {
		name := argStr(a, "timezone")
		if name == "" {
			name = "America/New_York"
		}
		loc, err := time.LoadLocation(name)
		if err != nil {
			return nil, fmt.Errorf("no timezone called %q", name)
		}
		t := d.Now().In(loc)
		out := map[string]any{
			"iso": t.Format(time.RFC3339), "readable": t.Format("Monday, 2 January 2006 at 3:04 PM MST"),
			"weekday": t.Format("Monday"), "timezone": name,
		}
		if u := argStr(a, "until"); u != "" {
			at, ok := ParseUntil(u, t)
			if !ok {
				return nil, fmt.Errorf("could not read %q as a time or a date, try something like \"10am\" or \"October 4 7pm\"", u)
			}
			out["until"] = at.Format("Monday, 2 January 2006 at 3:04 PM MST")
			out["remaining"] = Remaining(t, at)
		}
		return out, nil
	},
}
