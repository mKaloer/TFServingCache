package taskhandler

import (
	"fmt"
	"math/rand"
	"net/http"
	"net/url"
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

func (handler *TaskHandler) nodeForKey(modelName string, version string) (ServingService, error) {
	var modelKey = modelName + "##" + version
	nodes, err := handler.Cluster.FindNodeForKey(modelKey)
	if err != nil {
		return ServingService{}, err
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
	selectedUrl, err := url.Parse(fmt.Sprintf("http://%s:%d", selectedNode.Host, selectedNode.RestPort))
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
	// grpc host is idx 0, port is idx 2 after split
	grpcHost := fmt.Sprintf("%s:%d", selectedNode.Host, selectedNode.GrpcPort)
	log.Infof("Forwarding to cache: %s", grpcHost)
	return grpc.Dial(grpcHost, grpc.WithInsecure())
}
