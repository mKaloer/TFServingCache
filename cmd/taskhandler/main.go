package main

import (
	"fmt"
	"net/http"

	"github.com/mKaloer/TFServingCache/pkg/cachemanager"
	"github.com/mKaloer/TFServingCache/pkg/cachemanager/modelproviders/diskmodelprovider"
	"github.com/mKaloer/TFServingCache/pkg/cachemanager/modelproviders/s3modelprovider"
	"github.com/mKaloer/TFServingCache/pkg/taskhandler"
	"github.com/mKaloer/TFServingCache/pkg/taskhandler/discovery/consul"
	"github.com/mKaloer/TFServingCache/pkg/taskhandler/discovery/etcd"
	"github.com/mKaloer/TFServingCache/pkg/taskhandler/discovery/kubernetes"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/viper"
)

func main() {

	SetConfig()

	dService := CreateDiscoveryService()

	cache := CreateCacheManager()
	cacheMux := http.NewServeMux()
	cacheMux.HandleFunc("/v1/models/", cache.ServeRest())
	go http.ListenAndServe(fmt.Sprintf(":%d", viper.GetInt("cacheRestPort")), cacheMux)

	go cache.GrpcProxy.Listen(viper.GetInt("cacheGrpcPort"))
	defer cache.GrpcProxy.Close()

	tHandler := taskhandler.NewTaskHandler(dService)
	err := tHandler.ConnectToCluster()
	if err != nil {
		log.WithError(err).Fatal("Could not connect to cluster")
	}
	defer tHandler.DisconnectFromCluster()

	go tHandler.GrpcProxy.Listen(viper.GetInt("proxyGrpcPort"))
	defer tHandler.GrpcProxy.Close()

	proxyMux := http.NewServeMux()
	proxyMux.HandleFunc("/v1/models/", tHandler.ServeRest())
	proxyMux.HandleFunc(viper.GetString("metrics.metricsPath"), promhttp.Handler().ServeHTTP)
	// Num forwarded (grpc + rest)
	// Num handled (grpc + rest)
	// Cache hits
	// Cache misses
	// Avg response time (grpc + rest)

	http.ListenAndServe(fmt.Sprintf(":%d", viper.GetInt("proxyRestPort")), proxyMux)
}

func CreateCacheManager() *cachemanager.CacheManager {
	provider := CreateModelProvider()
	modelCache := cachemanager.NewLRUCache(viper.GetString("modelCache.hostModelPath"), viper.GetInt64("modelCache.size"))
	c := cachemanager.New(provider, &modelCache,
		viper.GetString("serving.servingModelPath"),
		viper.GetString("serving.grpcHost"),
		viper.GetString("serving.restHost"),
		10.0,
		viper.GetInt("serving.maxConcurrentModels"))
	return c
}

func CreateDiscoveryService() taskhandler.DiscoveryService {

	var dService taskhandler.DiscoveryService = nil
	var err error = nil
	switch viper.GetString("serviceDiscovery.type") {
	case "consul":
		dService, err = consul.NewDiscoveryService(healthCheck)
	case "etcd":
		dService, err = etcd.NewDiscoveryService(healthCheck)
	case "k8s":
		dService, err = kubernetes.NewDiscoveryService()
	default:
		log.Fatalf("Unsupported discoveryService: %s", viper.GetString("serviceDiscovery.type"))
	}

	if err != nil {
		log.WithError(err).Fatal("Could not create discovery service")
	}
	return dService
}

func CreateModelProvider() cachemanager.ModelProvider {
	var mProvider cachemanager.ModelProvider = nil
	var err error = nil

	switch viper.GetString("modelProvider.type") {
	case "diskProvider":
		mProvider = diskmodelprovider.DiskModelProvider{
			BaseDir: viper.GetString("modelProvider.baseDir"),
		}
	case "s3Provider":
		mProvider, err = s3modelprovider.NewS3ModelProvider(
			viper.GetString("modelProvider.s3.bucket"),
			viper.GetString("modelProvider.s3.basePath"))
	default:
		log.Fatalf("Unsupported discoveryService: %s", viper.GetString("serviceDiscovery.type"))
	}

	if err != nil {
		log.WithError(err).Fatal("Could not create discovery service")
	}
	return mProvider
}

func healthCheck() (bool, error) {
	return true, nil
}
