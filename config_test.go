package sx

import (
	"os"
	"testing"
)

func TestReadGatewayConfig(t *testing.T) {
	conf := new(GatewayConfig)
	f, err := os.Open("config.yml")
	if err != nil {
		t.Fatal(err)
	}
	err = conf.Read(f)
	if err != nil {
		t.Fatal(err)
	}
}

func TestRedisCleanValidate(t *testing.T) {
	r := &Redis{
		ReadAddresses:  []string{"  localhost:6379   "},
		WriteAddresses: []string{"     "},
	}
	r.clean()
	if len(r.ReadAddresses) != 1 {
		t.Fatalf("valid read address not preserved")
		return
	}
	if len(r.WriteAddresses) != 0 {
		t.Fatalf("invalid write address not removed")
		return
	}
	if r.configured() {
		t.Fatalf("redis should NOT appear configured")
		return
	}
}

func TestCacheRateLimitCleanValidate(t *testing.T) {
	redis := Redis{
		ReadAddresses:  []string{"localhost:6379"},
		WriteAddresses: []string{"localhost:6379"},
	}
	conf := &GatewayConfig{
		Redis: Redis{},
	}
	c := &Cache{}
	r := &RateLimit{}
	c.clean()
	r.clean()
	errRedisNotConfigured := "cache and ratelimit require redis"
	if err := c.validate(conf); err == nil || err.Error() != errRedisNotConfigured {
		t.Errorf("bad validation error: %v", err)
	}
	if err := r.validate(conf); err == nil || err.Error() != errRedisNotConfigured {
		t.Errorf("bad validation error: %v", err)
	}
	conf.Redis = redis
	if err := c.validate(conf); err == nil || err.Error() != "cache ttl must be greater than 1s" {
		t.Errorf("bad validation error: %v", err)
	}
	if err := r.validate(conf); err == nil || err.Error() != "ratelimit needs at least one of day, hour, minute or second" {
		t.Errorf("bad validation error: %v", err)
	}
}
