package auth

import (
	"context"
	"sync"
	"testing"
)

type checkSpy struct {
	mu      sync.Mutex
	verdict Verdict
	asked   int
}

func (s *checkSpy) check(context.Context, string) Verdict {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.asked++
	return s.verdict
}

func TestVerifier_ReportsWhatTheAPISays(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		verdict      Verdict
		wantRejected bool
	}{
		"accepted":                   {Accepted, false},
		"rejected":                   {Rejected, true},
		"unknown lets the call pass": {Unknown, false},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			v := NewVerifier((&checkSpy{verdict: tc.verdict}).check)

			if got := v.Rejected(context.Background(), "tok-"+t.Name()); got != tc.wantRejected {
				t.Fatalf("Rejected = %v, want %v", got, tc.wantRejected)
			}
		})
	}
}

// A token revoked right after an accepted check is refused on the next request:
// initialize and tools/list make no API call of their own to catch it.
func TestVerifier_RefusesATokenRevokedRightAfterItWasAccepted(t *testing.T) {
	t.Parallel()
	spy := &checkSpy{verdict: Accepted}
	v := NewVerifier(spy.check)
	token := "tok-" + t.Name()

	if v.Rejected(context.Background(), token) {
		t.Fatal("an accepted token is refused")
	}
	spy.verdict = Rejected
	if !v.Rejected(context.Background(), token) {
		t.Fatal("a token revoked after an accepted check is still accepted")
	}
	if spy.asked != 2 {
		t.Fatalf("asked %d times, want 2: every request is checked", spy.asked)
	}
}

func TestVerifier_AsksAgainAboutTokensItDidNotAccept(t *testing.T) {
	t.Parallel()
	for name, verdict := range map[string]Verdict{"rejected": Rejected, "unknown": Unknown} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			spy := &checkSpy{verdict: verdict}
			v := NewVerifier(spy.check)

			v.Rejected(context.Background(), "tok")
			v.Rejected(context.Background(), "tok")

			if spy.asked != 2 {
				t.Fatalf("asked %d times, want 2", spy.asked)
			}
		})
	}
}

func TestVerifier_TellsTokensApart(t *testing.T) {
	t.Parallel()
	spy := &checkSpy{verdict: Accepted}
	v := NewVerifier(spy.check)

	v.Rejected(context.Background(), "tok-a")
	spy.verdict = Rejected

	if !v.Rejected(context.Background(), "tok-b") {
		t.Fatal("another token rode on the accepted one")
	}
}
