// package redis provides a Redis cache backend implementation.
package redis

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	redis "github.com/go-redis/redis/v8"
	"github.com/trapped/sx"
)

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
func (c *Client) GetResponse(ctx context.Context, k string) (resp *http.Response, ok bool) {
	res, err := c.nextRead().Get(ctx, k).Bytes()
	if err != nil {
		return
	}
	buf := bufio.NewReader(bytes.NewBuffer(res))
	resp, err = http.ReadResponse(buf, nil)
	return resp, err == nil
}

// SetResponse stores an HTTP response into Redis with the provided TTL.
func (c *Client) SetResponse(ctx context.Context, k string, resp *http.Response, ttl time.Duration) {
	// backup body
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Printf("error reading response body: %v", err)
		return
	}
	if resp.ContentLength <= 0 {
		resp.ContentLength = int64(len(body))
	}
	r := bytes.NewReader(body)
	// restore body
	resp.Body = io.NopCloser(r)
	// serialize response
	buf := bytes.NewBuffer(nil)
	err = resp.Write(buf)
	if err != nil {
		log.Printf("error writing response to cache buffer: %v", err)
		return
	}
	if err := c.nextWrite().SetNX(ctx, k, buf.Bytes(), ttl).Err(); err != nil {
		log.Printf("cache write error: %v", err)
	}
	r.Seek(0, io.SeekStart)
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
