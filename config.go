package sx

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/pkg/errors"
	"github.com/trapped/sx/pkg/jwt"
	"gopkg.in/yaml.v2"
)

type GatewayConfig struct {
	Redis    Redis      `yaml:"redis"`
	Services []*Service `yaml:"services"`
}

// Read reads GatewayConfig from an io.Reader strictly, returning any
// errors caused by invalid/missing values, required fields or extra
// fields.
func (conf *GatewayConfig) Read(r io.Reader) error {
	d := yaml.NewDecoder(r)
	d.SetStrict(true)
	if err := d.Decode(conf); err != nil {
		return errors.Wrap(err, "error decoding yaml")
	}
	conf.Redis.clean()
	if err := conf.Redis.validate(); err != nil {
		return errors.Wrap(err, "error validating redis")
	}
	svcMap := make(map[string]bool)
	for i, svc := range conf.Services {
		svc.clean()
		if svcMap[svc.Name] {
			return errors.Errorf("service %q already exists", svc.Name)
		}
		svcMap[svc.Name] = true
		if err := svc.validate(conf, i); err != nil {
			return err
		}

		for _, r := range svc.Routes {
			if err := r.Walk(func(parent, g *RouteGroup) error {
				g.Parent = parent
				g.ParentService = svc
				if g.Name == "" && parent != nil {
					g.Name = parent.Name
				}
				if g.Auth == nil && parent != nil {
					g.Auth = parent.Auth
				}
				if g.Cache == nil && parent != nil {
					g.Cache = parent.Cache
				}
				if g.RateLimit == nil && parent != nil {
					g.RateLimit = parent.RateLimit
				}
				return nil
			}); err != nil {
				return errors.Wrapf(err, "in service %q", svc.Name)
			}
		}
	}
	return nil
}

type Service struct {
	Name       string `yaml:"name"`
	PathPrefix string `yaml:"-"`

	Addresses []string `yaml:"addresses"`

	Routes []*RouteGroup `yaml:"routes"`
}

func (s *Service) CompileRoutes() (newroutes []Route, err error) {
	for j := 0; j < len(s.Routes); j++ {
		rg := s.Routes[j]
		if rg != nil {
			if err := rg.Walk(func(p, rg *RouteGroup) error {
				if rg.Path == "" {
					// groups are not endpoints
					return nil
				}
				r, err := NewRoute(rg)
				if err != nil {
					return err
				}
				newroutes = append(newroutes, r)
				return nil
			}); err != nil {
				return nil, err
			}
		}
	}
	return
}

func (s *Service) clean() {
	s.Name = strings.TrimSpace(s.Name)
	for i, addr := range s.Addresses {
		s.Addresses[i] = strings.TrimSpace(addr)
	}
	s.PathPrefix = fmt.Sprintf("/%s", s.Name)
	for _, rg := range s.Routes {
		rg.clean()
	}
}

func (s *Service) validate(conf *GatewayConfig, svcIdx int) error {
	if s.Name == "" {
		return errors.Errorf("in service #%d: name is required", svcIdx)
	}
	if s.Addresses == nil || len(s.Addresses) < 1 {
		return errors.Errorf("in service %q: addresses is required", s.Name)
	}
	for _, rg := range s.Routes {
		if err := rg.validate(conf); err != nil {
			return errors.Wrapf(err, "in service %q", s.Name)
		}
	}
	return nil
}

type RouteGroup struct {
	ParentService *Service    `yaml:"-"`
	Parent        *RouteGroup `yaml:"-"`

	Name   string         `yaml:"name"`
	Method string         `yaml:"method"`
	Path   string         `yaml:"path"`
	Routes *[]*RouteGroup `yaml:"routes"`

	Auth      *Auth      `yaml:"auth"`
	Cache     *Cache     `yaml:"cache"`
	RateLimit *RateLimit `yaml:"ratelimit"`
}

func (rg *RouteGroup) clean() {
	rg.Name = strings.TrimSpace(rg.Name)
	rg.Method = strings.TrimSpace(rg.Method)
	rg.Path = strings.TrimSpace(rg.Path)
	if rg.Auth != nil {
		rg.Auth.clean()
	}
	if rg.Cache != nil {
		rg.Cache.clean()
	}
	if rg.RateLimit != nil {
		rg.RateLimit.clean()
	}
	if rg.Routes == nil {
		return
	}
	for _, r := range *rg.Routes {
		r.clean()
	}
}

func (rg *RouteGroup) validate(conf *GatewayConfig) error {
	if rg.Auth != nil {
		if err := rg.Auth.validate(conf); err != nil {
			return errors.Wrapf(err, "route %q can't validate auth", rg.Name)
		}
	}
	if rg.Cache != nil {
		if err := rg.Cache.validate(conf); err != nil {
			return errors.Wrapf(err, "route %q can't validate cache", rg.Name)
		}
	}
	if rg.RateLimit != nil {
		if err := rg.RateLimit.validate(conf); err != nil {
			return errors.Wrapf(err, "route %q can't validate ratelimit", rg.Name)
		}
	}
	if rg.Routes == nil {
		return nil
	}
	for _, r := range *rg.Routes {
		if err := r.validate(conf); err != nil {
			return errors.Wrapf(err, "in route %q", rg.Name)
		}
	}
	return nil
}

// Walk calls walkFunc recursively on all defined routes until it returns error.
func (rg *RouteGroup) Walk(walkFunc func(parent, group *RouteGroup) error) error {
	return rg.walk(nil, walkFunc)
}

func (rg *RouteGroup) walk(parent *RouteGroup, walkFunc func(parent, group *RouteGroup) error) error {
	if err := walkFunc(parent, rg); err != nil {
		return err
	}
	if rg.Routes == nil {
		return nil
	}
	for _, r := range *rg.Routes {
		if err := r.walk(rg, walkFunc); err != nil {
			return err
		}
	}
	return nil
}

// AbsolutePath constructs the absolute path for the route or group.
func (rg *RouteGroup) AbsolutePath() string {
	path := rg.Path
	parent := rg.Parent
	if parent != nil {
		path = fmt.Sprintf("%s%s", parent.Path, path)
	}
	return path
}

type Auth struct {
	Basic  *AuthBasic  `yaml:"basic"`
	Bearer *AuthBearer `yaml:"bearer"`
}

func (a *Auth) clean() {
	if a.Basic != nil {
		a.Basic.clean()
	}
	if a.Bearer != nil {
		a.Bearer.clean()
	}
}

func (a *Auth) validate(conf *GatewayConfig) error {
	if a.Basic != nil {
		if err := a.Basic.validate(); err != nil {
			return errors.Wrap(err, "auth can't validate basic")
		}
	}
	if a.Bearer != nil {
		if err := a.Bearer.validate(); err != nil {
			return errors.Wrap(err, "auth can't validate bearer")
		}
	}
	return nil
}

type AuthBasic struct {
	Username string `yaml:"username"`
	Password string `yaml:"password"`
}

func (ab *AuthBasic) clean() {
	ab.Username = strings.TrimSpace(ab.Username)
	ab.Password = strings.TrimSpace(ab.Password)
}

func (ab *AuthBasic) validate() error {
	if ab.Username == "" || ab.Password == "" {
		return errors.Errorf("basic auth username and password are both required")
	}
	return nil
}

func (ab *AuthBasic) Verify(username, password string) bool {
	return ab.Username == username && ab.Password == password
}

type AuthBearer struct {
	PublicKey string `yaml:"publickey"`

	verifier jwt.Verifier
}

func (ab *AuthBearer) clean() {
	ab.PublicKey = strings.TrimSpace(ab.PublicKey)
}

func (ab *AuthBearer) validate() error {
	if ab.PublicKey == "" {
		return errors.Errorf("bearer auth requires publickey")
	}
	v, err := jwt.NewSingleKeyVerifier(ab.PublicKey)
	if err != nil {
		return errors.Wrap(err, "bearer auth can't load public key")
	}
	ab.verifier = v
	return nil
}

func (ab *AuthBearer) Verify(token string) error {
	return ab.verifier.Verify(token)
}

type CacheKey struct {
	Header *string `yaml:"header"`
	Query  *string `yaml:"query"`
}

func (ck *CacheKey) clean() {
	if ck.Header != nil {
		*ck.Header = strings.TrimSpace(*ck.Header)
	}
	if ck.Query != nil {
		*ck.Query = strings.TrimSpace(*ck.Query)
	}
}

func (ck *CacheKey) validate() error {
	// TODO: validate JSON, Header & Query value formats
	return nil
}

type CacheKeyExtractor interface {
	ExtractHeader(name string) string
	ExtractQuery(name string) string
}

type CacheKeySet []CacheKey

func (cc CacheKeySet) Extract(x CacheKeyExtractor) (ks []string) {
	for i := 0; i < len(cc); i++ {
		c := cc[i]
		if c.Header != nil {
			ks = append(ks, x.ExtractHeader(*c.Header))
		}
		if c.Query != nil {
			ks = append(ks, x.ExtractQuery(*c.Query))
		}
	}
	return
}

type Cache struct {
	TTL  time.Duration `yaml:"ttl"`
	Keys []CacheKey    `yaml:"keys"`
}

func (c *Cache) clean() {
	for i, k := range c.Keys {
		k.clean()
		c.Keys[i] = k
	}
}

func (c *Cache) validate(conf *GatewayConfig) error {
	if !conf.Redis.configured() {
		return errors.Errorf("cache and ratelimit require redis")
	}
	if c.TTL.Seconds() < 1 {
		return errors.Errorf("cache ttl must be greater than 1s")
	}

	for _, k := range c.Keys {
		if err := k.validate(); err != nil {
			return errors.Wrap(err, "cache can't validate cache key")
		}
	}
	return nil
}

type RateLimit struct {
	PerDay    *int       `yaml:"day"`
	PerHour   *int       `yaml:"hour"`
	PerMinute *int       `yaml:"minute"`
	PerSecond *int       `yaml:"second"`
	Keys      []CacheKey `yaml:"keys"`
}

func (rl *RateLimit) clean() {
	for i, k := range rl.Keys {
		k.clean()
		rl.Keys[i] = k
	}
}

func (rl *RateLimit) validate(conf *GatewayConfig) error {
	if !conf.Redis.configured() {
		return errors.Errorf("cache and ratelimit require redis")
	}
	if rl.PerDay == nil && rl.PerHour == nil && rl.PerMinute == nil && rl.PerSecond == nil {
		return errors.Errorf("ratelimit needs at least one of day, hour, minute or second")
	}
	if *rl.PerDay == 0 && *rl.PerHour == 0 && *rl.PerMinute == 0 && *rl.PerSecond == 0 {
		return errors.Errorf("ratelimit needs at least one of day, hour, minute or second")
	}
	for _, k := range rl.Keys {
		if err := k.validate(); err != nil {
			return errors.Wrap(err, "rate limit can't validate cache key")
		}
	}
	return nil
}

type Redis struct {
	ReadAddresses  []string `yaml:"readaddresses"`
	WriteAddresses []string `yaml:"writeaddresses"`
}

func (r *Redis) clean() {
	ra, wa := []string{}, []string{}
	for _, addr := range r.ReadAddresses {
		s := strings.TrimSpace(addr)
		if s != "" {
			ra = append(ra, s)
		}
	}
	for _, addr := range r.WriteAddresses {
		s := strings.TrimSpace(addr)
		if s != "" {
			wa = append(wa, s)
		}
	}
	r.ReadAddresses = ra
	r.WriteAddresses = wa
}

func (r *Redis) configured() bool {
	read := r.ReadAddresses != nil && len(r.ReadAddresses) > 0
	write := r.WriteAddresses != nil && len(r.WriteAddresses) > 0
	return read && write
}

func (r *Redis) validate() error {
	// TODO: validate they are absolute hosts
	return nil
}
