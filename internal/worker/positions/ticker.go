package positions

import "time"

type timeTicker struct {
	ticker *time.Ticker
}

func NewTicker(interval time.Duration) Ticker {
	return &timeTicker{ticker: time.NewTicker(interval)}
}

func (t *timeTicker) C() <-chan time.Time {
	return t.ticker.C
}

func (t *timeTicker) Stop() {
	t.ticker.Stop()
}
