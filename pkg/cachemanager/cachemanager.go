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
	identifier ModelIdentifier
	path       string
	sizeOnDisk uint32
}

type ModelIdentifier struct {
	ModelName string
	Version   string
}

type CacheManager struct {
	RestProxy     *tfservingproxy.RestProxy
	localRestUrl  url.URL
	ModelProvider ModelProvider
	LocalCache    ModelCache
	rwMux         sync.RWMutex
}

func (handler *CacheManager) ServeRest() func(http.ResponseWriter, *http.Request) {
	return handler.RestProxy.Serve()
}

func (cache *CacheManager) fetchModel(identifier ModelIdentifier) {
	_, isPresent := cache.tryGetModelFromCache(identifier)
	if !isPresent {
		// Model does not exist - get size, then put in cache
		cache.rwMux.Lock()
		defer cache.rwMux.Unlock()
		modelSize := cache.ModelProvider.ModelSize(identifier.ModelName, identifier.Version)
		cache.LocalCache.EnsureFreeBytes(modelSize)
		model := cache.ModelProvider.FetchModel(identifier.ModelName, identifier.Version)
		cache.LocalCache.Put(identifier, model)
		ReloadConfig(cache.LocalCache.ListModels())
	}
}

func (cache *CacheManager) tryGetModelFromCache(identifier ModelIdentifier) (Model, bool) {
	cache.rwMux.RLock()
	defer cache.rwMux.RUnlock()
	model, isPresent := cache.LocalCache.Get(identifier)
	fileExists := isPresent && fileExists(model.path)
	if isPresent && !fileExists {
		log.Warnf("Model in cache but not present on disk. Name: %s, Version: %s, path: %s",
			identifier.ModelName, identifier.Version, model.path)
	}
	return model, fileExists
}

func New(localRestUrl string, modelProvider ModelProvider, modelCache ModelCache) *CacheManager {
	restUrl, err := url.Parse(localRestUrl)
	if err != nil {
		return nil
	}
	h := &CacheManager{
		localRestUrl:  *restUrl,
		ModelProvider: modelProvider,
		LocalCache:    modelCache,
	}

	director := func(req *http.Request, modelName string, version string) {
		log.Infof("Fetching model...")
		identifier := ModelIdentifier{ModelName: modelName,
			Version: version}
		h.fetchModel(identifier)
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

func fileExists(filename string) bool {
	info, err := os.Stat(filename)
	if os.IsNotExist(err) {
		return false
	}
	return !info.IsDir()
}
