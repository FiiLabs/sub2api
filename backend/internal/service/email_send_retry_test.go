package service

import (
	"errors"
	"fmt"
	"net/textproto"
	"testing"
)

func TestIsRetryableSMTPError(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"454 throttling", &textproto.Error{Code: 454, Msg: "Throttling failure"}, true},
		{"421 not available", &textproto.Error{Code: 421, Msg: "service not available"}, true},
		{"451 local error", &textproto.Error{Code: 451, Msg: "try again"}, true},
		{"550 mailbox unavailable", &textproto.Error{Code: 550, Msg: "no such user"}, false},
		{"535 auth failed", &textproto.Error{Code: 535, Msg: "auth failed"}, false},
		{"wrapped 452 transient", fmt.Errorf("smtp rcpt: %w", &textproto.Error{Code: 452, Msg: "too many"}), true},
		{"wrapped 553 permanent", fmt.Errorf("smtp mail: %w", &textproto.Error{Code: 553, Msg: "bad address"}), false},
		{"generic network error", errors.New("dial tcp: connection reset"), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isRetryableSMTPError(tc.err); got != tc.want {
				t.Fatalf("isRetryableSMTPError(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

func TestSendWithRetry(t *testing.T) {
	t.Run("succeeds on first attempt", func(t *testing.T) {
		calls := 0
		err := sendWithRetry(func() error { calls++; return nil })
		if err != nil || calls != 1 {
			t.Fatalf("calls=%d err=%v, want calls=1 err=nil", calls, err)
		}
	})

	t.Run("retries transient then succeeds", func(t *testing.T) {
		calls := 0
		err := sendWithRetry(func() error {
			calls++
			if calls < 2 {
				return &textproto.Error{Code: 454, Msg: "Throttling failure"}
			}
			return nil
		})
		if err != nil || calls != 2 {
			t.Fatalf("calls=%d err=%v, want calls=2 err=nil", calls, err)
		}
	})

	t.Run("does not retry permanent 5xx", func(t *testing.T) {
		calls := 0
		permanent := &textproto.Error{Code: 550, Msg: "no such user"}
		err := sendWithRetry(func() error { calls++; return permanent })
		if calls != 1 || !errors.Is(err, permanent) {
			t.Fatalf("calls=%d err=%v, want calls=1 err=permanent", calls, err)
		}
	})

	t.Run("gives up after max attempts on persistent transient error", func(t *testing.T) {
		calls := 0
		err := sendWithRetry(func() error {
			calls++
			return &textproto.Error{Code: 421, Msg: "service not available"}
		})
		if calls != smtpMaxSendAttempts || err == nil {
			t.Fatalf("calls=%d err=%v, want calls=%d err!=nil", calls, err, smtpMaxSendAttempts)
		}
	})
}
