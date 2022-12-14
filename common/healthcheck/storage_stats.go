package healthcheck

import (
	"math"
	"time"
)

// Stats is the statistics of RTTs
type Stats struct {
	All       int // total number of health checks
	Fail      int // number of failed health checks
	Deviation RTT // standard deviation of RTTs
	Average   RTT // average RTT of all health checks
	Max       RTT // maximum RTT of all health checks
	Min       RTT // minimum RTT of all health checks
	Latest    RTT // latest RTT of all health checks

	Expires time.Time // time of the statistics expires
}

// Stats get statistics and write cache for next call
// Make sure use Mutex.Lock() before calling it, RWMutex.RLock()
// is not an option since it writes cache
func (h *Storage) Stats() Stats {
	if h == nil {
		return Stats{}
	}
	if !h.stats.Expires.IsZero() {
		return h.stats
	}
	h.refreshStats()
	return h.stats
}

func (h *Storage) refreshStats() {
	if h == nil {
		return
	}
	h.removeStats()
	now := time.Now()
	latest := h.history[h.idx]
	if now.Sub(latest.Time) > h.validity {
		return
	}
	h.stats.Latest = latest.Delay
	min := RTT(math.MaxUint16)
	sum := RTT(0)
	cnt := 0
	validRTTs := make([]RTT, 0, h.cap)
	var expiresAt time.Time
	for i := 0; i < h.cap; i++ {
		// from latest to oldest
		idx := h.offset(-i)
		itemExpiresAt := h.history[idx].Time.Add(h.validity)
		if itemExpiresAt.Before(now) {
			// the latter is invalid, so are the formers
			break
		}
		// the time when the oldest item expires
		expiresAt = itemExpiresAt
		if h.history[idx].Delay == Failed {
			h.stats.Fail++
			continue
		}
		cnt++
		sum += h.history[idx].Delay
		validRTTs = append(validRTTs, h.history[idx].Delay)
		if h.stats.Max < h.history[idx].Delay {
			h.stats.Max = h.history[idx].Delay
		}
		if min > h.history[idx].Delay {
			min = h.history[idx].Delay
		}
	}

	// remove stats cache when it expires
	// use self-maintained cache expiring management instead
	// of comparing with time.Now() every time, since calls
	// to time.Now() can be very expensive in some cases.
	if !expiresAt.IsZero() {
		h.expireTimer = time.AfterFunc(
			expiresAt.Sub(now),
			h.removeStats,
		)
	}

	h.stats.Expires = expiresAt
	h.stats.All = cnt + h.stats.Fail
	if cnt > 0 {
		h.stats.Average = RTT(int(sum) / cnt)
	}
	if h.stats.All == 0 || h.stats.Fail == h.stats.All {
		return
	}
	h.stats.Min = min
	var std float64
	if cnt < 2 {
		// no enough data for standard deviation, we assume it's half of the average rtt
		// if we don't do this, standard deviation of 1 round tested nodes is 0, will always
		// selected before 2 or more rounds tested nodes
		std = float64(h.stats.Average / 2)
	} else {
		variance := float64(0)
		for _, rtt := range validRTTs {
			variance += math.Pow(float64(rtt)-float64(h.stats.Average), 2)
		}
		std = math.Sqrt(variance / float64(cnt))
	}
	h.stats.Deviation = RTT(std)
}

func (h *Storage) removeStats() {
	if h == nil {
		return
	}
	if h.expireTimer != nil {
		h.expireTimer.Stop()
	}
	h.stats = Stats{}
	h.expireTimer = nil
}
