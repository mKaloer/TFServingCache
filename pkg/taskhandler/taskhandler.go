package taskhandler

import (
	"math/rand"
	"net/http"
	"net/url"
	"time"

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

func New(dService DiscoveryService) *TaskHandler {
	h := &TaskHandler{
		Cluster: NewCluster(dService),
	}

	rand.Seed(time.Now().UnixNano())

	director := func(req *http.Request, modelName string, version string) {
		var modelKey = modelName + "##" + version
		nodes, err := h.Cluster.FindNodeForKey(modelKey)
		if err != nil {
			log.WithError(err).Error("Error finding node")
			return
		}
		// Pick random node
		selectedNode := nodes[rand.Intn(len(nodes))]
		selectedUrl, err := url.Parse("http://" + selectedNode)
		if err != nil {
			log.WithError(err).Error("Error parsing proxy url")
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

func (handler *TaskHandler) ConnectToCluster() error {
	return handler.Cluster.Connect()
}

func (handler *TaskHandler) DisconnectFromCluster() error {
	return handler.Cluster.Disconnect()
}
