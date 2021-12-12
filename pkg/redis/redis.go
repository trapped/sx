// package redis provides a Redis cache backend implementation.
package redis

import (
	"context"
	"log"
	"strings"
	"sync/atomic"
	"time"

	redis "github.com/go-redis/redis/v8"
	"github.com/shamaton/msgpack/v2"
	"github.com/trapped/sx"
)

// Response represents an HTTP response.
type Response struct {
	Status  int                 `msgpack:"s"`
	Headers map[string][]string `msgpack:"h"`
	Body    []byte              `msgpack:"b"`
}

// Client encapsulates sets of Redis clients for reading and writing,
// as well as round-robin sequences.
type Client struct {
	readClients  []*redis.Client
	writeClients []*redis.Client
	readIdx      uint32
	writeIdx     uint32
}

func (c *Client) nextRead() *redis.Client {
	next := c.readClients[c.readIdx%uint32(len(c.readClients))]
	atomic.AddUint32(&c.readIdx, 1)
	return next
}
func (c *Client) nextWrite() *redis.Client {
	next := c.writeClients[c.writeIdx%uint32(len(c.writeClients))]
	atomic.AddUint32(&c.writeIdx, 1)
	return next
}

// MakeKey joins the provided mode, url and keys into a single cache key string.
func (c *Client) MakeKey(mode, url string, keys []string) string {
	return strings.Join(append([]string{"sx", mode, url}, keys...), ":")
}

// GetResponse fetches a previously cached HTTP response from Redis.
func (c *Client) GetResponse(k string) (resp Response, ok bool) {
	res, err := c.nextRead().Get(context.TODO(), k).Bytes()
	if err != nil {
		return
	}
	if err := msgpack.Unmarshal(res, &resp); err != nil {
		log.Printf("msgpack unmarshal error: %v", err)
		return
	}
	ok = true
	return
}

// SetResponse stores an HTTP response into Redis with the provided TTL.
func (c *Client) SetResponse(k string, resp Response, ttl time.Duration) {
	z, err := msgpack.Marshal(resp)
	if err != nil {
		log.Printf("msgpack marshal error: %v", err)
		return
	}
	if err := c.nextWrite().SetNX(context.TODO(), k, z, ttl).Err(); err != nil {
		log.Printf("cache write error: %v", err)
	}
}

// NewClient initializes a new set of Redis clients according to the specified configuration.
func NewClient(conf sx.Redis) *Client {
	c := &Client{
		readClients:  []*redis.Client{},
		writeClients: []*redis.Client{},
	}
	for _, readAddr := range conf.ReadAddresses {
		c.readClients = append(c.readClients, redis.NewClient(&redis.Options{
			Addr: readAddr,
		}))
	}
	for _, writeAddr := range conf.WriteAddresses {
		c.writeClients = append(c.writeClients, redis.NewClient(&redis.Options{
			Addr: writeAddr,
		}))
	}
	return c
}
