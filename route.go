package sx

import (
	"fmt"

	"github.com/gobwas/glob"
)

type Route struct {
	Pattern         string
	CompiledPattern glob.Glob

	RouteGroup *RouteGroup
}

func (r *Route) Match(path string) bool {
	return r.CompiledPattern.Match(path)
}

func NewRoute(r *RouteGroup) (Route, error) {
	pattern := fmt.Sprintf("%s%s", r.ParentService.PathPrefix, r.AbsolutePath())
	compiled, err := glob.Compile(pattern)
	if err != nil {
		return Route{}, err
	}
	return Route{pattern, compiled, r}, nil
}
