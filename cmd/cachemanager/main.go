package main

import (
	"fmt"
	"net/http"

	"github.com/mKaloer/tfservingcache/pkg/cachemanager"
	"github.com/mKaloer/tfservingcache/pkg/cachemanager/diskmodelprovider"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/viper"
)

func main() {
	SetConfig()

	if viper.GetString("modelProvider.type") != "diskProvider" {
		log.Fatalf("Unsupported modelProvider: %s", viper.GetString("modelProvider.type"))
	}
	provider := diskmodelprovider.DiskModelProvider{
		BaseDir: viper.GetString("modelProvider.baseDir"),
	}
	modelCache := cachemanager.NewLRUCache(viper.GetString("modelCache.hostModelPath"), viper.GetInt64("modelCache.size"))
	c := cachemanager.New(provider, &modelCache,
		viper.GetString("serving.servingModelPath"), viper.GetString("serving.grpcHost"), viper.GetString("serving.restHost"), 10.0)
	http.HandleFunc("/v1/models/", c.ServeRest())
	http.ListenAndServe(fmt.Sprintf(":%d", viper.GetInt("restPort")), nil)
}
