package auth

import (
	"context"
	"sync/atomic"
)

// Rejection records that the API refused the caller's access token during one HTTP
// request. The router answers such a request with a 401 challenge instead of the
// tool result, so the MCP client refreshes the token rather than showing an error.
type Rejection struct{ rejected atomic.Bool }

type rejectionKey struct{}

func WithRejection(ctx context.Context) (context.Context, *Rejection) {
	r := &Rejection{}
	return context.WithValue(ctx, rejectionKey{}, r), r
}

// MarkTokenRejected is a no-op outside a request the router is watching.
func MarkTokenRejected(ctx context.Context) {
	if r, ok := ctx.Value(rejectionKey{}).(*Rejection); ok {
		r.rejected.Store(true)
	}
}

func (r *Rejection) Rejected() bool { return r.rejected.Load() }
