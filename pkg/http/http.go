package http

import (
	"bytes"
	"encoding/json"
	"io"
	"io/ioutil"
	"net/http"
	"strings"

	"github.com/pkg/errors"
	"github.com/trapped/sx"
	"github.com/trapped/sx/pkg/redis"
)

type Gateway struct {
	routes          []sx.Route
	serviceBackends map[string]*backendgroup
	redis           *redis.Client
}

func (g *Gateway) match(path string) *sx.Route {
	for i := 0; i < len(g.routes); i++ {
		r := &g.routes[i]
		if r.Match(path) {
			return r
		}
	}
	return nil
}

func writeError(w http.ResponseWriter, e sx.Error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(e.Code)
	json.NewEncoder(w).Encode(e)
}

// LoadConfig configures the Gateway to use a new configuration.
// If the gateway was already running, it will start using the new
// configuration for new requests.
func (g *Gateway) LoadConfig(conf *sx.GatewayConfig) error {
	newroutes := make([]sx.Route, 0)
	serviceBackends := make(map[string]*backendgroup)
	for i := 0; i < len(conf.Services); i++ {
		svc := conf.Services[i]
		if bg, err := newBackendGroup(g, svc.Addresses); err != nil {
			return errors.Wrapf(err, "in service %q", svc.Name)
		} else {
			serviceBackends[svc.Name] = bg
		}
		svcRoutes, err := svc.CompileRoutes()
		if err != nil {
			return errors.Wrapf(err, "in service %q", svc.Name)
		}
		newroutes = append(newroutes, svcRoutes...)
	}
	// XXX: swap, definitely unsafe
	g.routes = newroutes
	g.serviceBackends = serviceBackends
	g.redis = redis.NewClient(conf.Redis)
	return nil
}

func (g *Gateway) authorize(rg *sx.RouteGroup, r *http.Request) bool {
	if rg.Auth == nil {
		return true
	}
	if rg.Auth.Basic != nil {
		username, password, ok := r.BasicAuth()
		return ok || rg.Auth.Basic.Verify(username, password)
	}
	if rg.Auth.Bearer != nil {
		token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		return rg.Auth.Bearer.Verify(token) == nil
	}
	return true
}

type httpCacheKeyExtractor struct {
	r *http.Request
}

func (x *httpCacheKeyExtractor) ExtractHeader(name string) string {
	return x.r.Header.Get(name)
}

func (x *httpCacheKeyExtractor) ExtractQuery(name string) string {
	return x.r.URL.Query().Get(name)
}

func (g *Gateway) postResponse(req *http.Request, res *http.Response) error {
	path := req.Header.Get("X-SX-Path")
	rt := g.match(path)
	// update cache
	if rt != nil && rt.RouteGroup.Cache != nil {
		cacheKey := req.Header.Get("X-SX-Key")
		// backup body
		b := res.Body
		body, err := ioutil.ReadAll(b)
		if err != nil {
			return err
		}
		defer b.Close()
		res.Body = io.NopCloser(bytes.NewReader(body))
		// set response in cache
		g.redis.SetResponse(cacheKey, redis.Response{
			Status:  res.StatusCode,
			Headers: res.Header,
			Body:    body,
		}, rt.RouteGroup.Cache.TTL)
	}
	return nil
}

// ServeHTTP implements the standard Go HTTP interface.
func (g *Gateway) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	rt := g.match(r.URL.Path)
	if rt == nil || rt.RouteGroup == nil {
		writeError(w, sx.ErrorNotFound)
		return
	}
	if !g.authorize(rt.RouteGroup, r) {
		writeError(w, sx.ErrorForbidden)
		return
	}
	if rt.RouteGroup.Method != "" && r.Method != rt.RouteGroup.Method {
		writeError(w, sx.ErrorBadMethod)
		return
	}
	// get next backend to proxy request to
	b := g.serviceBackends[rt.RouteGroup.ParentService.Name].next()
	if b == nil {
		writeError(w, sx.ErrorBadGateway)
		return
	}
	// patch request
	r.URL.Scheme = "http"
	originalPath := r.URL.Path
	r.Header.Set("X-SX-Path", originalPath)
	r.URL.Host = b.url.Host
	r.URL.Path = strings.TrimPrefix(r.URL.Path, rt.RouteGroup.ParentService.PathPrefix)
	// TODO: check rate limit
	// fetch from cache
	if rt.RouteGroup.Cache != nil {
		cacheKeys := sx.CacheKeySet(rt.RouteGroup.Cache.Keys).Extract(&httpCacheKeyExtractor{r})
		cacheKey := g.redis.MakeKey("resp", originalPath, cacheKeys)
		r.Header.Set("X-SX-Key", cacheKey)
		res, ok := g.redis.GetResponse(cacheKey)
		if ok {
			for k, v := range res.Headers {
				for i := 0; i < len(v); i++ {
					w.Header().Set(k, v[i])
				}
			}
			w.WriteHeader(res.Status)
			w.Write(res.Body)
			return
		}
	}
	b.proxy.ServeHTTP(w, r)
}

// ListenAndServe is the entrypoint to run the Gateway.
func (g *Gateway) ListenAndServe(addr string) error {
	s := &http.Server{
		Addr:    addr,
		Handler: g,
	}
	return s.ListenAndServe()
}
