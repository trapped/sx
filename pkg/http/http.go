package http

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/trapped/sx"
	"github.com/trapped/sx/pkg/redis"
)

var (
	metricLatencyDefaultBuckets = []float64{
		0.001, 0.005,
		0.01, 0.02, 0.03, 0.04, 0.05, 0.06, 0.07, 0.08, 0.09,
		0.1, 0.2, 0.3, 0.4, 0.5, 0.6, 0.7, 0.8, 0.9,
		1, 2, 3, 4, 5, 10, 15, 20, 25, 30,
	}
	// cache
	metricCacheGetResponse = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "sx",
		Subsystem: "cache",
		Name:      "get_response",
		Help:      "Histogram of GetResponse duration buckets and total calls count",
		Buckets:   metricLatencyDefaultBuckets,
	}, []string{"service", "route", "path", "method"})
	metricCacheGetResponseHit = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "sx",
		Subsystem: "cache",
		Name:      "get_response_hit",
		Help:      "Histogram of GetResponse cache hit count",
	}, []string{"service", "route", "path", "method"})
	metricCacheSetResponse = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "sx",
		Subsystem: "cache",
		Name:      "set_response",
		Help:      "Histogram of SetResponse duration buckets and total calls count",
		Buckets:   metricLatencyDefaultBuckets,
	}, []string{"service", "route", "path", "method"})
	// request
	metricRouteRequest = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "sx",
		Subsystem: "route",
		Name:      "request",
		Help:      "Histogram of request duration buckets and total calls count",
		Buckets:   metricLatencyDefaultBuckets,
	}, []string{"service", "route", "path", "method", "status"})
)

// sxCtx is the context value added to request contexts.
type sxCtx struct {
	route       *sx.Route
	originalURL *url.URL
	cacheKey    string
	cached      bool
	startTime   time.Time
}

// sxCtxKey is the key used for setting and retrieving sxCtx from request contexts.
var sxCtxKey sxCtx

type Gateway struct {
	routes          []sx.Route
	serviceBackends map[string]*backendgroup
	redis           *redis.Client
	s               *http.Server
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
	// get context
	ctx := req.Context().Value(sxCtxKey).(*sxCtx)
	// update cache
	if ctx.route.RouteGroup.Cache != nil && !ctx.cached && res.StatusCode < 400 {
		// set response in cache
		setResponseStart := time.Now()
		g.redis.SetResponse(req.Context(), ctx.cacheKey, res, ctx.route.RouteGroup.Cache.TTL)
		metricCacheSetResponse.WithLabelValues(
			ctx.route.RouteGroup.ParentService.Name,
			ctx.route.RouteGroup.Name,
			ctx.route.RouteGroup.AbsolutePath(),
			req.Method,
		).Observe(float64(time.Since(setResponseStart).Seconds()))
	}
	// track metrics
	metricRouteRequest.WithLabelValues(
		ctx.route.RouteGroup.ParentService.Name,
		ctx.route.RouteGroup.Name,
		ctx.route.RouteGroup.AbsolutePath(),
		req.Method,
		strconv.Itoa(res.StatusCode),
	).Observe(float64(time.Since(ctx.startTime).Seconds()))
	return nil
}

func (g *Gateway) rewriteRequest(rt *sx.Route, b *backend, r *http.Request) *http.Request {
	// store values in context
	originalURL := *r.URL
	ctxVal := sxCtx{
		route:       rt,
		originalURL: &originalURL,
		startTime:   time.Now(),
	}
	// we don't know if upstream supports TLS
	r.URL.Scheme = "http"
	// http://sx-gateway/upstream/... -> http://upstream-url/upstream/...
	r.URL.Host = b.url.Host
	r.Host = b.url.Host
	// http://upstream-url/upstream/... -> http://upstream-url/...
	r.URL.Path = strings.TrimPrefix(r.URL.Path, rt.RouteGroup.ParentService.PathPrefix)
	// return curried request
	return r.WithContext(context.WithValue(r.Context(), sxCtxKey, &ctxVal))
}

func (g *Gateway) tryServeCache(rt *sx.Route, w http.ResponseWriter, r *http.Request) (resp *http.Response, ok bool) {
	// get context
	ctx := r.Context().Value(sxCtxKey).(*sxCtx)
	// prepare cache key
	ctx.cacheKey = g.redis.MakeKey(
		"resp",
		ctx.originalURL.Path,
		sx.CacheKeySet(rt.RouteGroup.Cache.Keys).Extract(&httpCacheKeyExtractor{r}))
	// record timing of cache fetch
	getResponseStart := time.Now()
	resp, ok = g.redis.GetResponse(r.Context(), ctx.cacheKey)
	metricCacheGetResponse.WithLabelValues(
		rt.RouteGroup.ParentService.Name,
		rt.RouteGroup.Name,
		rt.RouteGroup.AbsolutePath(),
		r.Method,
	).Observe(float64(time.Since(getResponseStart).Seconds()))
	// cache hit, handle request from cache
	if resp != nil && ok {
		metricCacheGetResponseHit.WithLabelValues(
			rt.RouteGroup.ParentService.Name,
			rt.RouteGroup.Name,
			rt.RouteGroup.AbsolutePath(),
			r.Method,
		).Inc()
		for k, vs := range resp.Header {
			for i := 0; i < len(vs); i++ {
				w.Header().Set(k, vs[i])
			}
		}
		w.WriteHeader(resp.StatusCode)
		io.Copy(w, resp.Body)
		resp.Body.Close()
		ctx.cached = true
	}
	return resp, ok
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
	// TODO: set SX values in context rather than headers
	// rewrite request
	r = g.rewriteRequest(rt, b, r)
	ctx := r.Context().Value(sxCtxKey).(*sxCtx)

	// TODO: check rate limit
	// try serving from cache
	if rt.RouteGroup.Cache != nil {
		if resp, ok := g.tryServeCache(rt, w, r); ok {
			log.Printf("%s %s (cached)", r.Method, ctx.originalURL)
			g.postResponse(r, resp)
			return
		}
	}
	// forward request to upstream
	log.Printf("%s %s -> %s", r.Method, ctx.originalURL, r.URL)
	b.proxy.ServeHTTP(w, r)
}

// ListenAndServe is the entrypoint to run the Gateway.
func (g *Gateway) ListenAndServe(addr string) error {
	s := &http.Server{
		Addr:    addr,
		Handler: g,
	}
	g.s = s
	return s.ListenAndServe()
}

// Shutdown gracefully stops the gateway.
func (g *Gateway) Shutdown(ctx context.Context) error {
	if g.s != nil {
		return g.s.Shutdown(ctx)
	}
	return nil
}
