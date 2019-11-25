package taskhandler

import (
	"encoding/json"
	log "github.com/sirupsen/logrus"
	"math/rand"
	"net/http"
	"net/http/httputil"
	"net/url"
)

type TaskHandler struct {
	Cluster *ClusterIpList
	Proxy   *httputil.ReverseProxy
}

type Query struct {
	Key string
}

func (handler *TaskHandler) Serve() func(http.ResponseWriter, *http.Request) {
	return handler.Proxy.ServeHTTP
}

func New() *TaskHandler {
	h := &TaskHandler{
		Cluster: NewCluster(),
	}

	director := func(req *http.Request) {
		// Parse body
		var q Query
		err := json.NewDecoder(req.Body).Decode(&q)
		if err != nil {
			return
		}
		nodes, err := h.Cluster.FindNodeForKey(q.Key)
		if err != nil {
			return
		}
		// Pick random node
		selectedNode := nodes[rand.Intn(len(nodes))]
		selectedUrl, err := url.Parse("http://" + selectedNode)
		if err != nil {
			return
		}
		selectedUrl.Path = req.URL.Path
		log.Infof("Forwarding to %s", selectedUrl.String())
		req.URL = selectedUrl

		if _, ok := req.Header["User-Agent"]; !ok {
			// explicitly disable User-Agent so it's not set to default value
			req.Header.Set("User-Agent", "")
		}
	}
	h.Proxy = &httputil.ReverseProxy{Director: director}

	return h
}
