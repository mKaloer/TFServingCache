package taskhandler

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"

	log "github.com/sirupsen/logrus"
	"github.com/spf13/viper"
	"stathat.com/c/consistent"
)

type ServingService struct {
	Host     string
	GrpcPort int
	RestPort int
}

type DiscoveryService interface {
	AddNodeListUpdated(string, chan []ServingService)
	RemoveNodeListUpdated(string)
	RegisterService() error
	UnregisterService() error
}

type ClusterState int

const (
	ClusterStateReady ClusterState = iota
	ClusterStateStarted
)

type ClusterIpList struct {
	consistent       *consistent.Consistent
	DiscoveryService DiscoveryService
	State            ClusterState
	memberUpdateChan chan []ServingService
}

func NewCluster(dService DiscoveryService) *ClusterIpList {
	cluster := &ClusterIpList{
		consistent:       consistent.New(),
		DiscoveryService: dService,
		State:            ClusterStateReady,
	}

	return cluster
}

func (cluster *ClusterIpList) Connect() error {
	if cluster.State != ClusterStateReady {
		return errors.New(fmt.Sprintf("Illegal cluster state: %s", cluster.State.String()))
	}

	cluster.memberUpdateChan = make(chan []ServingService)
	cluster.DiscoveryService.AddNodeListUpdated("clusterChan", cluster.memberUpdateChan)

	err := cluster.DiscoveryService.RegisterService()
	if err != nil {
		log.WithError(err).Fatal("Could not register discovery service")
		return err
	}
	cluster.State = ClusterStateStarted
	go clusterUpdated(cluster, cluster.memberUpdateChan)

	return nil
}

func (cluster *ClusterIpList) Disconnect() error {
	if cluster.State != ClusterStateStarted {
		return fmt.Errorf("Illegal cluster state: %s", cluster.State.String())
	}

	cluster.DiscoveryService.RemoveNodeListUpdated("clusterChan")
	cluster.State = ClusterStateReady
	err := cluster.DiscoveryService.UnregisterService()
	if err != nil {
		log.WithError(err).Fatal("Could not unregister discovery service")
		return err
	}

	return nil
}

func clusterUpdated(cluster *ClusterIpList, updateChan chan []ServingService) {
	for cluster.State == ClusterStateStarted {
		memberships := <-updateChan
		services := make([]string, len(memberships))
		for m := range memberships {
			services[m] = memberships[m].String()
		}
		cluster.consistent.Set(services)
	}
}

func (cluster *ClusterIpList) FindNodeForKey(key string) ([]ServingService, error) {
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
