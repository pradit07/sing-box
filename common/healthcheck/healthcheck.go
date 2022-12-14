package healthcheck

import (
	"context"
	"errors"
	"time"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/common/urltest"
	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing/common/batch"
	E "github.com/sagernet/sing/common/exceptions"
)

var _ adapter.Service = (*HealthCheck)(nil)

// errors
var (
	ErrNoNetWork = errors.New("no network")
)

// HealthCheck is the health checker for balancers
type HealthCheck struct {
	Storage *Storages

	router         adapter.Router
	logger         log.Logger
	globalHistory  *urltest.HistoryStorage
	providers      []adapter.Provider
	providersByTag map[string]adapter.Provider

	options *option.HealthCheckOptions

	cancel context.CancelFunc
}

// New creates a new HealthPing with settings.
//
// The globalHistory is optional and is only used to sync latency history
// between different health checkers. Each HealthCheck will maintain its own
// history storage since different ones can have different check destinations,
// sampling numbers, etc.
func New(
	router adapter.Router,
	providers []adapter.Provider, providersByTag map[string]adapter.Provider,
	options *option.HealthCheckOptions, logger log.Logger,
) *HealthCheck {
	if options == nil {
		options = &option.HealthCheckOptions{}
	}
	if options.Destination == "" {
		//goland:noinspection HttpUrlsUsage
		options.Destination = "http://www.gstatic.com/generate_204"
	}
	if options.Interval < option.Duration(10*time.Second) {
		options.Interval = option.Duration(10 * time.Second)
	}
	if options.Sampling <= 0 {
		options.Sampling = 10
	}
	var globalHistory *urltest.HistoryStorage
	if clashServer := router.ClashServer(); clashServer != nil {
		globalHistory = clashServer.HistoryStorage()
	}
	return &HealthCheck{
		router:         router,
		logger:         logger,
		globalHistory:  globalHistory,
		providers:      providers,
		providersByTag: providersByTag,
		options:        options,
		Storage: NewStorages(
			options.Sampling,
			time.Duration(options.Sampling+1)*time.Duration(options.Interval),
		),
	}
}

// Start starts the health check service, implements adapter.Service
func (h *HealthCheck) Start() error {
	if h.cancel != nil {
		return nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	h.cancel = cancel
	go func() {
		// wait for all providers to be ready
		for _, p := range h.providers {
			p.Wait()
		}
		go h.checkLoop(ctx)
		go h.cleanupLoop(ctx, 8*time.Hour)
	}()
	return nil
}

// Close stops the health check service, implements adapter.Service
func (h *HealthCheck) Close() error {
	if h.cancel != nil {
		h.cancel()
		h.cancel = nil
	}
	return nil
}

// ReportFailure reports a failure of the node
func (h *HealthCheck) ReportFailure(outbound adapter.Outbound) {
	if _, ok := outbound.(adapter.OutboundGroup); ok {
		return
	}
	tag := outbound.Tag()
	history := h.Storage.Latest(tag)
	if history == nil || history.Delay != Failed {
		// don't put more failed records if it's known failed,
		// or it will interferes with the max_fail assertion
		h.Storage.Put(tag, Failed)
	}
}

func (h *HealthCheck) checkLoop(ctx context.Context) {
	batch, _ := batch.New(context.Background(), batch.WithConcurrencyNum[any](10))
	h.checkAll(batch)
	ticker := time.NewTicker(time.Duration(h.options.Interval))
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			h.checkAll(batch)
		}
	}
}

// CheckProvider performs checks for nodes of the provider
func (h *HealthCheck) CheckProvider(tag string) {
	p, ok := h.providersByTag[tag]
	if !ok {
		return
	}
	batch, _ := batch.New(context.Background(), batch.WithConcurrencyNum[any](10))
	// share ctx information between checks
	ctx := NewContext(h.options.Connectivity)
	h.checkProvider(batch, p, ctx)
	batch.Wait()
}

// CheckOutbound performs check for the specified node
func (h *HealthCheck) CheckOutbound(tag string) (uint16, error) {
	ctx := NewContext(h.options.Connectivity)
	if outbound, ok := h.outbound(tag); ok {
		return h.checkOutbound(outbound, ctx)
	}
	return 0, E.New("outbound not found")
}

// CheckAll performs checks for nodes of all providers
func (h *HealthCheck) CheckAll() {
	batch, _ := batch.New(context.Background(), batch.WithConcurrencyNum[any](10))
	h.checkAll(batch)
	batch.Wait()
}

func (h *HealthCheck) checkAll(batch *batch.Batch[any]) {
	// share ctx information between checks
	ctx := NewContext(h.options.Connectivity)
	for _, provider := range h.providers {
		provider := provider
		h.checkProvider(batch, provider, ctx)
	}
}

func (h *HealthCheck) checkProvider(batch *batch.Batch[any], provider adapter.Provider, ctx *Context) {
	for _, outbound := range provider.Outbounds() {
		outbound := outbound
		tag := outbound.Tag()
		batch.Go(
			tag,
			func() (any, error) {
				return h.checkOutbound(outbound, ctx)
			},
		)
	}
}

func (h *HealthCheck) outbound(tag string) (adapter.Outbound, bool) {
	for _, provider := range h.providers {
		outbound, ok := provider.Outbound(tag)
		if ok {
			return outbound, ok
		}
	}
	return nil, false
}

func (h *HealthCheck) checkOutbound(outbound adapter.Outbound, ctx *Context) (uint16, error) {
	if group, isGroup := outbound.(adapter.OutboundGroup); isGroup {
		real, err := adapter.RealOutbound(h.router, group)
		if err != nil {
			return 0, err
		}
		outbound = real
	}
	tag := outbound.Tag()
	if ctx.Checked(tag) {
		// the outbound is checked, but the the dealy history may not be updated yet, return 0.
		// it won't cause any problem, since the method is called only by `CheckOutbound()` and `checkProvider()``:
		// 1. CheckOutbound passes a new context, not sharing with other checks, it won't get into this branch
		// 2. checkProvider passes a shared context, it may reach here, but it doesn't care about the result
		return 0, nil
	}
	ctx.ReportChecked(tag)
	testCtx, cancel := context.WithTimeout(context.Background(), C.TCPTimeout)
	defer cancel()
	testCtx = log.ContextWithOverrideLevel(testCtx, log.LevelDebug)
	t, err := urltest.URLTest(testCtx, h.options.Destination, outbound)
	if err == nil {
		rtt := RTT(t)
		h.logger.Debug("outbound ", tag, " available: ", rtt)
		ctx.ReportConnected()
		h.Storage.Put(tag, rtt)
		if h.globalHistory != nil {
			h.globalHistory.StoreURLTestHistory(tag, &urltest.History{
				Time:  time.Now(),
				Delay: t,
			})
		}
		return t, nil
	}
	if !ctx.Connected() {
		return 0, ErrNoNetWork
	}
	h.logger.Debug("outbound ", tag, " unavailable: ", err)
	h.Storage.Put(tag, Failed)
	if h.globalHistory != nil {
		h.globalHistory.StoreURLTestHistory(tag, &urltest.History{
			Time:  time.Now(),
			Delay: 0,
		})
	}
	return 0, err
}

func (h *HealthCheck) cleanupLoop(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(time.Duration(h.options.Interval))
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			h.cleanup()
		}
	}
}

func (h *HealthCheck) cleanup() {
	for _, tag := range h.Storage.List() {
		if _, ok := h.outbound(tag); !ok {
			h.Storage.Delete(tag)
		}
	}
}
