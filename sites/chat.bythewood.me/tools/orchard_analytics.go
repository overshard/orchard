package tools

import (
	"context"
	"net/url"
	"strconv"
	"strings"
)

type orchardAnalyticsArgs struct {
	Days     float64
	Property string
}

func (a *orchardAnalyticsArgs) params() []param {
	return []param{
		numArg(&a.Days, "days", "how many days back, default 7, up to 365"),
		strArg(&a.Property, "property", "one property by name, optional, otherwise every one"),
	}
}

var OrchardAnalytics = Define(Tool{
	Name:   "orchard_analytics",
	Traits: Private | Estate,
	Description: "Read Isaac's own analytics at analytics.bythewood.me: sessions, page views, live " +
		"users, and the top pages, referrers, countries, browsers and devices for each property. Use " +
		"it for anything about his traffic or where his visitors come from. Read only.",
}, func(ctx context.Context, d *Deps, a orchardAnalyticsArgs) (any, error) {
	q := url.Values{}
	if dd := int(a.Days); dd > 0 {
		q.Set("days", strconv.Itoa(dd))
	}
	p := a.Property
	if p != "" {
		q.Set("property", p)
	}
	var out map[string]any
	if err := estateGet(ctx, d, analyticsBase+"/api/summary?"+q.Encode(), &out); err != nil {
		return nil, err
	}
	// An empty list reads to a model as no traffic at all.
	if found, _ := out["properties"].([]any); p != "" && len(found) == 0 {
		q.Del("property")
		out = nil
		if err := estateGet(ctx, d, analyticsBase+"/api/summary?"+q.Encode(), &out); err != nil {
			return nil, err
		}
		out["note"] = "no property is named " + strconv.Quote(p) + ", so these are all of them"
	}
	if props, ok := out["properties"].([]any); ok {
		for _, prop := range props {
			if m, ok := prop.(map[string]any); ok {
				labelFirst(m)
			}
		}
	}
	return out, nil
})

// labelFirst turns each {"count":8,"label":"/posts/x/"} row into "/posts/x/: 8".
// A map marshals its keys sorted, so the count lands before its label and the
// model pairs each label with the next row's count.
func labelFirst(m map[string]any) {
	for k, v := range m {
		rows, ok := v.([]any)
		if !ok || !strings.HasPrefix(k, "top_") {
			continue
		}
		flat := make([]string, 0, len(rows))
		for _, r := range rows {
			row, _ := r.(map[string]any)
			label, _ := row["label"].(string)
			count, ok := row["count"].(float64)
			if !ok {
				continue
			}
			if label == "" {
				label = "(none)"
			}
			flat = append(flat, label+": "+strconv.FormatFloat(count, 'f', -1, 64))
		}
		m[k] = flat
	}
}
