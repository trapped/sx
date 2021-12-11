package http

import (
	"net/http"
	"runtime"
)

func newTransport() *http.Transport {
	return &http.Transport{
		MaxIdleConns:        1000 * runtime.NumCPU(),
		MaxIdleConnsPerHost: 1000 * runtime.NumCPU(),
	}
}
