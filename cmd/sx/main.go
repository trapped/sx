package main

import (
	"flag"
	"log"
	"net/http"
	"os"

	_ "net/http/pprof"

	"github.com/trapped/sx"
	"github.com/trapped/sx/pkg/fasthttp"
	h "github.com/trapped/sx/pkg/http"
)

var (
	listenAddr      = flag.String("l", ":7654", "Listen address")
	pprofListenAddr = flag.String("pprof", ":6060", "pprof listen address")
	configPath      = flag.String("f", "config.yml", "Path to configuration file")
	fastHttp        = flag.Bool("fast", false, "Enables valyala/fasthttp for extra performance")
)

func main() {
	flag.Parse()

	conf := new(sx.GatewayConfig)
	f, err := os.Open(*configPath)
	if err != nil {
		panic(err)
	}
	defer f.Close()
	if err := conf.Read(f); err != nil {
		panic(err)
	}

	log.Printf("pprof listening at %s", *pprofListenAddr)
	go func() {
		log.Fatal(http.ListenAndServe(*pprofListenAddr, nil))
	}()

	log.Printf("listening at %s", *listenAddr)
	if *fastHttp {
		g := new(fasthttp.Gateway)
		if err := g.LoadConfig(conf); err != nil {
			panic(err)
		}
		if err := g.ListenAndServe(*listenAddr); err != nil {
			panic(err)
		}
	} else {
		g := new(h.Gateway)
		if err := g.LoadConfig(conf); err != nil {
			panic(err)
		}
		if err := g.ListenAndServe(*listenAddr); err != nil {
			panic(err)
		}
	}
}
