package taskhandler

import (
	"errors"
	"fmt"
	"math"

	log "github.com/sirupsen/logrus"
	"github.com/spf13/viper"
	"stathat.com/c/consistent"
)

type DiscoveryService interface {
	AddNodeListUpdated(string, chan []string)
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
	memberUpdateChan chan []string
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

	cluster.memberUpdateChan = make(chan []string)
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
		return errors.New(fmt.Sprintf("Illegal cluster state: %s", cluster.State.String()))
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

func clusterUpdated(cluster *ClusterIpList, updateChan chan []string) {
	for cluster.State == ClusterStateStarted {
		memberships := <-updateChan
		cluster.consistent.Set(memberships)
	}
}

func (cluster *ClusterIpList) FindNodeForKey(key string) ([]string, error) {
	nodes, err := cluster.consistent.GetN(key, int(math.Max(viper.GetFloat64("proxy.replicasPerModel"), 1)))
	if err != nil {
		return nil, err
	}
	return nodes, nil
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
