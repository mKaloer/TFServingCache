package main

import (
	"net/http"

	"github.com/mKaloer/tfservingcache/pkg/cachemanager"
	"github.com/mKaloer/tfservingcache/pkg/cachemanager/diskmodelprovider"
)

func main() {
	provider := diskmodelprovider.DiskModelProvider{
		BaseDir: "./model_repo",
	}
	modelCache := cachemanager.NewLRUCache("./models/", 300000)
	c := cachemanager.New("http://localhost:8888", provider, &modelCache, "/models")
	http.HandleFunc("/v1/models/", c.ServeRest())
	http.ListenAndServe(":8091", nil)
}
