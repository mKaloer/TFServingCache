package cachemanager

import (
	"errors"
	"math"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/mKaloer/TFServingCache/pkg/tfservingproxy"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/viper"
	"google.golang.org/grpc"
	"google.golang.org/grpc/backoff"
)

type Model struct {
	Identifier ModelIdentifier
	Path       string
	SizeOnDisk int64
}

type ModelIdentifier struct {
	ModelName string
	Version   int64
}

type CacheManager struct {
	RestProxy                    *tfservingproxy.RestProxy
	GrpcProxy                    *tfservingproxy.GrpcProxy
	localRestURL                 url.URL
	localGrpcURL                 string
	localGrpcConnection          *grpc.ClientConn
	ModelProvider                ModelProvider
	LocalCache                   ModelCache
	MaxConcurrentModels          int
	TFServingServerModelBasePath string
	ServingController            *TFServingController
	ModelFetchTimeout            float32 // model fetch timeout in seconds
	rwMux                        sync.RWMutex
}

func (handler *CacheManager) ServeRest() func(http.ResponseWriter, *http.Request) {
	return handler.RestProxy.Serve()
}

func (cache *CacheManager) fetchModel(identifier ModelIdentifier) error {
	model, isPresent := cache.tryGetModelFromCache(identifier)
	if !isPresent {
		// Model does not exist - get size, then put in cache
		cache.rwMux.Lock()
		defer cache.rwMux.Unlock()
		modelSize, err := cache.ModelProvider.ModelSize(identifier.ModelName, identifier.Version)
		if err != nil {
			log.WithError(err).Error("Error while retrieving model size")
			return err
		}
		cache.LocalCache.EnsureFreeBytes(modelSize)
		model, err := cache.ModelProvider.LoadModel(identifier.ModelName, identifier.Version, cache.LocalCache.BaseDir())
		if err != nil {
			log.WithError(err).Error("Error while retrieving model")
			return err
		}
		cache.LocalCache.Put(identifier, model)
		cache.reloadServingConfig(model)
	} else if state, err := cache.ServingController.GetModelStatus(model); err != nil ||
		state == ModelVersionStatus_UNLOADING ||
		state == ModelVersionStatus_END {
		// Model in disk cache but not loaded in serving
		cache.rwMux.Lock()
		defer cache.rwMux.Unlock()
		cache.reloadServingConfig(model)
	}
	return nil
}

func (cache *CacheManager) tryGetModelFromCache(identifier ModelIdentifier) (Model, bool) {
	cache.rwMux.RLock()
	defer cache.rwMux.RUnlock()
	model, isPresent := cache.LocalCache.Get(identifier)
	hostModelPath := cache.LocalCache.ModelPath(model)
	fileExists := isPresent && fileOrDirExists(hostModelPath)
	if isPresent && !fileExists {
		log.Warnf("Model in cache but not present on disk. Name: %s, Version: %d, path: %s",
			identifier.ModelName, identifier.Version, hostModelPath)
	}
	return model, fileExists
}

func (cache *CacheManager) reloadServingConfig(requestedModel Model) error {
	availableModels := cache.LocalCache.ListModels()
	numActiveModels := int(math.Min(float64(len(availableModels)), float64(cache.MaxConcurrentModels)))
	err := cache.ServingController.ReloadConfig(availableModels[:numActiveModels], cache.TFServingServerModelBasePath)
	if err != nil {
		log.WithError(err).Error("Error while loading model")
		return err
	}
	totalTime := float32(0.0)
	for totalTime == 0 || totalTime < cache.ModelFetchTimeout {
		status, err := cache.ServingController.GetModelStatus(requestedModel)
		if err != nil {
			log.WithError(err).Errorf("Error getting model status. Duration: %fs", totalTime)
		} else if status == ModelVersionStatus_AVAILABLE {
			log.Info("Model available")
			break
		} else {
			log.Debugf("Model not yet available: %s. Duration: %fs", status.String(), totalTime)
		}
		totalTime += 0.5
		time.Sleep(time.Millisecond * 500)
	}
	if totalTime >= cache.ModelFetchTimeout {
		return errors.New("Timeout: Model did not load in time")
	}
	return nil
}

func New(
	modelProvider ModelProvider,
	modelCache ModelCache,
	tfServingServerBasePath string,
	tfservingServerGRPCHost string,
	tfservingServerRESTHost string,
	modelFetchTimeout float32,
	maxConcurrentModels int,
) *CacheManager {
	restUrl, err := url.Parse(tfservingServerRESTHost)
	if err != nil {
		return nil
	}

	servingController, err := NewTFServingController(tfservingServerGRPCHost, tfservingServerRESTHost)
	if err != nil {
		return nil
	}

	h := &CacheManager{
		localRestURL:                 *restUrl,
		localGrpcURL:                 tfservingServerGRPCHost,
		ModelProvider:                modelProvider,
		LocalCache:                   modelCache,
		ServingController:            servingController,
		TFServingServerModelBasePath: tfServingServerBasePath,
		ModelFetchTimeout:            modelFetchTimeout,
		MaxConcurrentModels:          maxConcurrentModels,
	}
	h.RestProxy = tfservingproxy.NewRestProxy(h.restDirector)
	h.GrpcProxy = tfservingproxy.NewGrpcProxy(h.grpcDirector)

	// Create new grpc client
	localConn, err := grpc.Dial(h.localGrpcURL,
		grpc.WithInsecure(),
		grpc.WithTimeout(viper.GetDuration("proxy.grpcTimeout")*time.Second),
		grpc.WithConnectParams(grpc.ConnectParams{Backoff: backoff.DefaultConfig}))

	if err != nil {
		log.WithError(err).Error("Could not create grpc connection to tfserving")
		return nil
	}
	h.localGrpcConnection = localConn

	return h
}

func fileOrDirExists(filename string) bool {
	_, err := os.Stat(filename)
	return !os.IsNotExist(err)
}

func (cache *CacheManager) restDirector(req *http.Request, modelName string, version string) {
	err := cache.handleModelRequest(modelName, version)
	if err != nil {
		log.WithError(err).Errorf("Error handling request. Aborting: %s", req.URL.String())
		req.Response.StatusCode = 500
		return
	}
	localURL := cache.localRestURL
	localURL.Path = req.URL.Path
	log.Infof("Forwarding to %s", localURL.String())
	req.URL = &localURL
	if _, ok := req.Header["User-Agent"]; !ok {
		// explicitly disable User-Agent so it's not set to default value
		req.Header.Set("User-Agent", "")
	}
}

func (cache *CacheManager) grpcDirector(modelName string, version string) (*grpc.ClientConn, error) {
	err := cache.handleModelRequest(modelName, version)
	if err != nil {
		log.WithError(err).Errorf("Error handling request")
		return nil, err
	}
	return cache.localGrpcConnection, nil
}

func (cache *CacheManager) handleModelRequest(modelName string, version string) error {
	log.Infof("Handling request: %s:%s", modelName, version)

	modelVersion, err := strconv.ParseInt(version, 10, 64)
	if err != nil {
		log.WithError(err).Errorf("Error handling request. Version must be valid integer: '%s'", version)
		return err
	}
	identifier := ModelIdentifier{ModelName: modelName, Version: modelVersion}
	err = cache.fetchModel(identifier)
	if err != nil {
		log.WithError(err).Errorf("Error handling request.")
		return err
	}
	return nil
}

func (cache *CacheManager) Close() error {
	err1 := cache.ServingController.Close()
	if err1 != nil {
		log.WithError(err1).Error("Could not close TF serving controller")
	}
	err2 := cache.localGrpcConnection.Close()
	if err2 != nil {
		log.WithError(err2).Error("Could not close local TF serving connection")
		return err2
	}
	return err1
}
