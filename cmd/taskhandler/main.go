package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
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

	cache, cacheHTTPServer := serveCache()

	taskHandler, proxyHTTPServer, err := serveProxy()
	if err != nil {
		log.WithError(err).Fatal("Could not start proxy")
	}

	// Wait for shutdown signal
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGTERM, syscall.SIGINT)

	// Run health checks until shutdown signal
	healthTicker := time.NewTicker(30 * time.Second)
	defer healthTicker.Stop()

	for {
		select {
		case <-healthTicker.C:
			isHealthy := cache.IsHealthy()
			cache.GrpcProxy.SetHealth(isHealthy)
			if taskHandler != nil {
				taskHandler.GrpcProxy.SetHealth(isHealthy)
			}
		case sig := <-stop:
			log.Infof("Received signal %v, shutting down gracefully...", sig)
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()

			// Mark as unhealthy so k8s stops routing traffic
			cache.GrpcProxy.SetHealth(false)
			if taskHandler != nil {
				taskHandler.GrpcProxy.SetHealth(false)
			}

			// Allow k8s to propagate the unhealthy status before closing connections
			time.Sleep(5 * time.Second)

			if taskHandler != nil {
				if err := taskHandler.Close(); err != nil {
					log.WithError(err).Error("Error closing task handler")
				}
			}
			if err := cache.GrpcProxy.Close(); err != nil {
				log.WithError(err).Error("Error closing cache gRPC proxy")
			}
			if proxyHTTPServer != nil {
				if err := proxyHTTPServer.Shutdown(shutdownCtx); err != nil {
					log.WithError(err).Error("Error shutting down proxy HTTP server")
				}
			}
			if err := cacheHTTPServer.Shutdown(shutdownCtx); err != nil {
				log.WithError(err).Error("Error shutting down cache HTTP server")
			}

			log.Info("Shutdown complete")
			return
		}
	}
}

func serveCache() (*cachemanager.CacheManager, *http.Server) {

	var (
		restPort = viper.GetInt("cacheRestPort")
		grpcPort = viper.GetInt("cacheGrpcPort")
	)

	log.Infof("Cache is ready to handle requests at rest:%v and grpc:%v", restPort, grpcPort)

	cache := CreateCacheManager()

	cacheMux := http.NewServeMux()

	cacheMux.HandleFunc("/v1/models/", cache.ServeRest())
	cacheHTTPServer := &http.Server{Addr: fmt.Sprintf(":%d", restPort), Handler: cacheMux}
	go func() {
		if err := cacheHTTPServer.ListenAndServe(); err != http.ErrServerClosed {
			log.WithError(err).Fatal("Cache HTTP server error")
		}
	}()

	go func() {
		if err := cache.GrpcProxy.Listen(grpcPort); err != nil {
			log.WithError(err).Fatal("Cache gRPC server error")
		}
	}()

	return cache, cacheHTTPServer
}

func serveProxy() (*taskhandler.TaskHandler, *http.Server, error) {

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
			return nil, nil, err
		}

		go func() {
			if err := tHandler.GrpcProxy.Listen(grpcPort); err != nil {
				log.WithError(err).Fatal("Proxy gRPC server error")
			}
		}()

		proxyMux.HandleFunc("/v1/models/", tHandler.ServeRest())

		log.Infof("Proxy is ready to handle requests at rest:%v and grpc:%v", restPort, grpcPort)

	} else {
		log.Info("Proxy is disabled")
	}

	proxyMux.Handle(metricsPath, taskhandler.MetricsHandler(servingRestHost, servingMetricsPath, metricsTimeout))

	log.Infof("Metrics are available at %v:%v", restPort, metricsPath)

	proxyHTTPServer := &http.Server{Addr: fmt.Sprintf(":%d", restPort), Handler: proxyMux}
	go func() {
		if err := proxyHTTPServer.ListenAndServe(); err != http.ErrServerClosed {
			log.WithError(err).Fatal("Proxy HTTP server error")
		}
	}()
	return tHandler, proxyHTTPServer, nil
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
