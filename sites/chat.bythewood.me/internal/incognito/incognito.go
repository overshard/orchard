// Package incognito marks a turn that writes nothing down. The mark rides the
// context instead of a client, since the clients are shared by every turn, and
// this way the gate, the compaction pass, the tools and the painter all inherit it.
package incognito

import "context"

type key struct{}

// With marks a context as belonging to an incognito turn.
func With(ctx context.Context) context.Context {
	return context.WithValue(ctx, key{}, true)
}

// On reports whether the context belongs to an incognito turn.
func On(ctx context.Context) bool {
	on, _ := ctx.Value(key{}).(bool)
	return on
}
