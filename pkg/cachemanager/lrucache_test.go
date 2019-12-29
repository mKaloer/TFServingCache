package cachemanager

import (
	"testing"
)

func TestCacheAddGet(t *testing.T) {
	cache := NewLRUCache("./cache", 1024)
	identifier := ModelIdentifier{ModelName: "foo", Version: 42}
	m := Model{Identifier: identifier, Path: "/some/path", SizeOnDisk: 10}
	cache.Put(identifier, m)
	m2, avail := cache.Get(identifier)
	if !avail {
		t.Errorf("Cache item not available")
	}
	if m2.Path == "" {
		t.Errorf("Cached model path not set")
	}
	if m2.Identifier.ModelName != identifier.ModelName || m2.Identifier.Version != identifier.Version {
		t.Errorf("Wrong model identifier")
	}
	if m2.SizeOnDisk != 10 {
		t.Errorf("Wrong size on disk")
	}
}

func TestCacheGetNotPresent(t *testing.T) {
	cache := NewLRUCache("./cache", 1024)
	identifier := ModelIdentifier{ModelName: "foo", Version: 42}
	_, avail := cache.Get(identifier)
	if avail {
		t.Errorf("Cache item available even though it has not been added")
	}
}

func TestCacheRemovesLRUSeqAccess(t *testing.T) {
	cache := NewLRUCache("./cache", 95)
	for i := 1; i <= 10; i++ {
		identifier := ModelIdentifier{ModelName: "foo", Version: int64(i)}
		m := Model{Identifier: identifier, Path: "/some/path", SizeOnDisk: 10}
		cache.Put(identifier, m)
	}

	lruIdentifier := ModelIdentifier{ModelName: "foo", Version: 1}
	secondlruIdentifier := ModelIdentifier{ModelName: "foo", Version: 2}
	_, avail1 := cache.Get(lruIdentifier)
	_, avail2 := cache.Get(secondlruIdentifier)
	if avail1 {
		t.Errorf("Cache item available even though it should have been removed")
	}
	if !avail2 {
		t.Errorf("Second LRU not available even though it should be")
	}
	if cache.currentSize != 90 {
		t.Errorf("Expectec cache size of %d but was %d", 90, cache.currentSize)
	}
}

func TestCacheRemovesLRUNonSeqAccess(t *testing.T) {
	cache := NewLRUCache("./cache", 100)
	for i := 1; i <= 10; i++ {
		identifier := ModelIdentifier{ModelName: "foo", Version: int64(i)}
		m := Model{Identifier: identifier, Path: "/some/path", SizeOnDisk: 10}
		cache.Put(identifier, m)
	}

	lruIdentifier := ModelIdentifier{ModelName: "foo", Version: 1}
	secondlruIdentifier := ModelIdentifier{ModelName: "foo", Version: 2}
	_, avail1 := cache.Get(lruIdentifier)

	identifier := ModelIdentifier{ModelName: "foo", Version: 11}
	m := Model{Identifier: identifier, Path: "/some/path", SizeOnDisk: 10}
	cache.Put(identifier, m)
	_, avail1 = cache.Get(lruIdentifier)
	_, avail2 := cache.Get(secondlruIdentifier)
	if !avail1 {
		t.Errorf("Cache item 1 not available even though it should have been")
	}
	if avail2 {
		t.Errorf("Second LRU is available even though it should not be")
	}
}

func TestCacheRemovesLRUVarSizes(t *testing.T) {
	cache := NewLRUCache("./cache", 100)
	for i := 4; i >= 1; i-- {
		identifier := ModelIdentifier{ModelName: "foo", Version: int64(i)}
		m := Model{Identifier: identifier, Path: "/some/path", SizeOnDisk: int64(10 * i)}
		cache.Put(identifier, m)
	}

	// adding 2 of size 20 should remove 1
	identifier2 := ModelIdentifier{ModelName: "foo", Version: 5}
	m2 := Model{Identifier: identifier2, Path: "/some/path", SizeOnDisk: 20}
	cache.Put(identifier2, m2)

	_, avail1 := cache.Get(ModelIdentifier{ModelName: "foo", Version: 4})
	if avail1 {
		t.Errorf("Expected LRU model to be removed, but is is not")
	}
	if cache.currentSize != 80 {
		t.Errorf("Expected cache to be of size 80, but it is %d", cache.currentSize)
	}
	if len(cache.ListModels()) != 4 {
		t.Errorf("Expected number of cache items to be 4, but it is %d", len(cache.ListModels()))
	}

	identifier3 := ModelIdentifier{ModelName: "foo", Version: 6}
	m3 := Model{Identifier: identifier2, Path: "/some/path", SizeOnDisk: 20}
	cache.Put(identifier3, m3)

	if len(cache.ListModels()) != 5 {
		t.Errorf("Expected number of cache items to be 5, but it is %d", len(cache.ListModels()))
	}

}
