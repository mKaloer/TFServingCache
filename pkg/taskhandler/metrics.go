package taskhandler

import (
	"net/http"
	"net/url"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/prometheus/common/expfmt"

	dto "github.com/prometheus/client_model/go"
)

// MetricsHandler returns an http.Handler
func MetricsHandler(metricsHost string, metricsPath string, timeout int) http.Handler {

	target, _ := url.Parse(metricsHost)
	target.Path = metricsPath

	gatherers := prometheus.Gatherers{
		prometheus.DefaultGatherer,
		prometheus.GathererFunc(func() ([]*dto.MetricFamily, error) {

			// assuming that tfserving always returns metrics in plain text format,
			// otherwise, we can enforce a suitable format through Accept header
			httpClient := http.Client{Timeout: time.Second * time.Duration(timeout)}

			resp, err := httpClient.Get(target.String())
			if err != nil {
				return nil, err
			}

			var parser expfmt.TextParser

			parsed, err := parser.TextToMetricFamilies(resp.Body)
			if err != nil {
				return nil, err
			}

			var result []*dto.MetricFamily
			for _, mf := range parsed {
				result = append(result, mf)
			}

			return result, nil
		}),
	}

	return promhttp.InstrumentMetricHandler(
		prometheus.DefaultRegisterer, promhttp.HandlerFor(gatherers, promhttp.HandlerOpts{}),
	)
}
