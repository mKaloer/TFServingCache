package tfservingproxy

import (
	"encoding/json"
	"net/http"
	"net/http/httputil"
	"regexp"

	log "github.com/sirupsen/logrus"
)

var tfServingRestURLMatch = regexp.MustCompile(`(?i)^/v1/models/(?P<modelName>[a-z0-9]+)(/versions/(?P<version>[0-9]+))?`)

type RestProxy struct {
	RestProxy *httputil.ReverseProxy
}

func (handler *RestProxy) Serve() func(http.ResponseWriter, *http.Request) {
	// Wrap proxy in custom function to check for invalid requests
	proxyFun := func(rw http.ResponseWriter, req *http.Request) {
		log.Debugf("Handling URL: %s", req.URL.String())
		matches := tfServingRestURLMatch.FindStringSubmatch(req.URL.String())
		log.Debugf("Model name: '%s' Version: '%s'", matches[1], matches[3])
		if matches[3] == "" {
			rw.Header().Set("Content-Type", "application/json")
			rw.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(rw).Encode(struct {
				Status  string
				Message string
			}{
				Status:  "Error",
				Message: "Model version must be provided",
			})
			return
		}
		handler.RestProxy.ServeHTTP(rw, req)
	}
	return proxyFun
}

type InvalidRequestError struct {
	Url     string
	message string
}

func NewRestProxy(handler func(req *http.Request, modelName string, version string)) *RestProxy {
	director := func(req *http.Request) {
		log.Debugf("Handling URL: %s", req.URL.String())
		matches := tfServingRestURLMatch.FindStringSubmatch(req.URL.String())
		log.Debugf("Model name: '%s' Version: '%s'", matches[1], matches[3])
		handler(req, matches[1], matches[3])
	}
	h := &RestProxy{
		RestProxy: &httputil.ReverseProxy{Director: director},
	}

	return h
}
