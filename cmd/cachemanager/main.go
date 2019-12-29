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
	c := cachemanager.New(provider, &modelCache, "/models", "localhost:8500", "http://localhost:8501")
	http.HandleFunc("/v1/models/", c.ServeRest())
	http.ListenAndServe(":8091", nil)
}
