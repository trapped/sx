package http

import (
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync/atomic"
)

type backend struct {
	proxy *httputil.ReverseProxy
	url   *url.URL
}

type backendgroup struct {
	backends []backend
	rr       uint32
}

func (bg *backendgroup) next() *backend {
	if bg.backends == nil {
		return nil
	}
	b := &bg.backends[bg.rr%uint32(len(bg.backends))]
	atomic.AddUint32(&bg.rr, 1)
	return b
}

func parseURL(u string) (*url.URL, error) {
	if strings.IndexByte(u, ':') >= 0 && !strings.Contains(u, "://") {
		host, port, err := net.SplitHostPort(u)
		if err != nil {
			return nil, err
		}
		return url.Parse("http://" + host + ":" + port)
	}
	return url.Parse(u)
}

func newBackendGroup(g *Gateway, upstreams []string) (bg *backendgroup, err error) {
	bg = &backendgroup{
		backends: make([]backend, len(upstreams)),
	}
	for i := 0; i < len(upstreams); i++ {
		burl, err := parseURL(upstreams[i])
		if err != nil {
			return nil, err
		}
		proxy := httputil.NewSingleHostReverseProxy(burl)
		proxy.Transport = newTransport()
		proxy.ModifyResponse = func(r *http.Response) error {
			return g.postResponse(r.Request, r)
		}
		bg.backends[i] = backend{proxy, burl}
	}
	return
}
