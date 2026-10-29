package kubernetes

import (
	"errors"
	"testing"
	"time"
)

func TestRetry_ReturnsImmediatelyOnSuccess(t *testing.T) {
	calls := 0

	start := time.Now()
	err := retry(5, time.Second, func() error {
		calls++
		return nil
	})
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("retry returned an error: %v", err)
	}
	if calls != 1 {
		t.Errorf("operation ran %d times, want 1", calls)
	}
	if elapsed > 100*time.Millisecond {
		t.Errorf("retry slept %s before returning a success", elapsed)
	}
}

func TestRetry_SucceedsOnALaterAttempt(t *testing.T) {
	calls := 0

	err := retry(5, time.Millisecond, func() error {
		calls++
		if calls < 3 {
			return errors.New("TLS handshake timeout")
		}
		return nil
	})

	if err != nil {
		t.Fatalf("retry returned an error: %v", err)
	}
	if calls != 3 {
		t.Errorf("operation ran %d times, want 3", calls)
	}
}

func TestRetry_GivesUpAfterTheLastAttemptAndReturnsItsError(t *testing.T) {
	calls := 0
	final := errors.New("still unreachable")

	err := retry(4, time.Millisecond, func() error {
		calls++
		if calls == 4 {
			return final
		}
		return errors.New("earlier failure")
	})

	if calls != 4 {
		t.Errorf("operation ran %d times, want 4", calls)
	}
	if !errors.Is(err, final) {
		t.Errorf("retry returned %v, want the error from the last attempt", err)
	}
}

func TestRetry_DoesNotSleepAfterTheFinalAttempt(t *testing.T) {
	// Three attempts with a 40ms base delay sleep after attempts 1 and 2 only,
	// so ~120ms of backoff. A sleep after the last attempt would add 160ms.
	start := time.Now()
	err := retry(3, 40*time.Millisecond, func() error {
		return errors.New("unreachable")
	})
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected an error when every attempt fails")
	}
	if elapsed > 150*time.Millisecond {
		t.Errorf("retry took %s; it slept after the final attempt", elapsed)
	}
}

func TestRetry_BacksOffBetweenAttempts(t *testing.T) {
	// 40ms then 80ms of backoff across three attempts.
	start := time.Now()
	retry(3, 40*time.Millisecond, func() error {
		return errors.New("unreachable")
	})
	elapsed := time.Since(start)

	if elapsed < 110*time.Millisecond {
		t.Errorf("retry took %s; the delay does not grow between attempts", elapsed)
	}
}
