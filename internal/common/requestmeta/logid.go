package requestmeta

import "context"

type logIDKey struct{}

func WithLogID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, logIDKey{}, id)
}

func LogID(ctx context.Context) string {
	id, _ := ctx.Value(logIDKey{}).(string)
	return id
}
