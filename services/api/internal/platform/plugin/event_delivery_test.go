package plugin

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"
)

func TestRecordSendFailure_IgnoredOutsideAnEvent(t *testing.T) {
	// No outcomes on the context (an HTTP-handler call): must not panic.
	recordSendFailure(context.Background(), errors.New("boom"))
}

func TestSendOutcomes_RetryableBeatsPermanent(t *testing.T) {
	ctx, o := withSendOutcomes(context.Background())
	recordSendFailure(ctx, &permanentSendError{msg: "bad address"})
	recordSendFailure(ctx, errors.New("dial tcp: connection refused"))
	if retryable, err := o.result(); err == nil || !retryable {
		t.Fatalf("one retryable failure must make the delivery retryable, got retryable=%v err=%v", retryable, err)
	}
}

func TestSendOutcomes_NoFailureIsNoError(t *testing.T) {
	_, o := withSendOutcomes(context.Background())
	if retryable, err := o.result(); err != nil || retryable {
		t.Fatalf("got retryable=%v err=%v, want neither", retryable, err)
	}
}

// fakeSMTP accepts one connection and answers MAIL FROM with mailReply, enough
// of the protocol for net/smtp to reach that step.
func fakeSMTP(t *testing.T, mailReply string) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		r := bufio.NewReader(conn)
		_, _ = fmt.Fprint(conn, "220 fake ESMTP\r\n")
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				return
			}
			switch cmd := strings.ToUpper(strings.TrimSpace(line)); {
			case strings.HasPrefix(cmd, "EHLO"), strings.HasPrefix(cmd, "HELO"):
				_, _ = fmt.Fprint(conn, "250 fake\r\n")
			case strings.HasPrefix(cmd, "MAIL"):
				_, _ = fmt.Fprint(conn, mailReply+"\r\n")
			default:
				_, _ = fmt.Fprint(conn, "250 ok\r\n")
			}
		}
	}()
	return ln.Addr().String()
}

func sendVia(t *testing.T, addr string) error {
	t.Helper()
	prev := smtpDialContext
	smtpDialContext = (&net.Dialer{Timeout: time.Second}).DialContext
	t.Cleanup(func() { smtpDialContext = prev })

	host, portStr, _ := net.SplitHostPort(addr)
	var port int
	_, _ = fmt.Sscanf(portStr, "%d", &port)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	return dialAndSendSMTP(ctx, smtpSendRequest{
		Host: host, Port: port, From: "a@example.com", To: "b@example.com", Subject: "s", TextBody: "t",
	})
}

func TestIsRetryableSendError_ClassifiesRealSMTPFailures(t *testing.T) {
	cases := []struct {
		name      string
		reply     string
		retryable bool
	}{
		{"4xx is a temporary refusal", "451 4.3.0 try again later", true},
		{"5xx is a permanent refusal", "550 5.7.1 relay denied", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := sendVia(t, fakeSMTP(t, c.reply))
			if err == nil {
				t.Fatal("expected the send to fail")
			}
			if got := isRetryableSendError(err); got != c.retryable {
				t.Fatalf("isRetryableSendError(%v) = %v, want %v", err, got, c.retryable)
			}
		})
	}
}

func TestIsRetryableSendError_UnreachableServerIsRetryable(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	_ = ln.Close() // nothing listening now: connection refused

	err = sendVia(t, addr)
	if err == nil {
		t.Fatal("expected the send to fail")
	}
	if !isRetryableSendError(err) {
		t.Fatalf("a refused connection must be retryable, got %v", err)
	}
}

func TestIsRetryableSendError_InvalidRequestIsPermanent(t *testing.T) {
	if isRetryableSendError(&permanentSendError{msg: "send_email: invalid to address"}) {
		t.Fatal("an invalid request must not be retried")
	}
}
