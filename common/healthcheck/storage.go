package healthcheck

import (
	"time"
)

// Storage holds ping rtts for health Checker
type Storage struct {
	idx      int
	cap      int
	validity time.Duration
	history  []History

	stats       Stats
	expireTimer *time.Timer
}

// History is the rtt history
type History struct {
	Time  time.Time `json:"time"`
	Delay RTT       `json:"delay"`
}

// NewStorage returns a new rtt storage with specified capacity
func NewStorage(cap uint, validity time.Duration) *Storage {
	return &Storage{
		cap:      int(cap),
		validity: validity,
		history:  make([]History, cap, cap),
	}
}

// Put puts a new rtt to the HealthPingResult
func (h *Storage) Put(d RTT) {
	h.idx = h.offset(1)
	now := time.Now()
	h.history[h.idx].Time = now
	h.history[h.idx].Delay = d
	// statistics is not valid any more
	h.removeStats()
}

// Get gets the history at the offset to the latest history, ignores the validity
func (h *Storage) Get(offset int) *History {
	if h == nil {
		return nil
	}
	rtt := h.history[h.offset(offset)]
	if rtt.Time.IsZero() {
		return nil
	}
	return &rtt
}

// Latest gets the latest history, alias of Get(0)
func (h *Storage) Latest() *History {
	if h == nil {
		return nil
	}
	rtt := h.history[h.idx]
	if rtt.Time.IsZero() {
		return nil
	}
	return h.Get(0)
}

// All returns all the history, ignores the validity
func (h *Storage) All() []*History {
	if h == nil {
		return nil
	}
	all := make([]*History, 0, h.cap)
	for i := 0; i < h.cap; i++ {
		rtt := h.history[h.offset(-i)]
		if rtt.Time.IsZero() {
			break
		}
		all = append(all, &rtt)
	}
	return all
}

func (h *Storage) offset(n int) int {
	idx := h.idx
	idx += n
	if idx >= h.cap {
		idx %= h.cap
	} else if idx < 0 {
		idx %= h.cap
		idx += h.cap
	}
	return idx
}
