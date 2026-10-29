package taskhandler

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"

	log "github.com/sirupsen/logrus"
	"github.com/spf13/viper"
	"stathat.com/c/consistent"
)

// ServingService contains network information of a
// service that provides TF Serving
type ServingService struct {
	Host     string
	GrpcPort int
	RestPort int
}

// DiscoveryService is a service discovery provider.
// It has the responsibility to discover other
// ServingServices and to register itself on the network.
type DiscoveryService interface {
	AddNodeListUpdated(string, chan []ServingService)
	RemoveNodeListUpdated(string)
	RegisterService() error
	UnregisterService() error
}

// ClusterState represents the current state of the node
type ClusterState int

const (
	// ClusterStateReady represents that the node is ready to connect to cluster
	ClusterStateReady ClusterState = iota
	// ClusterStateStarted represents that the node is connected to cluster
	ClusterStateStarted
)

// ClusterConnection represents a connection to a cluster,
// and contains information such as the cluster membership list
type ClusterConnection struct {
	consistent       *consistent.Consistent
	DiscoveryService DiscoveryService
	State            ClusterState
	stateMux         sync.RWMutex
	memberUpdateChan chan []ServingService
	// done stops the membership goroutine; stopped is closed by that
	// goroutine as it exits, so Disconnect can wait for it.
	done    chan struct{}
	stopped chan struct{}
}

func (cluster *ClusterConnection) state() ClusterState {
	cluster.stateMux.RLock()
	defer cluster.stateMux.RUnlock()
	return cluster.State
}

func (cluster *ClusterConnection) setState(state ClusterState) {
	cluster.stateMux.Lock()
	defer cluster.stateMux.Unlock()
	cluster.State = state
}

// NewClusterConnection creates a new ClusterConnection.
// It does not connect to the cluster before Connect() is called.
func NewClusterConnection(dService DiscoveryService) *ClusterConnection {
	cluster := &ClusterConnection{
		consistent:       consistent.New(),
		DiscoveryService: dService,
		State:            ClusterStateReady,
	}

	return cluster
}

// Connect connects the current node to the cluster,
// that is, it registers the node and starts listening for
// memebership updates.
func (cluster *ClusterConnection) Connect() error {
	if state := cluster.state(); state != ClusterStateReady {
		return fmt.Errorf("Illegal cluster state: %s", state.String())
	}

	cluster.memberUpdateChan = make(chan []ServingService)
	cluster.done = make(chan struct{})
	cluster.stopped = make(chan struct{})
	cluster.DiscoveryService.AddNodeListUpdated("clusterChan", cluster.memberUpdateChan)

	err := cluster.DiscoveryService.RegisterService()
	if err != nil {
		log.WithError(err).Fatal("Could not register discovery service")
		return err
	}
	cluster.setState(ClusterStateStarted)
	go clusterUpdated(cluster, cluster.memberUpdateChan, cluster.done, cluster.stopped)

	return nil
}

// Disconnect removes this node from the cluster. Notice
// that it is not necesarraily unregistered immediately,
// depending on the discovery service implementation.
func (cluster *ClusterConnection) Disconnect() error {
	if state := cluster.state(); state != ClusterStateStarted {
		return fmt.Errorf("Illegal cluster state: %s", state.String())
	}

	cluster.DiscoveryService.RemoveNodeListUpdated("clusterChan")
	cluster.setState(ClusterStateReady)
	// Stop the membership goroutine and wait for it: it blocks on the update
	// channel, so it would otherwise outlive the connection.
	close(cluster.done)
	<-cluster.stopped
	err := cluster.DiscoveryService.UnregisterService()
	if err != nil {
		log.WithError(err).Fatal("Could not unregister discovery service")
		return err
	}

	return nil
}

func clusterUpdated(cluster *ClusterConnection, updateChan chan []ServingService, done, stopped chan struct{}) {
	defer close(stopped)
	for {
		select {
		case <-done:
			return
		case memberships := <-updateChan:
			services := make([]string, len(memberships))
			for m := range memberships {
				services[m] = memberships[m].String()
			}
			cluster.consistent.Set(services)
		}
	}
}

// FindNodeForKey returns a node that can handle the model specified by the given key.
func (cluster *ClusterConnection) FindNodeForKey(key string) ([]ServingService, error) {
	nodes, err := cluster.consistent.GetN(key, int(math.Max(viper.GetFloat64("proxy.replicasPerModel"), 1)))
	if err != nil {
		return nil, err
	}
	services := make([]ServingService, 0, len(nodes))
	for n := range nodes {
		s, err := serviceFromString(nodes[n])
		if err != nil {
			log.WithError(err).Errorf("Invalid memmber in memberlist. Skipping: %s", nodes[n])
		}
		services = append(services, s)
	}
	return services, nil
}

func (state *ClusterState) String() string {
	switch *state {
	case ClusterStateReady:
		return "READY"
	case ClusterStateStarted:
		return "STARTED"
	}
	return "UNKNOWN"
}

func (service *ServingService) String() string {
	return fmt.Sprintf("%s:%d:%d", service.Host, service.RestPort, service.GrpcPort)
}

func serviceFromString(host string) (ServingService, error) {
	stringParts := strings.Split(host, ":")
	restPort, err := strconv.Atoi(stringParts[1])
	if err != nil {
		log.WithError(err).Errorf("Could not convert port number to int: %s", stringParts[1])
		return ServingService{}, err
	}
	grpcPort, err := strconv.Atoi(stringParts[2])
	if err != nil {
		log.WithError(err).Errorf("Could not convert port number to int: %s", stringParts[2])
		return ServingService{}, err
	}

	return ServingService{
		Host:     stringParts[0],
		RestPort: restPort,
		GrpcPort: grpcPort,
	}, nil
}
