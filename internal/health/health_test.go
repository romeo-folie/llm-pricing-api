package health

import (
	"context"
	"errors"
	"testing"
	"time"
)

func okPing(context.Context) error { return nil }

func errPing(context.Context) error { return errors.New("dependency unreachable") }

// blockUntilCancel blocks until the context is done, then reports its error.
// It stands in for a dependency that hangs — the failure mode this package
// exists to bound.
func blockUntilCancel(ctx context.Context) error {
	<-ctx.Done()
	return ctx.Err()
}

func TestCheckerHealthy(t *testing.T) {
	c := NewChecker(okPing, okPing, time.Second)

	st := c.Check(context.Background())

	if !st.OK() {
		t.Errorf("OK() = false, want true (db=%v redis=%v)", st.DBOK, st.RedisOK)
	}
	if !st.DBOK || !st.RedisOK {
		t.Errorf("DBOK/RedisOK = %v/%v, want true/true", st.DBOK, st.RedisOK)
	}
}

func TestCheckerReportsWhichDependencyFailed(t *testing.T) {
	tests := []struct {
		name      string
		pingDB    func(context.Context) error
		pingRedis func(context.Context) error
		wantDB    bool
		wantRedis bool
	}{
		{"db down", errPing, okPing, false, true},
		{"redis down", okPing, errPing, true, false},
		{"both down", errPing, errPing, false, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			st := NewChecker(tc.pingDB, tc.pingRedis, time.Second).Check(context.Background())
			if st.DBOK != tc.wantDB || st.RedisOK != tc.wantRedis {
				t.Errorf("DBOK/RedisOK = %v/%v, want %v/%v", st.DBOK, st.RedisOK, tc.wantDB, tc.wantRedis)
			}
			if tc.wantDB && tc.wantRedis {
				if !st.OK() {
					t.Error("OK() = false, want true")
				}
				return
			}
			if st.OK() {
				t.Error("OK() = true, want false when a dependency is down")
			}
		})
	}
}

// TestCheckerBoundsHangingDependency is the regression test for the incident:
// /health previously pinged with an unbounded context, so when a dependency
// hung the endpoint hung with it and Railway's healthcheck never failed.
func TestCheckerBoundsHangingDependency(t *testing.T) {
	c := NewChecker(blockUntilCancel, blockUntilCancel, 50*time.Millisecond)

	start := time.Now()
	st := c.Check(context.Background())
	elapsed := time.Since(start)

	if st.OK() {
		t.Error("OK() = true, want false when both dependencies hang")
	}
	if elapsed > time.Second {
		t.Errorf("Check took %v; the timeout was not enforced", elapsed)
	}
}

// TestCheckerBoundsTotalTimeNotPerDependency guards against a sequential
// implementation: two hanging dependencies must not cost twice the timeout.
func TestCheckerBoundsTotalTimeNotPerDependency(t *testing.T) {
	const timeout = 60 * time.Millisecond
	c := NewChecker(blockUntilCancel, blockUntilCancel, timeout)

	start := time.Now()
	c.Check(context.Background())
	elapsed := time.Since(start)

	if elapsed > 3*timeout {
		t.Errorf("Check took %v for a %v budget; checks are not sharing one deadline", elapsed, timeout)
	}
}

func TestCheckerDefaultsTimeoutWhenUnset(t *testing.T) {
	if got := NewChecker(okPing, okPing, 0).timeout; got != DefaultCheckTimeout {
		t.Errorf("timeout = %v, want %v", got, DefaultCheckTimeout)
	}
	if got := NewChecker(okPing, okPing, -time.Second).timeout; got != DefaultCheckTimeout {
		t.Errorf("negative timeout = %v, want %v", got, DefaultCheckTimeout)
	}
}

func TestWatchdogTripsAfterThreshold(t *testing.T) {
	w := NewWatchdog(3)

	if w.Observe(false) {
		t.Fatal("tripped after 1 failure, want 3")
	}
	if w.Observe(false) {
		t.Fatal("tripped after 2 failures, want 3")
	}
	if !w.Observe(false) {
		t.Error("did not trip after 3 consecutive failures")
	}
}

// TestWatchdogHealthyResetsCounter is what stops a single blip from
// accumulating towards a restart.
func TestWatchdogHealthyResetsCounter(t *testing.T) {
	w := NewWatchdog(3)

	w.Observe(false)
	w.Observe(false)
	w.Observe(true) // reset

	if w.Observe(false) {
		t.Error("tripped after reset; a healthy check must clear the count")
	}
	if w.Observe(false) {
		t.Error("tripped at 2 of 3 after reset")
	}
	if !w.Observe(false) {
		t.Error("did not trip after 3 consecutive failures following a reset")
	}
}

// TestWatchdogResetsAfterTrip keeps a watchdog that nobody exits on from
// reporting tripped forever: after a trip the count resets, so the next run
// needs another full run of failures.
func TestWatchdogResetsAfterTrip(t *testing.T) {
	w := NewWatchdog(2)

	if w.Observe(false) {
		t.Fatal("tripped after 1 failure, want 2")
	}
	if !w.Observe(false) {
		t.Fatal("did not trip after 2 consecutive failures")
	}
	if w.Observe(false) {
		t.Error("tripped again immediately after a trip; count was not reset")
	}
	if !w.Observe(false) {
		t.Error("did not trip after a fresh run of 2 consecutive failures")
	}
}

func TestWatchdogThresholdOneTripsImmediately(t *testing.T) {
	if !NewWatchdog(1).Observe(false) {
		t.Error("threshold 1 did not trip on the first failure")
	}
}

func TestWatchdogClampsNonPositiveThreshold(t *testing.T) {
	if !NewWatchdog(0).Observe(false) {
		t.Error("threshold 0 did not trip on the first failure")
	}
	if !NewWatchdog(-5).Observe(false) {
		t.Error("negative threshold did not trip on the first failure")
	}
}

func TestWatchdogHealthyNeverTrips(t *testing.T) {
	w := NewWatchdog(2)
	for i := range 5 {
		if w.Observe(true) {
			t.Fatalf("tripped on healthy observation %d", i+1)
		}
	}
}
