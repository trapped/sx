package fasthttp

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"strings"

	"github.com/pkg/errors"
	"github.com/trapped/sx"
	"github.com/trapped/sx/pkg/tricks"
	"github.com/valyala/fasthttp"
)

type Gateway struct {
	routes         []sx.Route
	serviceBackend map[string]*fasthttp.HostClient
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

func writeError(ctx *fasthttp.RequestCtx, e sx.Error) {
	ctx.SetContentType("application/json")
	ctx.SetStatusCode(e.Code)
	json.NewEncoder(ctx).Encode(e)
}

func parseAuthorization(t string, auth []byte) (username, password string) {
	i := bytes.IndexByte(auth, ' ')
	if i == -1 {
		return
	}
	if !bytes.EqualFold(auth[:i], tricks.StringToBytes(t)) {
		return
	}
	decoded, err := base64.StdEncoding.DecodeString(tricks.BytesToString(auth[i+1:]))
	if err != nil {
		return
	}
	credentials := bytes.Split(decoded, []byte{':'})
	if len(credentials) >= 1 {
		username = tricks.BytesToString(credentials[0])
	}
	if len(credentials) >= 2 {
		password = tricks.BytesToString(credentials[1])
	}
	return
}

func (g *Gateway) authorize(rg *sx.RouteGroup, ctx *fasthttp.RequestCtx) bool {
	if rg.Auth == nil {
		return true
	}
	if rg.Auth.Basic != nil {
		username, password := parseAuthorization("basic", ctx.Request.Header.Peek("Authorization"))
		return rg.Auth.Basic.Verify(username, password)
	}
	if rg.Auth.Bearer != nil {
		token, _ := parseAuthorization("bearer", ctx.Request.Header.Peek("Authorization"))
		return rg.Auth.Bearer.Verify(token) == nil
	}
	return true
}

// ServeFastHTTP implements the valyala/fasthttp handler interface.
func (g *Gateway) ServeFastHTTP(ctx *fasthttp.RequestCtx) {
	rt := g.match(tricks.BytesToString(ctx.Path()))
	if rt == nil || rt.RouteGroup == nil {
		writeError(ctx, sx.ErrorNotFound)
		return
	}
	if !g.authorize(rt.RouteGroup, ctx) {
		writeError(ctx, sx.ErrorForbidden)
		return
	}
	if rt.RouteGroup.Method != "" && tricks.BytesToString(ctx.Method()) != rt.RouteGroup.Method {
		writeError(ctx, sx.ErrorBadMethod)
		return
	}
	// TODO: check rate limit
	// TODO: fetch from cache
	// get next backend to proxy request to
	backend := g.serviceBackend[rt.RouteGroup.ParentService.Name]
	// rewrite the URI
	uri := ctx.URI()
	uri.SetPathBytes(uri.Path()[len(rt.RouteGroup.ParentService.PathPrefix):])
	// wire up streams
	// execute the request
	if err := backend.DoRedirects(&ctx.Request, &ctx.Response, 50); err != nil {
		ctx.Logger().Printf("error proxying request: %v", err)
	}
}

// LoadConfig configures the Gateway to use a new configuration.
// If the gateway was already running, it will start using the new
// configuration for new requests.
func (g *Gateway) LoadConfig(conf *sx.GatewayConfig) error {
	newroutes := make([]sx.Route, 0)
	serviceBackend := make(map[string]*fasthttp.HostClient)
	for i := 0; i < len(conf.Services); i++ {
		svc := conf.Services[i]
		serviceBackend[svc.Name] = &fasthttp.HostClient{
			Addr:     strings.Join(svc.Addresses, ","),
			MaxConns: 1000 * len(svc.Addresses),
		}
		svcRoutes, err := svc.CompileRoutes()
		if err != nil {
			return errors.Wrapf(err, "in service %q", svc.Name)
		}
		newroutes = append(newroutes, svcRoutes...)
	}
	// XXX: swap, definitely unsafe
	g.routes = newroutes
	g.serviceBackend = serviceBackend
	return nil
}

func (g *Gateway) ListenAndServe(addr string) error {
	return fasthttp.ListenAndServe(addr, g.ServeFastHTTP)
}

// // AuthorizeFastHTTP is called for each request to check if it's authorized to pass through.
// func (rg *RouteGroup) AuthorizeFastHTTP(r *fasthttp.Request) bool {
// 	if rg.Auth == nil {
// 		return true
// 	}
// 	if rg.Auth.Basic != nil {
// 		username, password := parseAuthorization("basic", r.Header.Peek("Authorization"))
// 		return username != rg.Auth.Basic.Username || password != rg.Auth.Basic.Password
// 	}
// 	if rg.Auth.Bearer != nil {
// 		token, _ := parseAuthorization("bearer", r.Header.Peek("Authorization"))
// 		return rg.Auth.Bearer.verifier.Verify(token) == nil
// 	}
// 	return true
// }
