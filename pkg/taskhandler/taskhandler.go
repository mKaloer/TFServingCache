package taskhandler

import (
	"fmt"
	"math/rand"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/mKaloer/TFServingCache/pkg/tfservingproxy"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/viper"
	"google.golang.org/grpc"
	"google.golang.org/grpc/backoff"
	"google.golang.org/grpc/connectivity"
)

// TaskHandler handles TFServing jobs. A TaskHandler is
// usually associated with one TFServing server, e.g. as a sidecar.
type TaskHandler struct {
	Cluster         *ClusterConnection
	RestProxy       *tfservingproxy.RestProxy
	GrpcProxy       *tfservingproxy.GrpcProxy
	grpcConnections *grpcConnMap
	maxGrpcMsgSize  int
}

type grpcConnMap struct {
	ConnMap map[string]*grpc.ClientConn
	mutex   sync.RWMutex
}

// ServeRest returns a function for HTTP serving
func (handler *TaskHandler) ServeRest() func(http.ResponseWriter, *http.Request) {
	return handler.RestProxy.Serve()
}

// NewTaskHandler creates a new TaskHandler
func NewTaskHandler(dService DiscoveryService) *TaskHandler {
	maxGrpcMsgSize := viper.GetInt("serving.grpcMaxMsgSize")
	if maxGrpcMsgSize == 0 {
		maxGrpcMsgSize = 16 * 1024 * 1024
	}
	h := &TaskHandler{
		Cluster:        NewClusterConnection(dService),
		maxGrpcMsgSize: maxGrpcMsgSize,
	}

	rand.Seed(time.Now().UnixNano())

	h.RestProxy = tfservingproxy.NewRestProxy(h.restDirector)
	h.GrpcProxy = tfservingproxy.NewGrpcProxy(h.grpcDirector, maxGrpcMsgSize)
	h.grpcConnections = &grpcConnMap{ConnMap: make(map[string]*grpc.ClientConn)}
	return h
}

func (handler *TaskHandler) Close() error {
	err := handler.DisconnectFromCluster()
	if err != nil {
		log.WithError(err).Error("Could not disconnect from cluster")
	}
	err = handler.GrpcProxy.Close()
	if err != nil {
		log.WithError(err).Error("Could not close grpc proxy")
	}
	err = handler.grpcConnections.Close()

	return err
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

// nodesForKey returns all replica nodes that can handle the given model
func (handler *TaskHandler) nodesForKey(modelName string, version string) ([]ServingService, error) {
	var modelKey = modelName + "##" + version
	nodes, err := handler.Cluster.FindNodeForKey(modelKey)
	if err != nil {
		return nil, err
	}
	return nodes, nil
}

// restDirector is the director of REST requests.
func (handler *TaskHandler) restDirector(req *http.Request, modelName string, version string) error {
	nodes, err := handler.nodesForKey(modelName, version)
	if err != nil {
		log.WithError(err).Error("Error finding node for model")
		return fmt.Errorf("Error finding node for model: %w", err)
	}
	if len(nodes) == 0 {
		return fmt.Errorf("no available nodes for model %s:%s", modelName, version)
	}
	selectedNode := nodes[rand.Intn(len(nodes))]
	selectedURL, err := url.Parse(fmt.Sprintf("http://%s:%d", selectedNode.Host, selectedNode.RestPort))
	if err != nil {
		log.WithError(err).Error("Error parsing proxy url")
		return fmt.Errorf("Error parsing proxy url: %w", err)
	}
	selectedURL.Path = req.URL.Path
	log.Infof("Forwarding to cache: %s", selectedURL.String())
	req.URL = selectedURL
	if _, ok := req.Header["User-Agent"]; !ok {
		req.Header.Set("User-Agent", "")
	}
	return nil
}

// grpcDirector returns connections to all replica nodes for the given model.
func (handler *TaskHandler) grpcDirector(modelName string, version string) ([]*grpc.ClientConn, error) {
	nodes, err := handler.nodesForKey(modelName, version)
	if err != nil {
		log.WithError(err).Error("Error finding nodes")
		return nil, err
	}
	conns := make([]*grpc.ClientConn, 0, len(nodes))
	for _, node := range nodes {
		grpcHost := fmt.Sprintf("%s:%d", node.Host, node.GrpcPort)
		conn, err := handler.getOrCreateConnection(grpcHost)
		if err != nil {
			log.WithError(err).Warnf("Could not get connection to %s, skipping", grpcHost)
			continue
		}
		conns = append(conns, conn)
	}
	if len(conns) == 0 {
		return nil, fmt.Errorf("no available connections for model %s:%s", modelName, version)
	}
	return conns, nil
}

// getOrCreateConnection returns a healthy cached connection or creates a new one.
// Evicts connections in TransientFailure or Shutdown state.
func (handler *TaskHandler) getOrCreateConnection(grpcHost string) (*grpc.ClientConn, error) {
	handler.grpcConnections.mutex.RLock()
	if conn, ok := handler.grpcConnections.ConnMap[grpcHost]; ok {
		state := conn.GetState()
		if state == connectivity.TransientFailure || state == connectivity.Shutdown {
			handler.grpcConnections.mutex.RUnlock()
			handler.grpcConnections.mutex.Lock()
			defer handler.grpcConnections.mutex.Unlock()
			// Re-check under write lock
			if existing, ok := handler.grpcConnections.ConnMap[grpcHost]; ok && existing == conn {
				log.Warnf("Evicting stale gRPC connection to %s (state: %s)", grpcHost, state)
				if err := conn.Close(); err != nil {
					log.WithError(err).Warnf("Error closing stale gRPC connection to %s", grpcHost)
				}
				delete(handler.grpcConnections.ConnMap, grpcHost)
			} else if ok {
				existState := existing.GetState()
				if existState == connectivity.TransientFailure || existState == connectivity.Shutdown {
					log.Warnf("Replacement connection to %s also stale (state: %s), re-dialing", grpcHost, existState)
					if err := existing.Close(); err != nil {
						log.WithError(err).Warnf("Error closing stale replacement connection to %s", grpcHost)
					}
					delete(handler.grpcConnections.ConnMap, grpcHost)
				} else {
					return existing, nil
				}
			}
			return handler.dialAndStore(grpcHost)
		}
		handler.grpcConnections.mutex.RUnlock()
		return conn, nil
	}
	handler.grpcConnections.mutex.RUnlock()
	handler.grpcConnections.mutex.Lock()
	defer handler.grpcConnections.mutex.Unlock()
	// Double-check after acquiring write lock
	if conn, ok := handler.grpcConnections.ConnMap[grpcHost]; ok {
		return conn, nil
	}
	return handler.dialAndStore(grpcHost)
}

func (handler *TaskHandler) dialAndStore(grpcHost string) (*grpc.ClientConn, error) {
	conn, err := grpc.Dial(grpcHost,
		grpc.WithInsecure(),
		grpc.WithTimeout(viper.GetDuration("serving.grpcPredictTimeout")*time.Second),
		grpc.WithConnectParams(grpc.ConnectParams{Backoff: backoff.DefaultConfig}),
		grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(handler.maxGrpcMsgSize), grpc.MaxCallSendMsgSize(handler.maxGrpcMsgSize)),
	)
	if err != nil {
		log.WithError(err).Errorf("Failed to dial gRPC host %s", grpcHost)
		return nil, err
	}
	handler.grpcConnections.ConnMap[grpcHost] = conn
	return conn, nil
}

func (connMap *grpcConnMap) Close() error {
	var err error = nil
	for k := range connMap.ConnMap {
		err = connMap.ConnMap[k].Close()
		if err != nil {
			log.WithError(err).Errorf("Could not close grpc connection: %s", k)
		}
	}
	return err
}
