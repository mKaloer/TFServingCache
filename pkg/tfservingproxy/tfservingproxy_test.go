package tfservingproxy

import (
	"testing"
	"net/http"
	"context"
	log "github.com/sirupsen/logrus"
)

type mockServer struct {
	proxyServer *http.Server
	modelServer *http.Server
}


func setupTestCache(proxyCallback func(modelName string, version string), modelCallback func()) *mockServer {
	handlerMock := func(req *http.Request, modelName string, version string) error {
		proxyCallback(modelName, version)
		req.URL.Path = "http://localhost:8089/foobar"
		req.URL.Scheme = "http"
		req.URL.Host = "localhost:8089"
		return nil
	}
	proxy := NewRestProxy(handlerMock)

	proxyHandler := http.NewServeMux()
	proxyHandler.HandleFunc("/", proxy.Serve())
	

	proxyServer := &http.Server{Addr: ":8088", Handler: proxyHandler}
	go func() {
		if err := proxyServer.ListenAndServe(); err != http.ErrServerClosed {
			log.Fatalf("Err: %v", err)
		}
	}()


	modelServerHandler := http.NewServeMux()

	modelServerHandler.HandleFunc("/", func(http.ResponseWriter, *http.Request) {
		modelCallback()
	})
	modelServer := &http.Server{Addr: ":8089", Handler: modelServerHandler}
	go func() {
		if err := modelServer.ListenAndServe(); err != http.ErrServerClosed {
			log.Fatalf("Err: %v", err)
		}
	}()

	return &mockServer{
		proxyServer,
		modelServer,
	}
}

func (m *mockServer) shutdown() {
	m.proxyServer.Shutdown(context.TODO())
	m.modelServer.Shutdown(context.TODO())
}

func TestProxyParsesUrl(t *testing.T) {
	modelHandlerCalled := false
	proxyCallback := func(modelName string, version string) {
		modelHandlerCalled = true
		if modelName != "foobar" {
			t.Errorf("Wrong model name")
		}
		if version != "42" {
			t.Errorf("Wrong model version")
		}
	}
	modelServerCalled := false
	modelServerCallback := func() {
		modelServerCalled = true
	}
	mockServer := setupTestCache(proxyCallback, modelServerCallback)
	_, err := http.Get("http://localhost:8088/v1/models/foobar/versions/42")
	if err != nil {
		log.Fatalln(err)
	}

	mockServer.shutdown()
	if !modelHandlerCalled {
		t.Errorf("Model handler (proxy) not called")
	}

	if !modelServerCalled {
		t.Errorf("Model server not called")
	}
}


