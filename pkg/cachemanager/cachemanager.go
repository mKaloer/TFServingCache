package cachemanager

import (
	"net/http"
	"net/url"
	"os"
	"sync"

	"github.com/mKaloer/tfservingcache/pkg/tfservingproxy"
	log "github.com/sirupsen/logrus"
)

type Model struct {
	Identifier ModelIdentifier
	Path       string
	SizeOnDisk int64
}

type ModelIdentifier struct {
	ModelName string
	Version   string
}

type CacheManager struct {
	RestProxy                    *tfservingproxy.RestProxy
	localRestUrl                 url.URL
	ModelProvider                ModelProvider
	LocalCache                   ModelCache
	TFServingServerModelBasePath string
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
		ReloadConfig(cache.LocalCache.ListModels(), cache.TFServingServerModelBasePath)
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
		log.Warnf("Model in cache but not present on disk. Name: %s, Version: %s, path: %s",
			identifier.ModelName, identifier.Version, hostModelPath)
	}
	return model, fileExists
}

func New(localRestUrl string, modelProvider ModelProvider, modelCache ModelCache, tfServingServerBasePath string) *CacheManager {
	restUrl, err := url.Parse(localRestUrl)
	if err != nil {
		return nil
	}

	h := &CacheManager{
		localRestUrl:                 *restUrl,
		ModelProvider:                modelProvider,
		LocalCache:                   modelCache,
		TFServingServerModelBasePath: tfServingServerBasePath,
	}

	director := func(req *http.Request, modelName string, version string) {
		log.Infof("Fetching model...")
		identifier := ModelIdentifier{ModelName: modelName,
			Version: version}
		err := h.fetchModel(identifier)
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
