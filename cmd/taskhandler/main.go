package main

import (
	"fmt"
	"net/http"
	"time"

	"github.com/mKaloer/TFServingCache/pkg/cachemanager"
	"github.com/mKaloer/TFServingCache/pkg/cachemanager/modelproviders/azblobmodelprovider"
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

	cache := serveCache()
	defer cache.GrpcProxy.Close()

	taskHandler, err := serveProxy()
	if err != nil {
		log.WithError(err).Fatal("Could not start proxy")
	}
	if taskHandler != nil {
		defer taskHandler.Close()
	}
	// Run health checks
	for {
		isHealthy := cache.IsHealthy()
		cache.GrpcProxy.SetHealth(isHealthy)
		if taskHandler != nil {
			taskHandler.GrpcProxy.SetHealth(isHealthy)
		}
		time.Sleep(time.Second * 30)
	}
}

func serveCache() *cachemanager.CacheManager {

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

	return cache
}

func serveProxy() (*taskhandler.TaskHandler, error) {

	var (
		restPort = viper.GetInt("proxyRestPort")
		grpcPort = viper.GetInt("proxyGrpcPort")

		servingRestHost = viper.GetString("serving.restHost")

		metricsPath    = viper.GetString("metrics.path")
		metricsTimeout = viper.GetInt("metrics.timeout")

		servingMetricsPath = metricsPath
	)

	if viper.IsSet("serving.metricsPath") {
		servingMetricsPath = viper.GetString("serving.metricsPath")
	}

	proxyMux := http.NewServeMux()

	dService := CreateDiscoveryService()
	var tHandler *taskhandler.TaskHandler
	if dService != nil {

		tHandler = taskhandler.NewTaskHandler(dService)
		err := tHandler.ConnectToCluster()
		if err != nil {
			log.WithError(err).Fatal("Could not connect to cluster")
			return nil, err
		}

		go tHandler.GrpcProxy.Listen(grpcPort)

		proxyMux.HandleFunc("/v1/models/", tHandler.ServeRest())

		log.Infof("Proxy is ready to handle requests at rest:%v and grpc:%v", restPort, grpcPort)

	} else {
		log.Info("Proxy is disabled")
	}

	proxyMux.Handle(metricsPath, taskhandler.MetricsHandler(servingRestHost, servingMetricsPath, metricsTimeout))

	log.Infof("Metrics are available at %v:%v", restPort, metricsPath)

	go http.ListenAndServe(fmt.Sprintf(":%d", restPort), proxyMux)
	return tHandler, nil
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
			dService, err = consul.NewDiscoveryService(isHealthy)
		case "etcd":
			dService, err = etcd.NewDiscoveryService(isHealthy)
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
	case "azBlobProvider":
		if viper.IsSet("modelProvider.azBlob.containerUrl") {
			mProvider, err = azblobmodelprovider.NewAZBlobModelProviderWithUrl(
				viper.GetString("modelProvider.azBlob.containerUrl"),
				viper.GetString("modelProvider.azBlob.basePath"),
				viper.GetString("modelProvider.azBlob.accountName"),
				viper.GetString("modelProvider.azBlob.accountKey"))
		} else {
			mProvider, err = azblobmodelprovider.NewAZBlobModelProvider(
				viper.GetString("modelProvider.azBlob.container"),
				viper.GetString("modelProvider.azBlob.basePath"),
				viper.GetString("modelProvider.azBlob.accountName"),
				viper.GetString("modelProvider.azBlob.accountKey"))
		}
	default:
		log.Fatalf("Unsupported discoveryService: %s", viper.GetString("serviceDiscovery.type"))
	}

	if err != nil {
		log.WithError(err).Fatal("Could not create discovery service")
	}
	return mProvider
}

func isHealthy() (bool, error) {
	// TODO: Implement a health check. Also expose via http
	return true, nil
}
