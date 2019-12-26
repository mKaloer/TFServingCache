package taskhandler

import (
	"math/rand"
	"net/http"
	"net/url"

	"github.com/mKaloer/tfservingcache/pkg/tfservingproxy"
	log "github.com/sirupsen/logrus"
)

type TaskHandler struct {
	Cluster   *ClusterIpList
	RestProxy *tfservingproxy.RestProxy
}

func (handler *TaskHandler) ServeRest() func(http.ResponseWriter, *http.Request) {
	return handler.RestProxy.Serve()
}

func New() *TaskHandler {
	h := &TaskHandler{
		Cluster: NewCluster(),
	}

	director := func(req *http.Request, modelName string, version string) {
		var modelKey = modelName + "##" + version
		nodes, err := h.Cluster.FindNodeForKey(modelKey)
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
	h.RestProxy = tfservingproxy.NewRestProxy(director)

	return h
}
