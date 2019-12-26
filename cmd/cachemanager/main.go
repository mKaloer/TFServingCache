package main

import (
	"net/http"

	"github.com/mKaloer/tfservingcache/pkg/cachemanager"
)

func main() {
	provider := cachemanager.DiskModelProvider{
		BaseDir: "/dev/fooo",
	}
	modelCache := cachemanager.NewLRUCache(300000)
	c := cachemanager.New("http://localhost:8888", provider, &modelCache)
	http.HandleFunc("/v1/models/", c.ServeRest())
	http.ListenAndServe(":8091", nil)
}
