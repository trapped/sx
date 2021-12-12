package fasthttp

import (
	"os"
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
