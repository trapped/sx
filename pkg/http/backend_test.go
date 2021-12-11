package http

import (
	"net/url"
	"testing"
)

func TestParseURL(t *testing.T) {
	withscheme, _ := parseURL("http://google.com")
	localportnoscheme, _ := parseURL("localhost:8080")
	if withscheme.Host != "google.com" || withscheme.Scheme != "http" {
		t.Errorf("bad parsed url: %v", withscheme)
	}
	if localportnoscheme.Host != "localhost:8080" || localportnoscheme.Scheme != "http" {
		t.Errorf("bad parsed url: %v", localportnoscheme)
	}
}

func TestBackendGroupNext(t *testing.T) {
	mustparse := func(s string) *url.URL {
		u, err := parseURL(s)
		if err != nil {
			panic(err)
		}
		return u
	}
	bg := &backendgroup{
		backends: []backend{
			{url: mustparse("http://google.com")},
			{url: mustparse("localhost:8080")},
		},
	}
	next_1 := bg.next().url.Host == "google.com"
	next_2 := bg.next().url.Host == "localhost:8080"
	next_3 := bg.next().url.Host == "google.com"
	next_4 := bg.next().url.Host == "localhost:8080"
	if !(next_1 && next_2 && next_3 && next_4) {
		t.Error("bad backendgroup round robin rotation")
	}
}
