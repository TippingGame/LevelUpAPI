//go:build unit

package service

import (
	"context"
	"errors"
	"net"
	"os"
	"syscall"
	"testing"
)

func TestClassifyOpenAITransportError(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		persistent bool
	}{
		{
			name:       "socks5 proxy credential rejected",
			err:        errors.New(`Post "https://chatgpt.com/backend-api/codex/responses": socks connect tcp 1.2.3.4:1234->chatgpt.com:443: username/password authentication failed`),
			persistent: true,
		},
		{
			name:       "proxy connection refused",
			err:        errors.New(`proxyconnect tcp: dial tcp 1.2.3.4:1080: connect: connection refused`),
			persistent: true,
		},
		{
			name:       "dns resolution failure",
			err:        errors.New(`dial tcp: lookup proxy.example.com: no such host`),
			persistent: true,
		},
		{
			name:       "client timeout",
			err:        errors.New(`context deadline exceeded (Client.Timeout exceeded while awaiting headers)`),
			persistent: false,
		},
		{
			name:       "i/o timeout",
			err:        errors.New(`dial tcp 1.2.3.4:443: i/o timeout`),
			persistent: false,
		},
		{
			name:       "connection reset",
			err:        errors.New(`read tcp 10.0.0.1:5->2.2.2.2:443: read: connection reset by peer`),
			persistent: false,
		},
		{
			name:       "ECONNREFUSED via net.OpError",
			err:        &net.OpError{Op: "dial", Net: "tcp", Err: &os.SyscallError{Syscall: "connect", Err: syscall.ECONNREFUSED}},
			persistent: true,
		},
		{
			name:       "EHOSTUNREACH bare",
			err:        syscall.EHOSTUNREACH,
			persistent: true,
		},
		{
			name:       "DNS not found typed",
			err:        &net.DNSError{Err: "no such host", Name: "proxy.example.com", IsNotFound: true},
			persistent: true,
		},
		{
			name:       "DNS timeout typed",
			err:        &net.DNSError{Err: "i/o timeout", Name: "proxy.example.com", IsTimeout: true},
			persistent: false,
		},
		{
			name:       "context canceled",
			err:        context.Canceled,
			persistent: false,
		},
		{
			name:       "nil",
			err:        nil,
			persistent: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := classifyOpenAITransportError(tt.err).Persistent; got != tt.persistent {
				t.Fatalf("Persistent = %v, want %v", got, tt.persistent)
			}
		})
	}
}
