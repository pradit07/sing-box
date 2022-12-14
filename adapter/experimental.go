package adapter

import (
	"context"
	"net"
	"time"

	"github.com/sagernet/sing-box/common/urltest"
	E "github.com/sagernet/sing/common/exceptions"
	N "github.com/sagernet/sing/common/network"
)

type ClashServer interface {
	Service
	Mode() string
	StoreSelected() bool
	CacheFile() ClashCacheFile
	HistoryStorage() *urltest.HistoryStorage
	RoutedConnection(ctx context.Context, conn net.Conn, metadata InboundContext, matchedRule Rule) (net.Conn, Tracker)
	RoutedPacketConnection(ctx context.Context, conn N.PacketConn, metadata InboundContext, matchedRule Rule) (N.PacketConn, Tracker)
}

type ClashCacheFile interface {
	LoadSelected(group string) string
	StoreSelected(group string, selected string) error
}

type Tracker interface {
	Leave()
}

type Provider interface {
	Service
	Tag() string
	Update() error
	UpdatedAt() time.Time
	Wait()
	Outbounds() []Outbound
	Outbound(tag string) (Outbound, bool)
}

type OutboundGroup interface {
	Outbound
	Now() string
	All() []string
	Outbound(tag string) (Outbound, bool)
	Outbounds() []Outbound
	Providers() []Provider
	Provider(tag string) (Provider, bool)
}

type OutboundCheckGroup interface {
	OutboundGroup
	CheckAll()
	CheckProvider(tag string)
	CheckOutbound(tag string) (uint16, error)
}

type V2RayServer interface {
	Service
	StatsService() V2RayStatsService
}

type V2RayStatsService interface {
	RoutedConnection(inbound string, outbound string, conn net.Conn) net.Conn
	RoutedPacketConnection(inbound string, outbound string, conn N.PacketConn) N.PacketConn
}

func RealOutbound(router Router, group Outbound) (Outbound, error) {
	redirected := group
	nLoop := 0
	for {
		var group OutboundGroup
		group, isGroup := redirected.(OutboundGroup)
		if !isGroup {
			return redirected, nil
		}
		nLoop++
		if nLoop > 100 {
			return nil, E.New("too deep or loop nesting of outbound groups")
		}
		redirected = getOutbound(router, group.Now())
		if redirected == nil {
			return nil, E.New("outbound not found:", group.Now())
		}
	}
}

func getOutbound(router Router, tag string) Outbound {
	if outbound, ok := router.Outbound(tag); ok {
		return outbound
	}
	for _, provider := range router.Providers() {
		if outbound, ok := provider.Outbound(tag); ok {
			return outbound
		}
	}
	return nil
}
