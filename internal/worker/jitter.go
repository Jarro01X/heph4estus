package worker

import (
	"crypto/rand"
	"math/big"
	"time"
)

// JitterDuration returns a random duration between 0 and maxSeconds (millisecond
// granularity) using crypto/rand for unpredictable delays. Returns 0 if
// maxSeconds <= 0 (disabled). This function does NOT sleep — use ApplyJitter
// for that.
func JitterDuration(maxSeconds int) time.Duration {
	if maxSeconds <= 0 {
		return 0
	}
	n, err := rand.Int(rand.Reader, big.NewInt(int64(maxSeconds)*1000))
	if err != nil {
		return 0
	}
	return time.Duration(n.Int64()) * time.Millisecond
}

// ApplyJitter sleeps for a random duration between 0 and maxSeconds.
// Returns the duration slept. Returns 0 immediately if maxSeconds <= 0.
func ApplyJitter(maxSeconds int) time.Duration {
	d := JitterDuration(maxSeconds)
	if d > 0 {
		time.Sleep(d)
	}
	return d
}
