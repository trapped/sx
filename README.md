SX: a minimal, declarative API gateway
======================================

> **WARNING: not production ready. Use at your own risk!**
> Need something better? Check out nginx, Caddy or Envoy

SX is an all-in-one HTTP API gateway.
It's intended to be the least-intrusive and simple as possible. It does not support TLS by design: add it on top.

This doesn't mean it's not feature packed - in fact, it provides:

- load balancing with multiple addresses (if running in Kubernetes you should just use the Service address)
- service mapping in YAML
- route grouping/matching (with simple glob patterns)
- route group authorization (Basic Auth or JWT Bearer token)
- caching and rate limiting (WIP) (requires Redis)
    - both support key extraction from request parameters
- Prometheus metrics (and provides a simple Grafana dashboard you can import and customize)

It's very simple to deploy, and provides standard YAML manifests for:

- docker-compose (includes Redis; also includes pre-configured Prometheus and Grafana)
- Kubernetes (Redis not included)

Importantly, it supports auto-reload for the Kubernetes ConfigMap. Note this can take a few seconds up to a few minutes depending on the kubelet's sync interval.

It's also stateless - which means you can simply horizontally scale it, run in on preemptible/spot nodes, whatever (you should bring your own scaling for Redis, though).

## Running

From the command line: `sx -l :7654 -pprof :6060 -f config.yml`

In docker-compose using the provided manifest: `docker-compose -f sx/docker-compose.yml up`

In Kubernetes: import the `kubernetes/sx.yml` manifest using kustomize or your preferred tool, then customize its configuration, resources...

## Configuration

Here's an example configuration file:

```yml
redis:
  readaddresses:
    - localhost:6379
  writeaddresses:
    - localhost:6379

services:
  - name: example
    addresses:
      - localhost:8080
    routes:
      - routes:
          - name: private
            method: POST
            path: /private
            ratelimit:
              perminute: 30
            auth:
              bearer:
                publickey: '{"use":"sign","kty":"oct","kid":"005456ff-1262-4bf0-a608-8534e1fe2763","alg":"HS256","k":"L0FCL4hivd7ShePdJnzEEoqlwoOfCrkcqdbXdADNk0s523xV7C5Sr6GiRIMpvNIelEsR6ta7MZnELY4JoHrm_w"}'
          - name: home
            method: GET
            path: /*
            cache:
              ttl: 5m
```

> The above example uses a symmetric key (oct) for simplicity - you should use something asymmetric like RSA or EC.

Routes are matched in the order they are defined.

SX prefixes service paths with `/{service name}`, so in the above example it would expose:

- `/example/private` -> `localhost:8080/private`
- `/example/*` -> `localhost:8080/*`

## Caching

You can enable caching for groups or single routes by specifying at least a Time-To-Live.

The request URL is always used as cache key; you can optionally add more keys (to partition your cache).

Responses are only cached if their status code indicates an OK result (1xx, 2xx, 3xx).

## Prometheus metrics and profiling

SX exposes Prometheus metrics on the address specified with `-pprof` (`0.0.0.0:6060` by default) at `/metrics`.

You can also attach using `go tool pprof`.

It's recommended not exposing this port to the external world.

Prometheus metrics follow this format:

- system (Go) metrics: `go_*`
- route metrics: `sx_route_*` with labels `service`, `route`, `method`, `path`, `status`
- cache metrics: `sx_cache_*` with labels `service`, `route`, `method`, `path`

Timings are always provided as seconds.

You can also find an example Grafana dashboard in `grafana/`.

A health check endpoint is also available at `/healthz`.

## `valyala/fasthttp` support

SX partially supports `valyala/fasthttp` which can be enabled with the `-fast` flag. Many things don't work, though, for example:

- request/response streaming: fasthttp currently buffers everything
- cache/rate limiting
- metrics
