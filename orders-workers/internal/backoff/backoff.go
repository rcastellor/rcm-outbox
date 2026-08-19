// Package backoff calcula el retardo de reintento para los eventos que no
// pudieron publicarse en SNS. Usa backoff exponencial acotado con jitter
// completo para dispersar los reintentos y evitar el thundering herd.
package backoff

import (
	"math/rand/v2"
	"time"
)

// NextAttemptDelay devuelve cuánto esperar antes del siguiente intento de
// publicación, en función del número de intentos ya realizados (attempt >= 1).
// El retardo crece exponencialmente (base × 2^(attempt-1)) hasta max y se le
// aplica jitter completo: el resultado está en el intervalo [0, exponencial].
func NextAttemptDelay(attempt int, base, max time.Duration) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	if base <= 0 {
		base = time.Minute
	}
	if max <= 0 || max < base {
		max = base
	}

	exp := base
	for i := 1; i < attempt; i++ {
		exp *= 2
		if exp >= max {
			exp = max
			break
		}
	}
	if exp <= 0 {
		return 0
	}
	return time.Duration(rand.Int64N(int64(exp)))
}
