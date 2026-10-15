package tools

import "context"

var OrchardStatus = Define(Tool{
	Name: "orchard_status",
	Description: "Read Isaac's own uptime monitoring at status.bythewood.me: every property he " +
		"watches, whether it is up, when it was last checked, its Lighthouse scores and its crawler " +
		"state. Use it for whether a site of his is down or slow, or how it scores. Read only.",
}, func(ctx context.Context, d *Deps, _ noArgs) (any, error) {
	var out any
	err := estateGet(ctx, d, statusBase+"/api/properties", &out)
	return out, err
})
