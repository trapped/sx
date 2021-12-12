package http

import (
	"context"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/trapped/sx"
)

func TestGatewayLoadConfig(t *testing.T) {
	conf := new(sx.GatewayConfig)
	f, err := os.Open("../../config.yml")
	if err != nil {
		t.Fatal(err)
	}
	err = conf.Read(f)
	if err != nil {
		t.Fatal(err)
	}
	g := new(Gateway)
	err = g.LoadConfig(conf)
	if err != nil {
		t.Fatal(err)
	}
}

type mockServer struct{}

func (m *mockServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Write([]byte("Hello world!"))
}

func TestGatewayMock(t *testing.T) {
	mock := httptest.NewServer(new(mockServer))
	defer mock.Close()

	g := new(Gateway)
	go func() {
		if err := g.ListenAndServe(":7655"); err != nil && err != http.ErrServerClosed {
			t.Errorf("failed to run gateway: %v", err)
		}
	}()
	defer g.Shutdown(context.Background())

	conf := new(sx.GatewayConfig)
	err := conf.Read(strings.NewReader(fmt.Sprintf(`
services:
  - name: mock
    addresses: ["%s"]
    routes:
      - name: root
        method: GET
        path: /
`, mock.Listener.Addr())))
	if err != nil {
		t.Fatalf("failed reading configuration: %v", err)
		return
	}
	err = g.LoadConfig(conf)
	if err != nil {
		t.Fatalf("failed loading configuration: %v", err)
		return
	}

	resp, err := http.Get("http://localhost:7655/mock/")
	if err != nil {
		t.Errorf("failed fetching root: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("bad status code: %v", err)
	}
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		t.Errorf("error reading body: %v", err)
	} else if string(body) != "Hello world!" {
		t.Errorf("bad body: %v", string(body))
	}

	conf = new(sx.GatewayConfig)
	err = conf.Read(strings.NewReader(fmt.Sprintf(`
services:
  - name: mock
    addresses: ["%s"]
    routes:
      - name: root
        method: GET
        path: /
        auth:
          basic:
            username: test
            password: test
`, mock.Listener.Addr())))
	if err != nil {
		t.Fatalf("failed reading configuration: %v", err)
		return
	}
	err = g.LoadConfig(conf)
	if err != nil {
		t.Fatalf("failed loading configuration: %v", err)
		return
	}

	resp, err = http.Get("http://localhost:7655/mock/")
	if err != nil {
		t.Errorf("failed fetching root: %v", err)
	}
	if resp.StatusCode != 401 {
		t.Errorf("bad status code: %v", err)
	}
	body, err = ioutil.ReadAll(resp.Body)
	if err != nil {
		t.Errorf("error reading body: %v", err)
	} else if string(body) != "{\"code\":401,\"message\":\"forbidden\"}\n" {
		t.Errorf("bad body: %v", string(body))
	}
}
