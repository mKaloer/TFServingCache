package taskhandler

import (
	"fmt"
	"math/rand"
	"net/http"
	"net/url"
	"time"

	"github.com/mKaloer/TFServingCache/pkg/tfservingproxy"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/viper"
	"google.golang.org/grpc"
	"google.golang.org/grpc/backoff"
)

// TaskHandler handles TFServing jobs. A TaskHandler is
// usually associated with one TFServing server, e.g. as a sidecar.
type TaskHandler struct {
	Cluster   *ClusterConnection
	RestProxy *tfservingproxy.RestProxy
	GrpcProxy *tfservingproxy.GrpcProxy
}

// ServeRest returns a function for HTTP serving
func (handler *TaskHandler) ServeRest() func(http.ResponseWriter, *http.Request) {
	return handler.RestProxy.Serve()
}

// NewTaskHandler creates a new TaskHandler
func NewTaskHandler(dService DiscoveryService) *TaskHandler {
	h := &TaskHandler{
		Cluster: NewClusterConnection(dService),
	}

	rand.Seed(time.Now().UnixNano())

	h.RestProxy = tfservingproxy.NewRestProxy(h.restDirector)
	h.GrpcProxy = tfservingproxy.NewGrpcProxy(h.grpcDirector)

	return h
}

// ConnectToCluster makes this TaskHandler discoverable
// in the cluster and starts listening for other members
func (handler *TaskHandler) ConnectToCluster() error {
	return handler.Cluster.Connect()
}

// DisconnectFromCluster disconnects the TaskHandler from the
// cluster (eventually)
func (handler *TaskHandler) DisconnectFromCluster() error {
	return handler.Cluster.Disconnect()
}

// nodeForKey returns a node that can handle the given model
func (handler *TaskHandler) nodeForKey(modelName string, version string) (ServingService, error) {
	var modelKey = modelName + "##" + version
	nodes, err := handler.Cluster.FindNodeForKey(modelKey)
	if err != nil {
		return ServingService{}, err
	}
	// Pick random node
	return nodes[rand.Intn(len(nodes))], nil
}

// restDirector is the director of REST requests.
func (handler *TaskHandler) restDirector(req *http.Request, modelName string, version string) {
	selectedNode, err := handler.nodeForKey(modelName, version)
	if err != nil {
		log.WithError(err).Error("Error finding node")
		return
	}
	selectedURL, err := url.Parse(fmt.Sprintf("http://%s:%d", selectedNode.Host, selectedNode.RestPort))
	if err != nil {
		log.WithError(err).Error("Error parsing proxy url")
		return
	}
	selectedURL.Path = req.URL.Path
	log.Infof("Forwarding to cache: %s", selectedURL.String())
	req.URL = selectedURL
	if _, ok := req.Header["User-Agent"]; !ok {
		// explicitly disable User-Agent so it's not set to default value
		req.Header.Set("User-Agent", "")
	}
}

// grpcDirector is the director of GRPC requests.
func (handler *TaskHandler) grpcDirector(modelName string, version string) (*grpc.ClientConn, error) {
	selectedNode, err := handler.nodeForKey(modelName, version)
	if err != nil {
		log.WithError(err).Error("Error finding node")
		return nil, err
	}
	// grpc host is idx 0, port is idx 2 after split
	grpcHost := fmt.Sprintf("%s:%d", selectedNode.Host, selectedNode.GrpcPort)
	log.Infof("Forwarding to cache: %s", grpcHost)
	return grpc.Dial(grpcHost,
		grpc.WithInsecure(),
		grpc.WithTimeout(viper.GetDuration("serving.grpcPredictTimeout")*time.Second),
		grpc.WithConnectParams(grpc.ConnectParams{Backoff: backoff.DefaultConfig}))
}
