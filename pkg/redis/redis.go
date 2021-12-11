package redis

import (
	"context"
	"strings"
	"sync/atomic"
	"time"

	redis "github.com/go-redis/redis/v8"
	"github.com/shamaton/msgpack/v2"
	"github.com/trapped/sx"
)

type Response struct {
	Status  int                 `msgpack:"s"`
	Headers map[string][]string `msgpack:"h"`
	Body    []byte              `msgpack:"b"`
}

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

func (c *Client) MakeKey(mode, url string, keys []string) string {
	return strings.Join(append([]string{mode, url}, keys...), ":")
}

func (c *Client) GetResponse(k string) (resp Response, ok bool) {
	res, err := c.nextRead().Get(context.TODO(), k).Bytes()
	if err != nil {
		return
	}
	if err := msgpack.Unmarshal(res, &resp); err != nil {
		return
	}
	ok = true
	return
}

func (c *Client) SetResponse(k string, resp Response, ttl time.Duration) {
	z, err := msgpack.Marshal(resp)
	if err != nil {
		return
	}
	c.nextWrite().SetNX(context.TODO(), k, z, ttl)
}

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
