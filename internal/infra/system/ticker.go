package system

import "time"

// Ticker is started and stopped by the owning worker's Run method.
type Ticker struct{ value *time.Ticker }

func (t *Ticker) Start(interval time.Duration) <-chan time.Time {
	t.value = time.NewTicker(interval)
	return t.value.C
}

func (t *Ticker) Stop() { t.value.Stop() }
