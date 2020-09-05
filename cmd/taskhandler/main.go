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
	log "github.com/sirupsen/logrus"
	"github.com/spf13/viper"
)

func main() {

	SetConfig()

	cleanup := serveCache()
	defer cleanup()

	serveProxy()

	log.Info("Server stopped")
}

func serveCache() func() error {

	var (
		restPort = viper.GetInt("cacheRestPort")
		grpcPort = viper.GetInt("cacheGrpcPort")
	)

	log.Infof("Cache is ready to handle requests at rest:%v and grpc:%v", restPort, grpcPort)

	cache := CreateCacheManager()

	cacheMux := http.NewServeMux()

	cacheMux.HandleFunc("/v1/models/", cache.ServeRest())
	go http.ListenAndServe(fmt.Sprintf(":%d", restPort), cacheMux)

	go cache.GrpcProxy.Listen(grpcPort)

	return cache.GrpcProxy.Close
}

func serveProxy() {

	var (
		restPort = viper.GetInt("proxyRestPort")
		restHost = viper.GetString("serving.restHost")
		grpcPort = viper.GetInt("proxyGrpcPort")

		metricsPath    = viper.GetString("metrics.path")
		metricsTimeout = viper.GetInt("metrics.timeout")
	)

	proxyMux := http.NewServeMux()

	dService := CreateDiscoveryService()
	if dService != nil {

		tHandler := taskhandler.NewTaskHandler(dService)
		err := tHandler.ConnectToCluster()
		if err != nil {
			log.WithError(err).Fatal("Could not connect to cluster")
		}
		defer tHandler.DisconnectFromCluster()

		go tHandler.GrpcProxy.Listen(grpcPort)
		defer tHandler.GrpcProxy.Close()

		proxyMux.HandleFunc("/v1/models/", tHandler.ServeRest())

		log.Infof("Proxy is ready to handle requests at rest:%v and grpc:%v", restPort, grpcPort)

	} else {
		log.Info("Proxy is disabled")
	}

	proxyMux.Handle(metricsPath, taskhandler.MetricsHandler(restHost, metricsPath, metricsTimeout))

	log.Infof("Metrics is available at %v:%v", restPort, metricsPath)

	http.ListenAndServe(fmt.Sprintf(":%d", restPort), proxyMux)
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

	if viper.IsSet("serviceDiscovery.type") {
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
	}

	return dService
}

func CreateModelProvider() cachemanager.ModelProvider {
	var mProvider cachemanager.ModelProvider = nil
	var err error = nil

	switch viper.GetString("modelProvider.type") {
	case "diskProvider":
		mProvider = diskmodelprovider.DiskModelProvider{
			BaseDir: viper.GetString("modelProvider.diskProvider.baseDir"),
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
	// TODO: Implement a health check. Also expose via http
	return true, nil
}
