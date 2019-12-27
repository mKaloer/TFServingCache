package cachemanager

import (
	"container/list"
	log "github.com/sirupsen/logrus"
	"os"
)

type ModelCache interface {
	Put(item ModelIdentifier, model Model)
	Get(item ModelIdentifier) (Model, bool)
	ListModels() []*Model
	EnsureFreeBytes(bytes uint32)
}

type LRUCache struct {
	lruList     *list.List
	modelMap    map[ModelIdentifier]*list.Element
	Capacity    uint32
	currentSize uint32
}

func NewLRUCache(capacityInBytes uint32) LRUCache {
	cache := LRUCache{
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
		cache.EnsureFreeBytes(model.sizeOnDisk)
		newElement := cache.lruList.PushFront(model)
		cache.modelMap[item] = newElement
		cache.currentSize += model.sizeOnDisk
	} else {
		cache.lruList.MoveToFront(existingElement)
	}
}

// Deletes LRU models until number of bytes are available
func (cache *LRUCache) EnsureFreeBytes(bytes uint32) {
	for cache.lruList.Len() > 0 && cache.Capacity-cache.currentSize < bytes {
		lruModelElement := cache.lruList.Back()
		lruModel := lruModelElement.Value.(Model)
		log.Infof("Removing model: %s:%s (%s)", lruModel.identifier.ModelName, lruModel.identifier.Version, lruModel.path)
		if fileExists(lruModel.path) {
			// Delete file
			err := os.Remove(lruModel.path)
			if err != nil {
				log.Fatalf("Could not delete file: %s - %s", lruModel.path, err)
			}
		}
		cache.currentSize -= lruModel.sizeOnDisk
		cache.lruList.Remove(lruModelElement)
		delete(cache.modelMap, lruModel.identifier)
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
