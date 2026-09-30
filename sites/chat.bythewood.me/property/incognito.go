package property

import (
	"context"
	"database/sql"
)

type incognitoKey struct{}

// Incognito marks a lookup whose address and coordinates must not be kept. The
// county and state caches still fill, since those say nothing about which house.
func Incognito(ctx context.Context) context.Context {
	return context.WithValue(ctx, incognitoKey{}, true)
}

func incognito(ctx context.Context) bool {
	on, _ := ctx.Value(incognitoKey{}).(bool)
	return on
}

// keep writes a cache row tied to one place, unless the turn is incognito.
func keep(ctx context.Context, db *sql.DB, query string, args ...any) (sql.Result, error) {
	if incognito(ctx) {
		return nil, nil
	}
	return db.ExecContext(ctx, query, args...)
}
