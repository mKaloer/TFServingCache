package cachemanager

import (
	"errors"
	"os"
	"path"
	"strconv"
	"sync"
	"testing"
	"time"
)

// fakeServingController stands in for a TF Serving instance. Model state is
// scripted per call so tests can reproduce a model that is still loading.
type fakeServingController struct {
	mu sync.Mutex

	// statusFn returns the state reported for the n'th GetModelStatus call,
	// counting from 1. A nil statusFn always reports AVAILABLE.
	statusFn func(call int) (ModelVersionStatus_State, error)
	// reloadFn is consulted for the n'th ReloadConfig call, counting from 1.
	// A nil reloadFn always succeeds.
	reloadFn func(call int) error

	statusCalls int
	reloadCalls int
	lastState   ModelVersionStatus_State
}

func (f *fakeServingController) GetModelStatus(model Model) (ModelVersionStatus_State, error) {
	f.mu.Lock()
	f.statusCalls++
	call := f.statusCalls
	statusFn := f.statusFn
	f.mu.Unlock()

	state, err := ModelVersionStatus_AVAILABLE, error(nil)
	if statusFn != nil {
		state, err = statusFn(call)
	}

	f.mu.Lock()
	f.lastState = state
	f.mu.Unlock()
	return state, err
}

func (f *fakeServingController) ReloadConfig(models []*Model, tfServingServerModelDir string) error {
	f.mu.Lock()
	f.reloadCalls++
	call := f.reloadCalls
	reloadFn := f.reloadFn
	f.mu.Unlock()

	if reloadFn == nil {
		return nil
	}
	return reloadFn(call)
}

func (f *fakeServingController) Close() error { return nil }

func (f *fakeServingController) counts() (statusCalls int, reloadCalls int, lastState ModelVersionStatus_State) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.statusCalls, f.reloadCalls, f.lastState
}

// fakeModelProvider writes an empty model directory instead of downloading one.
type fakeModelProvider struct {
	mu sync.Mutex

	size      int64
	loadCalls int
}

func (p *fakeModelProvider) ModelSize(modelName string, modelVersion int64) (int64, error) {
	return p.size, nil
}

func (p *fakeModelProvider) LoadModel(modelName string, modelVersion int64, destinationDir string) (*Model, error) {
	p.mu.Lock()
	p.loadCalls++
	p.mu.Unlock()

	relPath := path.Join(modelName, strconv.FormatInt(modelVersion, 10))
	if err := os.MkdirAll(path.Join(destinationDir, relPath), 0o755); err != nil {
		return nil, err
	}
	return &Model{
		Identifier: ModelIdentifier{ModelName: modelName, Version: modelVersion},
		Path:       relPath,
		SizeOnDisk: p.size,
	}, nil
}

func (p *fakeModelProvider) Check() bool { return true }

func (p *fakeModelProvider) calls() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.loadCalls
}

func newTestCacheManager(t *testing.T, controller ServingControl, provider ModelProvider) *CacheManager {
	t.Helper()
	lru := NewLRUCache(t.TempDir(), 1<<30)
	return &CacheManager{
		ModelProvider:                provider,
		LocalCache:                   &lru,
		ServingController:            controller,
		MaxConcurrentModels:          8,
		ModelFetchTimeout:            2,
		TFServingServerModelBasePath: "/models",
	}
}

// seedCachedModel puts a model in the local cache and creates its directory on
// disk, as a completed download would.
func seedCachedModel(t *testing.T, cache *CacheManager, id ModelIdentifier) Model {
	t.Helper()
	model := Model{
		Identifier: id,
		Path:       path.Join(id.ModelName, strconv.FormatInt(id.Version, 10)),
		SizeOnDisk: 1024,
	}
	if err := os.MkdirAll(path.Join(cache.LocalCache.BaseDir(), model.Path), 0o755); err != nil {
		t.Fatalf("could not create model directory: %v", err)
	}
	cache.LocalCache.Put(id, model)
	return model
}

func statesThenAvailable(loadingCalls int) func(call int) (ModelVersionStatus_State, error) {
	return func(call int) (ModelVersionStatus_State, error) {
		if call <= loadingCalls {
			return ModelVersionStatus_LOADING, nil
		}
		return ModelVersionStatus_AVAILABLE, nil
	}
}

func TestFetchModel_StillLoadingModelIsNotReportedAsReady(t *testing.T) {
	controller := &fakeServingController{statusFn: statesThenAvailable(2)}
	cache := newTestCacheManager(t, controller, &fakeModelProvider{size: 1024})
	id := ModelIdentifier{ModelName: "some-model", Version: 83}
	seedCachedModel(t, cache, id)

	if err := cache.fetchModel(id); err != nil {
		t.Fatalf("fetchModel returned an error: %v", err)
	}

	_, _, lastState := controller.counts()
	if lastState != ModelVersionStatus_AVAILABLE {
		t.Fatalf("fetchModel reported the model ready while TF Serving state was %s; "+
			"forwarding now fails with 'Servable not found'", lastState.String())
	}
}

func TestFetchModel_ModelThatNeverBecomesAvailableReturnsError(t *testing.T) {
	controller := &fakeServingController{
		statusFn: func(call int) (ModelVersionStatus_State, error) {
			return ModelVersionStatus_LOADING, nil
		},
	}
	cache := newTestCacheManager(t, controller, &fakeModelProvider{size: 1024})
	cache.ModelFetchTimeout = 1
	id := ModelIdentifier{ModelName: "some-model", Version: 83}
	seedCachedModel(t, cache, id)

	if err := cache.fetchModel(id); err == nil {
		t.Fatal("fetchModel reported success for a model stuck in LOADING")
	}
}

func TestFetchModel_StartingModelIsNotReportedAsReady(t *testing.T) {
	controller := &fakeServingController{
		statusFn: func(call int) (ModelVersionStatus_State, error) {
			if call <= 1 {
				return ModelVersionStatus_START, nil
			}
			return ModelVersionStatus_AVAILABLE, nil
		},
	}
	cache := newTestCacheManager(t, controller, &fakeModelProvider{size: 1024})
	id := ModelIdentifier{ModelName: "some-model", Version: 83}
	seedCachedModel(t, cache, id)

	if err := cache.fetchModel(id); err != nil {
		t.Fatalf("fetchModel returned an error: %v", err)
	}

	if _, _, lastState := controller.counts(); lastState != ModelVersionStatus_AVAILABLE {
		t.Fatalf("fetchModel reported the model ready while TF Serving state was %s", lastState.String())
	}
}

func TestFetchModel_UnknownStateIsNotReportedAsReady(t *testing.T) {
	available := false
	controller := &fakeServingController{
		statusFn: func(call int) (ModelVersionStatus_State, error) {
			if available {
				return ModelVersionStatus_AVAILABLE, nil
			}
			return ModelVersionStatus_UNKNOWN, nil
		},
		reloadFn: func(call int) error {
			available = true
			return nil
		},
	}
	cache := newTestCacheManager(t, controller, &fakeModelProvider{size: 1024})
	id := ModelIdentifier{ModelName: "some-model", Version: 83}
	seedCachedModel(t, cache, id)

	if err := cache.fetchModel(id); err != nil {
		t.Fatalf("fetchModel returned an error: %v", err)
	}

	_, reloadCalls, lastState := controller.counts()
	if reloadCalls != 1 {
		t.Errorf("serving config pushed %d time(s) for a model in UNKNOWN state, want 1", reloadCalls)
	}
	if lastState != ModelVersionStatus_AVAILABLE {
		t.Errorf("fetchModel reported the model ready while TF Serving state was %s", lastState.String())
	}
}

func TestFetchModel_RequestAfterFailedLoadWaitsForAvailability(t *testing.T) {
	// The first config push fails, leaving the model on disk while TF Serving
	// is still loading it.
	controller := &fakeServingController{
		reloadFn: func(call int) error {
			if call == 1 {
				return errors.New("config push failed")
			}
			return nil
		},
		statusFn: statesThenAvailable(2),
	}
	cache := newTestCacheManager(t, controller, &fakeModelProvider{size: 1024})
	id := ModelIdentifier{ModelName: "some-model", Version: 83}

	if err := cache.fetchModel(id); err == nil {
		t.Fatal("expected the first fetchModel to fail when the config push fails")
	}

	if err := cache.fetchModel(id); err != nil {
		t.Fatalf("second fetchModel returned an error: %v", err)
	}

	if _, _, lastState := controller.counts(); lastState != ModelVersionStatus_AVAILABLE {
		t.Fatalf("the retry reported the model ready while TF Serving state was %s", lastState.String())
	}
}

func TestFetchModel_AvailableModelIsServedWithoutReloadingConfig(t *testing.T) {
	controller := &fakeServingController{}
	cache := newTestCacheManager(t, controller, &fakeModelProvider{size: 1024})
	id := ModelIdentifier{ModelName: "some-model", Version: 83}
	seedCachedModel(t, cache, id)

	if err := cache.fetchModel(id); err != nil {
		t.Fatalf("fetchModel returned an error: %v", err)
	}

	if _, reloadCalls, _ := controller.counts(); reloadCalls != 0 {
		t.Fatalf("a cache hit pushed the serving config %d time(s); this unloads other models", reloadCalls)
	}
}

func TestEnsureServable_PushesTheConfigWhenServingCannotServeTheModel(t *testing.T) {
	tests := []struct {
		name  string
		state ModelVersionStatus_State
		err   error
	}{
		{name: "unknown", state: ModelVersionStatus_UNKNOWN},
		{name: "unloading", state: ModelVersionStatus_UNLOADING},
		{name: "end", state: ModelVersionStatus_END},
		{name: "not in serving config", err: errors.New("Model not found")},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			available := false
			controller := &fakeServingController{
				statusFn: func(call int) (ModelVersionStatus_State, error) {
					if available {
						return ModelVersionStatus_AVAILABLE, nil
					}
					return tc.state, tc.err
				},
				reloadFn: func(call int) error {
					available = true
					return nil
				},
			}
			cache := newTestCacheManager(t, controller, &fakeModelProvider{size: 1024})
			id := ModelIdentifier{ModelName: "some-model", Version: 83}
			model := seedCachedModel(t, cache, id)

			if err := cache.ensureServable(model); err != nil {
				t.Fatalf("ensureServable returned an error: %v", err)
			}

			if _, reloadCalls, _ := controller.counts(); reloadCalls != 1 {
				t.Errorf("serving config pushed %d time(s), want 1", reloadCalls)
			}
		})
	}
}

func TestEnsureServable_WaitsForAnInFlightLoadInsteadOfPushingAgain(t *testing.T) {
	for _, state := range []ModelVersionStatus_State{ModelVersionStatus_START, ModelVersionStatus_LOADING} {
		t.Run(state.String(), func(t *testing.T) {
			inFlight := state
			controller := &fakeServingController{
				statusFn: func(call int) (ModelVersionStatus_State, error) {
					if call <= 2 {
						return inFlight, nil
					}
					return ModelVersionStatus_AVAILABLE, nil
				},
			}
			cache := newTestCacheManager(t, controller, &fakeModelProvider{size: 1024})
			id := ModelIdentifier{ModelName: "some-model", Version: 83}
			model := seedCachedModel(t, cache, id)

			if err := cache.ensureServable(model); err != nil {
				t.Fatalf("ensureServable returned an error: %v", err)
			}

			_, reloadCalls, lastState := controller.counts()
			if reloadCalls != 0 {
				t.Errorf("serving config pushed %d time(s) for a model already loading", reloadCalls)
			}
			if lastState != ModelVersionStatus_AVAILABLE {
				t.Errorf("returned while TF Serving state was %s", lastState.String())
			}
		})
	}
}

func TestFetchModel_ConcurrentRequestsDownloadOnceAndAllSeeAnAvailableModel(t *testing.T) {
	const requests = 8

	available := false
	controller := &fakeServingController{
		statusFn: func(call int) (ModelVersionStatus_State, error) {
			if available {
				return ModelVersionStatus_AVAILABLE, nil
			}
			return ModelVersionStatus_END, nil
		},
		reloadFn: func(call int) error {
			available = true
			return nil
		},
	}
	provider := &fakeModelProvider{size: 1024}
	cache := newTestCacheManager(t, controller, provider)
	id := ModelIdentifier{ModelName: "some-model", Version: 83}

	var wg sync.WaitGroup
	errs := make([]error, requests)
	start := make(chan struct{})
	for i := 0; i < requests; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			errs[i] = cache.fetchModel(id)
		}(i)
	}
	close(start)
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("request %d failed: %v", i, err)
		}
	}
	if _, _, lastState := controller.counts(); lastState != ModelVersionStatus_AVAILABLE {
		t.Errorf("final TF Serving state is %s, want AVAILABLE", lastState.String())
	}
}

func TestFetchModel_TimeoutErrorMentionsTheBudget(t *testing.T) {
	controller := &fakeServingController{
		statusFn: func(call int) (ModelVersionStatus_State, error) {
			return ModelVersionStatus_LOADING, nil
		},
	}
	cache := newTestCacheManager(t, controller, &fakeModelProvider{size: 1024})
	cache.ModelFetchTimeout = 1
	id := ModelIdentifier{ModelName: "some-model", Version: 83}
	seedCachedModel(t, cache, id)

	start := time.Now()
	err := cache.fetchModel(id)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected a timeout error")
	}
	if elapsed > 2*time.Second {
		t.Errorf("fetchModel waited %s for a 1s fetch timeout", elapsed)
	}
}

func TestFetchModel_ConcurrentCacheHitsDoNotRaceOnTheLRUList(t *testing.T) {
	// LRUCache.Get moves the entry to the front of the list, so a cache
	// lookup is a write even though it reads like a read.
	controller := &fakeServingController{}
	cache := newTestCacheManager(t, controller, &fakeModelProvider{size: 1024})
	// Several entries, so Get actually relinks the list instead of
	// early-returning on an element that is already at the front.
	var ids []ModelIdentifier
	for v := int64(1); v <= 4; v++ {
		id := ModelIdentifier{ModelName: "some-model", Version: v}
		seedCachedModel(t, cache, id)
		ids = append(ids, id)
	}

	const readers = 16
	var wg sync.WaitGroup
	errs := make([]error, readers)
	start := make(chan struct{})
	for i := 0; i < readers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			errs[i] = cache.fetchModel(ids[i%len(ids)])
		}(i)
	}
	close(start)
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("cache hit %d failed: %v", i, err)
		}
	}
}
