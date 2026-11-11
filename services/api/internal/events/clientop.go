package events

import "context"

// ClientOpHeader is the request header a client stamps on a mutation so the
// change it causes can be told apart from changes made by anyone else: the id
// is echoed into the change log (client_op_id), and the client that sent it
// skips the echo of its own change instead of applying it a second time.
const ClientOpHeader = "X-Client-Op-Id"

const maxClientOpLen = 64

type clientOpKey struct{}

// ValidClientOp reports whether id is acceptable as a client operation id: a
// short token of letters, digits, dots, dashes and underscores. Anything else
// is ignored rather than rejected: the id only ever suppresses an echo.
func ValidClientOp(id string) bool {
	if id == "" || len(id) > maxClientOpLen {
		return false
	}
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9',
			r == '.', r == '-', r == '_':
		default:
			return false
		}
	}
	return true
}

// WithClientOp returns ctx carrying the client operation id of the request, so
// every event the request produces is stamped with it. An invalid id is dropped.
func WithClientOp(ctx context.Context, id string) context.Context {
	if !ValidClientOp(id) {
		return ctx
	}
	return context.WithValue(ctx, clientOpKey{}, id)
}

// ClientOp returns the client operation id carried by ctx, or "".
func ClientOp(ctx context.Context) string {
	id, _ := ctx.Value(clientOpKey{}).(string)
	return id
}

// withContextClientOp stamps an event with the request's client operation id
// when its producer left it unset.
func withContextClientOp(ctx context.Context, e Event) Event {
	if e.ClientOpID == "" {
		e.ClientOpID = ClientOp(ctx)
	}
	return e
}
