package main

import (
	"flag"
	"log"
	"net/http"
	"os"
	"time"

	_ "net/http/pprof"

	"github.com/fsnotify/fsnotify"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/trapped/sx"
	"github.com/trapped/sx/pkg/debounce"
	"github.com/trapped/sx/pkg/fasthttp"
	h "github.com/trapped/sx/pkg/http"
)

var (
	listenAddr      = flag.String("l", ":7654", "Listen address")
	pprofListenAddr = flag.String("pprof", ":6060", "pprof listen address")
	configPath      = flag.String("f", "config.yml", "Path to configuration file")
	fastHttp        = flag.Bool("fast", false, "Enables valyala/fasthttp for extra performance")
)

func readConf(path string) (*sx.GatewayConfig, error) {
	conf := new(sx.GatewayConfig)
	f, err := os.Open(*configPath)
	if err != nil {
		return nil, errors.Wrap(err, "error opening configuration")
	}
	defer f.Close()
	if err := conf.Read(f); err != nil {
		return nil, errors.Wrap(err, "error reading configuration")
	}
	return conf, nil
}

func watchConfig(path string, handler func(*sx.GatewayConfig) error) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		log.Fatalf("error creating configuration watcher: %v", err)
	}
	defer watcher.Close()

	debounce := debounce.New(1 * time.Second)

	done := make(chan bool)
	go func() {
		for {
			select {
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}
				if event.Op&fsnotify.Remove == fsnotify.Remove {
					// ignore file delete events since they would bork the server
					continue
				}
				debounce.Func(func() {
					log.Println("reloading configuration")
					conf, err := readConf(path)
					if err != nil {
						log.Printf("error reloading configuration, ignoring: %v", err)
					}
					if err := handler(conf); err != nil {
						log.Printf("error reapplying configuration, ignoring: %v", err)
					}
				})
			case err, ok := <-watcher.Errors:
				if !ok {
					return
				}
				log.Fatalf("configuration watcher error: %v", err)
			}
		}
	}()
	err = watcher.Add(path)
	if err != nil {
		log.Fatalf("error watching configuration: %v", err)
	}
	<-done
}

func main() {
	flag.Parse()

	conf, err := readConf(*configPath)
	if err != nil {
		log.Fatal(err)
	}

	log.Printf("pprof and metrics listening at %s", *pprofListenAddr)
	go func() {
		http.Handle("/metrics", promhttp.Handler())
		http.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte("OK\n"))
		})
		log.Fatal(http.ListenAndServe(*pprofListenAddr, nil))
	}()

	log.Printf("listening at %s", *listenAddr)
	if *fastHttp {
		g := new(fasthttp.Gateway)
		if err := g.LoadConfig(conf); err != nil {
			log.Fatalf("error loading configuration: %v", err)
		}
		go watchConfig(*configPath, func(c *sx.GatewayConfig) error {
			return g.LoadConfig(c)
		})
		if err := g.ListenAndServe(*listenAddr); err != nil {
			log.Fatalf("error starting listener: %v", err)
		}
	} else {
		g := new(h.Gateway)
		if err := g.LoadConfig(conf); err != nil {
			log.Fatalf("error loading configuration: %v", err)
		}
		go watchConfig(*configPath, func(c *sx.GatewayConfig) error {
			return g.LoadConfig(c)
		})
		if err := g.ListenAndServe(*listenAddr); err != nil {
			log.Fatalf("error starting listener: %v", err)
		}
	}
}
