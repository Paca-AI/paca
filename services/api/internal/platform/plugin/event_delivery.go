package plugin

import (
	"context"
	"errors"
	"net/textproto"
	"sync"
)

// sendOutcomes collects what happened to the outbound emails a plugin asked
// the host to send while handling one event.
//
// A plugin's HandleEvent returns nothing, and an email that fails to send is
// reported only to the plugin, which can do no more than log it. But the send
// itself runs in the host (paca.send_email), so the host sees every failure.
// Recording them here lets the event dispatcher tell a plugin that handled the
// event from one that tried to email someone and could not — without changing
// the plugin ABI or rebuilding any plugin.
type sendOutcomes struct {
	mu        sync.Mutex
	err       error
	retryable bool
}

type sendOutcomesKey struct{}

func withSendOutcomes(ctx context.Context) (context.Context, *sendOutcomes) {
	o := &sendOutcomes{}
	return context.WithValue(ctx, sendOutcomesKey{}, o), o
}

// recordSendFailure notes a failed send on the event being handled, if any. A
// send made outside an event (an HTTP request handler) records nothing: its
// caller gets the error in the response.
func recordSendFailure(ctx context.Context, err error) {
	o, ok := ctx.Value(sendOutcomesKey{}).(*sendOutcomes)
	if !ok || err == nil {
		return
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	retryable := isRetryableSendError(err)
	// One retryable failure makes the whole delivery retryable; otherwise keep
	// the first error.
	if o.err == nil || (retryable && !o.retryable) {
		o.err = err
		o.retryable = retryable
	}
}

func (o *sendOutcomes) result() (retryable bool, err error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.retryable, o.err
}

// permanentSendError marks a failure of the request itself (missing
// permission, malformed or invalid message): the same request fails the same
// way every time.
type permanentSendError struct{ msg string }

func (e *permanentSendError) Error() string { return e.msg }

// isRetryableSendError reports whether sending the same message again could
// succeed. Connection and timeout failures can; a permanent SMTP reply (5xx:
// bad credentials, rejected recipient) and an invalid request cannot, and
// retrying them only repeats a failure the admin has to fix.
func isRetryableSendError(err error) bool {
	var permanent *permanentSendError
	if errors.As(err, &permanent) {
		return false
	}
	var smtpErr *textproto.Error
	if errors.As(err, &smtpErr) {
		return smtpErr.Code < 500
	}
	return true
}
