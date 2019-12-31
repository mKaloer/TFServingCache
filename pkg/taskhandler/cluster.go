package taskhandler

import (
	"errors"
	"fmt"

	"stathat.com/c/consistent"
)

type DiscoveryService interface {
	AddNodeListUpdated(chan []string)
	NodeName() string
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

	updateChan := make(chan []string)
	cluster.DiscoveryService.AddNodeListUpdated(updateChan)
	go clusterUpdated(cluster, updateChan)

	return nil
}

func clusterUpdated(cluster *ClusterIpList, updateChan chan []string) {
	for cluster.State == ClusterStateStarted {
		memberships := <-updateChan
		cluster.consistent.Set(memberships)
	}
}

func (cluster *ClusterIpList) FindNodeForKey(key string) ([]string, error) {
	nodes, err := cluster.consistent.GetN(key, 3)
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
