package main

import (
	"fmt"
	"net/http"

	"github.com/mKaloer/tfservingcache/pkg/cachemanager"
	"github.com/mKaloer/tfservingcache/pkg/cachemanager/diskmodelprovider"
)

func main() {
	config := SetConfig()

	provider := diskmodelprovider.DiskModelProvider{
		BaseDir: config.ModelRepoDir,
	}
	modelCache := cachemanager.NewLRUCache(config.HostServingModelDir, config.ModelCacheSize)
	c := cachemanager.New(provider, &modelCache,
		config.ServerServingModelDir, config.ServingGrpcHost, config.ServingRestHost, 10.0)
	http.HandleFunc("/v1/models/", c.ServeRest())
	http.ListenAndServe(fmt.Sprintf(":%d", config.RestPort), nil)
}
