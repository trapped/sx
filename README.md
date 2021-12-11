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
- caching and rate limiting (requires Redis)
    - both support key extraction from request parameters
- Prometheus metrics (and provides a simple Grafana dashboard you can import and customize)

It's very simple to deploy, and provides standard YAML manifests for:

- docker-compose (includes Redis; also includes pre-configured Prometheus and Grafana)
- Kubernetes (Redis not included)

Importantly, it supports auto-reload for the Kubernetes ConfigMap. Note this can take a few seconds up to a few minutes depending on the kubelet's sync interval.

It's also stateless - which means you can simply horizontally scale it, run in on preemptible/spot nodes, whatever (you should bring your own scaling for Redis, though).

## `valyala/fasthttp` support

SX partially supports `valyala/fasthttp`. Many things don't work, though, for example:

- request/response streaming: fasthttp currently buffers everything
- cache/rate limiting
- metrics
