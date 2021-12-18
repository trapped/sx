package main

import (
	"flag"
	"log"
	"net/http"
	"os"
	"path/filepath"
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

	debounce := debounce.New(5 * time.Second)

	/*
		the following symlink-following logic was inspired by:
		https://github.com/spf13/viper/commit/e0f7631cf3ac7e7530949c7e154855076b0a4c17
	*/

	path, _ = filepath.Abs(path)
	realPath, _ := filepath.EvalSymlinks(path)
	dir, _ := filepath.Split(path)

	done := make(chan bool)
	go func() {
		for {
			select {
			case event := <-watcher.Events:
				currentPath, _ := filepath.EvalSymlinks(path)
				if event.Op&fsnotify.Remove == fsnotify.Remove {
					// ignore file delete events since they would bork the server
					continue
				}
				// either file was modified or a symlink was changed
				if filepath.Clean(event.Name) == path || currentPath != realPath {
					realPath = currentPath
					debounce.Func(func() {
						log.Println("reloading configuration")
						conf, err := readConf(realPath)
						if err != nil {
							log.Printf("error reloading configuration, ignoring: %v", err)
						}
						if err := handler(conf); err != nil {
							log.Printf("error reapplying configuration, ignoring: %v", err)
						}
					})
				}
			case err := <-watcher.Errors:
				log.Fatalf("configuration watcher error: %v", err)
			}
		}
	}()

	log.Printf("watching configuration %v in %s", path, dir)
	err = watcher.Add(dir)
	if err != nil {
		log.Fatalf("error watching configuration: %v", err)
	}
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
