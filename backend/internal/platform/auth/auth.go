// Package auth carries the authenticated project or user through the request context.
// Handlers read it and pass it explicitly to use cases; use cases never see the context value.
package auth

import "context"

type ctxKey struct{}

type userCtxKey struct{}

// WithProjectID returns a context carrying the authenticated project ID.
func WithProjectID(ctx context.Context, projectID string) context.Context {
	return context.WithValue(ctx, ctxKey{}, projectID)
}

// ProjectID returns the authenticated project ID, and false when the request is unauthenticated.
func ProjectID(ctx context.Context) (string, bool) {
	id, ok := ctx.Value(ctxKey{}).(string)
	return id, ok && id != ""
}

// WithUserID returns a context carrying the verified dashboard user ID.
func WithUserID(ctx context.Context, userID string) context.Context {
	return context.WithValue(ctx, userCtxKey{}, userID)
}

// UserID returns the verified user ID, and false when the request has no verified session.
func UserID(ctx context.Context) (string, bool) {
	id, ok := ctx.Value(userCtxKey{}).(string)
	return id, ok && id != ""
}
