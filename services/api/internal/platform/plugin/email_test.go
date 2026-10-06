package plugin

import (
	"context"
	"net"
	"strings"
	"testing"
)

func TestDialAndSendSMTP_BlocksPrivateTargets(t *testing.T) {
	// A live loopback listener proves the block happens before any connect.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	accepted := make(chan struct{}, 1)
	go func() {
		if c, err := ln.Accept(); err == nil {
			accepted <- struct{}{}
			_ = c.Close()
		}
	}()
	loopPort := ln.Addr().(*net.TCPAddr).Port

	for _, tc := range []struct {
		host string
		port int
		tls  bool
	}{
		{"127.0.0.1", loopPort, false},
		{"127.0.0.1", loopPort, true},
		{"localhost", loopPort, false},
		{"169.254.169.254", 80, false},
		{"10.0.0.1", 6379, false},
		{"::1", loopPort, false},
		{"::ffff:127.0.0.1", loopPort, false},
	} {
		err := dialAndSendSMTP(context.Background(), smtpSendRequest{
			Host: tc.host, Port: tc.port, UseTLS: tc.tls,
			From: "a@example.com", To: "b@example.com", TextBody: "x",
		})
		if err == nil || !strings.Contains(err.Error(), "private/internal") {
			t.Errorf("%s:%d tls=%v: want private/internal block, got %v", tc.host, tc.port, tc.tls, err)
		}
	}
	select {
	case <-accepted:
		t.Fatal("listener received a connection; guard was bypassed")
	default:
	}
}
