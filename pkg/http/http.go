package http

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"io/ioutil"
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
	// cache
	metricCacheGetResponse = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "sx",
		Subsystem: "cache",
		Name:      "get_response",
		Help:      "Histogram of GetResponse duration buckets and total calls count",
	}, []string{"service", "route", "path"})
	metricCacheGetResponseHit = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "sx",
		Subsystem: "cache",
		Name:      "get_response_hit",
		Help:      "Histogram of GetResponse cache hit count",
	}, []string{"service", "route", "path"})
	metricCacheSetResponse = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "sx",
		Subsystem: "cache",
		Name:      "set_response",
		Help:      "Histogram of SetResponse duration buckets and total calls count",
	}, []string{"service", "route", "path"})
	// request
	metricRouteRequest = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "sx",
		Subsystem: "route",
		Name:      "request",
		Help:      "Histogram of request duration buckets and total calls count",
	}, []string{"service", "route", "path", "status"})
)

// sxCtx is the context value added to request contexts.
type sxCtx struct {
	route       *sx.Route
	originalURL *url.URL
	cacheKey    string
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
	if ctx.route.RouteGroup.Cache != nil {
		// backup body
		b := res.Body
		body, err := ioutil.ReadAll(b)
		if err != nil {
			return err
		}
		defer b.Close()
		res.Body = io.NopCloser(bytes.NewReader(body))
		// set response in cache
		setResponseStart := time.Now()
		g.redis.SetResponse(ctx.cacheKey, redis.Response{
			Status:  res.StatusCode,
			Headers: res.Header,
			Body:    body,
		}, ctx.route.RouteGroup.Cache.TTL)
		metricCacheSetResponse.WithLabelValues(
			ctx.route.RouteGroup.ParentService.Name,
			ctx.route.RouteGroup.Name,
			ctx.route.RouteGroup.AbsolutePath(),
		).Observe(float64(time.Since(setResponseStart).Nanoseconds()))
	}
	// track metrics
	metricRouteRequest.WithLabelValues(
		ctx.route.RouteGroup.ParentService.Name,
		ctx.route.RouteGroup.Name,
		ctx.route.RouteGroup.AbsolutePath(),
		strconv.Itoa(res.StatusCode),
	).Observe(float64(time.Since(ctx.startTime).Nanoseconds()))
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

func (g *Gateway) tryServeCache(rt *sx.Route, w http.ResponseWriter, r *http.Request) bool {
	// get context
	ctx := r.Context().Value(sxCtxKey).(*sxCtx)
	// prepare cache key
	ctx.cacheKey = g.redis.MakeKey(
		"resp",
		ctx.originalURL.Path,
		sx.CacheKeySet(rt.RouteGroup.Cache.Keys).Extract(&httpCacheKeyExtractor{r}))
	// record timing of cache fetch
	getResponseStart := time.Now()
	res, ok := g.redis.GetResponse(ctx.cacheKey)
	metricCacheGetResponse.WithLabelValues(
		rt.RouteGroup.ParentService.Name,
		rt.RouteGroup.Name,
		rt.RouteGroup.AbsolutePath(),
	).Observe(float64(time.Since(getResponseStart).Nanoseconds()))
	// cache hit, handle request from cache
	if ok {
		metricCacheGetResponseHit.WithLabelValues(
			rt.RouteGroup.ParentService.Name,
			rt.RouteGroup.Name,
			rt.RouteGroup.AbsolutePath(),
		).Inc()
		for k, v := range res.Headers {
			for i := 0; i < len(v); i++ {
				w.Header().Set(k, v[i])
			}
		}
		w.WriteHeader(res.Status)
		w.Write(res.Body)
	}
	return ok
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
		if g.tryServeCache(rt, w, r) {
			log.Printf("%s %s (cached)", r.Method, ctx.originalURL)
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
