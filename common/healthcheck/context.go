package healthcheck

import (
	"net/http"
	"sync"

	C "github.com/sagernet/sing-box/constant"
)

// Context is the context for health check, it collects network connectivity
// status and checked status of outbounds
//
// About connectivity status collection:
//
// Consider the health checks are done asynchronously, success checks will
// report network is available in a short time, after that, there will be
// failure checks query the network connectivity. So,
//
// 1. In cases of any one check success, the network is known to be available,
// no extra connectivity check needed.
//
// 2. In cases of all checks failed, we can not distinguesh from the network is
// down or all nodes are dead. Only in this case we need to do connectivity
// check, and it's rare.
type Context struct {
	sync.Mutex

	connectivityURL string

	connected *bool
	checked   map[string]bool
}

// NewContext creates a new Connectivity status collector and checker
func NewContext(connectivityURL string) *Context {
	return &Context{
		connectivityURL: connectivityURL,
		checked:         make(map[string]bool),
	}
}

// ReportChecked reports the outbound of the tag is checked
func (c *Context) ReportChecked(tag string) {
	c.Lock()
	defer c.Unlock()
	c.checked[tag] = true
}

// Checked tells if the outbound of the tag is checked
func (c *Context) Checked(tag string) bool {
	c.Lock()
	defer c.Unlock()
	return c.checked[tag]
}

// ReportConnected reports the network is connected
func (c *Context) ReportConnected() {
	c.Lock()
	defer c.Unlock()
	c.udpate(true)
}

// Connected tells if the network connected
func (c *Context) Connected() bool {
	c.Lock()
	defer c.Unlock()
	if c.connected == nil {
		// no report, check it
		c.checkNetwork()
	}
	return *c.connected
}

// Check checks the network connectivity
func (c *Context) checkNetwork() {
	if c.connectivityURL == "" {
		c.udpate(true)
		return
	}
	client := &http.Client{Timeout: C.TCPTimeout}
	defer client.CloseIdleConnections()
	req, err := http.NewRequest(http.MethodHead, c.connectivityURL, nil)
	if err != nil {
		c.udpate(false)
		return
	}
	resp, err := client.Do(req)
	if err != nil {
		c.udpate(false)
		return
	}
	defer resp.Body.Close()
	c.udpate(true)
}

func (c *Context) udpate(available bool) {
	c.connected = &available
}
