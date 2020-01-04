package main

import (
	"fmt"
	"net/http"

	"github.com/mKaloer/tfservingcache/pkg/cachemanager"
	"github.com/mKaloer/tfservingcache/pkg/cachemanager/diskmodelprovider"
	"github.com/mKaloer/tfservingcache/pkg/taskhandler"
	"github.com/mKaloer/tfservingcache/pkg/taskhandler/consul"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/viper"
)

func main() {

	SetConfig()

	dService, err := consul.NewDiscoveryService(healthCheck)
	if err != nil {
		log.WithError(err).Fatal("Could not create discovery service")
	}

	cache := CreateCacheManager()
	cacheMux := http.NewServeMux()
	cacheMux.HandleFunc("/v1/models/", cache.ServeRest())
	go http.ListenAndServe(fmt.Sprintf(":%d", viper.GetInt("cacheRestPort")), cacheMux)

	tHandler := taskhandler.New(dService)
	err = tHandler.ConnectToCluster()
	if err != nil {
		log.WithError(err).Fatal("Could not create discovery service")
	}
	defer tHandler.DisconnectFromCluster()
	proxyMux := http.NewServeMux()
	proxyMux.HandleFunc("/v1/models/", tHandler.ServeRest())
	http.ListenAndServe(fmt.Sprintf(":%d", viper.GetInt("proxyRestPort")), proxyMux)
}

func CreateCacheManager() *cachemanager.CacheManager {
	if viper.GetString("modelProvider.type") != "diskProvider" {
		log.Fatalf("Unsupported modelProvider: %s", viper.GetString("modelProvider.type"))
	}
	provider := diskmodelprovider.DiskModelProvider{
		BaseDir: viper.GetString("modelProvider.baseDir"),
	}
	modelCache := cachemanager.NewLRUCache(viper.GetString("modelCache.hostModelPath"), viper.GetInt64("modelCache.size"))
	c := cachemanager.New(provider, &modelCache,
		viper.GetString("serving.servingModelPath"), viper.GetString("serving.grpcHost"), viper.GetString("serving.restHost"), 10.0)
	return c
}

func healthCheck() (bool, error) {
	return true, nil
}
