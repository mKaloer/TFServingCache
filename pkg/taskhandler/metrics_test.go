package taskhandler

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

func TestMetricsHandler(t *testing.T) {

	var (
		path           = "/metrics"
		servingCounter = ":tensorflow:core:counter"
		cacheCounter   = "tfservingcache_counter"
	)

	promauto.NewCounter(prometheus.CounterOpts{
		Name: cacheCounter,
	}).Inc()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

		if path != r.URL.Path {
			t.Error("expected", path, "got", r.URL.Path)
		}

		fmt.Fprintf(w, "# TYPE %v counter\n%v 42\n", servingCounter, servingCounter)
	}))
	defer ts.Close()

	handler := MetricsHandler(ts.URL, path, 42)

	req, err := http.NewRequest("GET", path, nil)
	if err != nil {
		t.Fatal(err)
	}

	res := httptest.NewRecorder()

	handler.ServeHTTP(res, req)

	if http.StatusOK != res.Code {
		t.Error("expected", http.StatusOK, "got", res.Code)
	}

	body := res.Body.String()

	if !strings.Contains(body, servingCounter) {
		t.Error("The response does not contain 'serving' metrics")
	}

	if !strings.Contains(body, cacheCounter) {
		t.Error("The response does not contain 'cache' metrics")
	}
}
