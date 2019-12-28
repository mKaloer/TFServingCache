package cachemanager

import (
	"container/list"
	"os"
	"path"

	log "github.com/sirupsen/logrus"
)

type ModelCache interface {
	BaseDir() string
	ModelPath(model Model) string
	Put(item ModelIdentifier, model Model)
	Get(item ModelIdentifier) (Model, bool)
	ListModels() []*Model
	EnsureFreeBytes(bytes int64)
}

type LRUCache struct {
	baseDir     string
	lruList     *list.List
	modelMap    map[ModelIdentifier]*list.Element
	Capacity    int64
	currentSize int64
}

func NewLRUCache(dir string, capacityInBytes int64) LRUCache {
	cache := LRUCache{
		baseDir:     dir,
		lruList:     list.New(),
		modelMap:    map[ModelIdentifier]*list.Element{},
		Capacity:    capacityInBytes,
		currentSize: 0,
	}
	return cache
}

// Retrieves an item from the cache as well as a bool
// indicating whether the item was present or not.
// If the item is not present, the zero value of
// the type is returned.
func (cache *LRUCache) Get(item ModelIdentifier) (Model, bool) {
	val, isContained := cache.modelMap[item]
	if isContained {
		cache.lruList.MoveToFront(val)
		return val.Value.(Model), true
	} else {
		return Model{}, false
	}
}

// Adds an item to the cache (if it does not already exist)
func (cache *LRUCache) Put(item ModelIdentifier, model Model) {
	existingElement, isContained := cache.modelMap[item]
	if !isContained {
		// Cleanup space
		cache.EnsureFreeBytes(model.SizeOnDisk)
		newElement := cache.lruList.PushFront(model)
		cache.modelMap[item] = newElement
		cache.currentSize += model.SizeOnDisk
	} else {
		cache.lruList.MoveToFront(existingElement)
	}
}

// Deletes LRU models until number of bytes are available
func (cache *LRUCache) EnsureFreeBytes(bytes int64) {
	for cache.lruList.Len() > 0 && cache.Capacity-cache.currentSize < bytes {
		lruModelElement := cache.lruList.Back()
		lruModel := lruModelElement.Value.(Model)
		log.Infof("Removing model: %s:%s (%s)", lruModel.Identifier.ModelName, lruModel.Identifier.Version, lruModel.Path)
		if fileOrDirExists(lruModel.Path) {
			// Delete file
			err := os.Remove(lruModel.Path)
			if err != nil {
				log.Fatalf("Could not delete file: %s - %s", lruModel.Path, err)
			}
		}
		cache.currentSize -= lruModel.SizeOnDisk
		cache.lruList.Remove(lruModelElement)
		delete(cache.modelMap, lruModel.Identifier)
	}
	if cache.lruList.Len() > 0 && cache.Capacity-cache.currentSize < bytes {
		log.Errorf("Cannot allocate requested number of bytes. Capacity: %d, request: %d", cache.Capacity, bytes)
	}
}

func (cache *LRUCache) ListModels() []*Model {
	res := []*Model{}
	for e := cache.lruList.Front(); e != nil; e = e.Next() {
		model := e.Value.(Model)
		res = append(res, &model)
	}
	return res

}

func (cache *LRUCache) BaseDir() string {
	return cache.baseDir
}

func (cache *LRUCache) ModelPath(model Model) string {
	return path.Join(cache.baseDir, model.Path)
}
