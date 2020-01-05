package cachemanager

import (
	"errors"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/mKaloer/TFServingCache/pkg/tfservingproxy"
	log "github.com/sirupsen/logrus"
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
	localRestUrl                 url.URL
	ModelProvider                ModelProvider
	LocalCache                   ModelCache
	TFServingServerModelBasePath string
	ServingController            TFServingController
	ModelFetchTimeout            float32 // model fetch timeout in seconds
	rwMux                        sync.RWMutex
}

func (handler *CacheManager) ServeRest() func(http.ResponseWriter, *http.Request) {
	return handler.RestProxy.Serve()
}

func (cache *CacheManager) fetchModel(identifier ModelIdentifier) error {
	_, isPresent := cache.tryGetModelFromCache(identifier)
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
		err = cache.ServingController.ReloadConfig(cache.LocalCache.ListModels(), cache.TFServingServerModelBasePath)
		if err != nil {
			log.WithError(err).Error("Error while loading model")
			return err
		}
		totalTime := float32(0.0)
		for totalTime == 0 || totalTime < cache.ModelFetchTimeout {
			status, err := cache.ServingController.GetModelStatus(model)
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

func New(
	modelProvider ModelProvider,
	modelCache ModelCache,
	tfServingServerBasePath string,
	tfservingServerGRPCHost string,
	tfservingServerRESTHost string,
	modelFetchTimeout float32,
) *CacheManager {
	restUrl, err := url.Parse(tfservingServerRESTHost)
	if err != nil {
		return nil
	}

	servingController := TFServingController{grpcHost: tfservingServerGRPCHost, restHost: tfservingServerRESTHost}

	h := &CacheManager{
		localRestUrl:                 *restUrl,
		ModelProvider:                modelProvider,
		LocalCache:                   modelCache,
		ServingController:            servingController,
		TFServingServerModelBasePath: tfServingServerBasePath,
		ModelFetchTimeout:            modelFetchTimeout,
	}

	director := func(req *http.Request, modelName string, version string) {
		log.Infof("Fetching model...")

		modelVersion, err := strconv.ParseInt(version, 10, 64)
		if err != nil {
			log.WithError(err).Errorf("Error handling request. Version must be valid integer: '%s'", version)
			req.Response.StatusCode = 500
			return
		}
		identifier := ModelIdentifier{ModelName: modelName, Version: modelVersion}
		err = h.fetchModel(identifier)
		if err != nil {
			log.WithError(err).Errorf("Error handling request. Aborting: %s", req.URL.String())
			req.Response.StatusCode = 500
			return
		}
		localUrl := *restUrl
		localUrl.Path = req.URL.Path
		log.Infof("Forwarding to %s", localUrl.String())
		req.URL = &localUrl
		if _, ok := req.Header["User-Agent"]; !ok {
			// explicitly disable User-Agent so it's not set to default value
			req.Header.Set("User-Agent", "")
		}
	}
	h.RestProxy = tfservingproxy.NewRestProxy(director)

	return h
}

func fileOrDirExists(filename string) bool {
	_, err := os.Stat(filename)
	return !os.IsNotExist(err)
}
