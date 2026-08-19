package backoff

import (
	"testing"
	"time"
)

func TestNextAttemptDelayExponentialAndCapped(t *testing.T) {
	base := time.Minute
	max := 8 * time.Minute

	caps := []time.Duration{time.Minute, 2 * time.Minute, 4 * time.Minute, 8 * time.Minute, 8 * time.Minute}
	for attempt, cap := range caps {
		d := NextAttemptDelay(attempt+1, base, max)
		if d < 0 || d > cap {
			t.Fatalf("intento %d: delay %v fuera del rango [0, %v]", attempt+1, d, cap)
		}
	}
}

func TestNextAttemptDelayNeverExceedsMax(t *testing.T) {
	max := 8 * time.Minute
	for i := 0; i < 1000; i++ {
		d := NextAttemptDelay(50, time.Minute, max)
		if d < 0 || d > max {
			t.Fatalf("delay %v excede el tope %v", d, max)
		}
	}
}

func TestNextAttemptDelayDefaults(t *testing.T) {
	d := NextAttemptDelay(0, 0, 0)
	if d < 0 || d > time.Minute {
		t.Fatalf("delay con defaults %v fuera del rango [0, 1m]", d)
	}
}
