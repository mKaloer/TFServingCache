package taskhandler

import (
	"fmt"
	"math/rand"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/mKaloer/TFServingCache/pkg/tfservingproxy"
	log "github.com/sirupsen/logrus"
	"google.golang.org/grpc"
)

type TaskHandler struct {
	Cluster   *ClusterIpList
	RestProxy *tfservingproxy.RestProxy
	GrpcProxy *tfservingproxy.GrpcProxy
}

func (handler *TaskHandler) ServeRest() func(http.ResponseWriter, *http.Request) {
	return handler.RestProxy.Serve()
}

func New(dService DiscoveryService) *TaskHandler {
	h := &TaskHandler{
		Cluster: NewCluster(dService),
	}

	rand.Seed(time.Now().UnixNano())

	h.RestProxy = tfservingproxy.NewRestProxy(h.restDirector)
	h.GrpcProxy = tfservingproxy.NewGrpcProxy(h.grpcDirector)

	return h
}

func (handler *TaskHandler) ConnectToCluster() error {
	return handler.Cluster.Connect()
}

func (handler *TaskHandler) DisconnectFromCluster() error {
	return handler.Cluster.Disconnect()
}

func (handler *TaskHandler) nodeForKey(modelName string, version string) (string, error) {
	var modelKey = modelName + "##" + version
	nodes, err := handler.Cluster.FindNodeForKey(modelKey)
	if err != nil {
		return "", err
	}
	// Pick random node
	return nodes[rand.Intn(len(nodes))], nil
}

func (handler *TaskHandler) restDirector(req *http.Request, modelName string, version string) {
	selectedNode, err := handler.nodeForKey(modelName, version)
	if err != nil {
		log.WithError(err).Error("Error finding node")
		return
	}
	nodeParts := strings.Split(selectedNode, ":")
	// Rest host is idx 0, port is idx 1 after split
	selectedUrl, err := url.Parse(fmt.Sprintf("http://%s:%s", nodeParts[0], nodeParts[1]))
	if err != nil {
		log.WithError(err).Error("Error parsing proxy url")
		return
	}
	selectedUrl.Path = req.URL.Path
	log.Infof("Forwarding to cache: %s", selectedUrl.String())
	req.URL = selectedUrl
	if _, ok := req.Header["User-Agent"]; !ok {
		// explicitly disable User-Agent so it's not set to default value
		req.Header.Set("User-Agent", "")
	}
}

func (handler *TaskHandler) grpcDirector(modelName string, version string) (*grpc.ClientConn, error) {
	selectedNode, err := handler.nodeForKey(modelName, version)
	if err != nil {
		log.WithError(err).Error("Error finding node")
		return nil, err
	}
	nodeParts := strings.Split(selectedNode, ":")
	// grpc host is idx 0, port is idx 2 after split
	grpcHost := fmt.Sprintf("%s:%s", nodeParts[0], nodeParts[2])
	return grpc.Dial(grpcHost, grpc.WithInsecure())
}
