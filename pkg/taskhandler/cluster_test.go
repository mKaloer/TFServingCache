package taskhandler

import (
	"fmt"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	log "github.com/sirupsen/logrus"
)

type DiscoveryServiceMock struct {
	ListUpdatedChans          map[string]chan []ServingService
	NumRegisterServiceCalls   int
	NumUnregisterServiceCalls int
}

func (dService *DiscoveryServiceMock) AddNodeListUpdated(name string, ch chan []ServingService) {
	dService.ListUpdatedChans[name] = ch
}

func (dService *DiscoveryServiceMock) RemoveNodeListUpdated(name string) {
	delete(dService.ListUpdatedChans, name)
}

func (dService *DiscoveryServiceMock) RegisterService() error {
	dService.NumRegisterServiceCalls++
	return nil
}

func (dService *DiscoveryServiceMock) UnregisterService() error {
	dService.NumUnregisterServiceCalls++
	return nil
}

func (dService *DiscoveryServiceMock) GenerateMembers(numMembers int) {
	// Simulate new member list
	memberList := make([]ServingService, numMembers)
	for i := 0; i < numMembers; i++ {
		memberList[i] = ServingService{
			Host:     fmt.Sprintf("testhost_%d", i),
			GrpcPort: 2000 + i,
			RestPort: 8000 + i,
		}
	}
	for ch := range dService.ListUpdatedChans {
		dService.ListUpdatedChans[ch] <- memberList
	}
}

func TestConsistentHashingForNodes(t *testing.T) {
	dService := &DiscoveryServiceMock{
		ListUpdatedChans: make(map[string]chan []ServingService, 0),
	}
	cluster := NewClusterConnection(dService)

	err := cluster.Connect()
	if err != nil {
		log.WithError(err).Panicf("Error connecting to cluster")
	}

	if dService.NumRegisterServiceCalls != 1 {
		t.Errorf("Expected service register to have been called once")
	}

	// wait for nodes to become visible
	err = generateNodesAndWaitForMembership(dService, cluster, 100)
	if err != nil {
		log.WithError(err).Panicf("Error generating nodes")
	}

	// Check consistent hashing
	nodeMap := make(map[string][]ServingService)
	nodeNames := []string{"FoobarA", "FoobarB", "FoobarC", "FoobarD", "FoobarE", "FoobarF"}
	for i := 0; i < 10000; i++ {
		for _, name := range nodeNames {
			nodes, err := cluster.FindNodeForKey(name)
			if err != nil {
				log.WithError(err).Panicf("Error resolving nodes. i=%d", i)
			}
			if val, ok := nodeMap[name]; ok {
				if !cmp.Equal(val, nodes) {
					t.Errorf("Expected consistent nodes but found difference")
				}
			} else {
				nodeMap[name] = nodes
			}
		}
	}

	if len(nodeMap) != len(nodeNames) {
		t.Errorf("Nodes not found in map")
	}

	cluster.Disconnect()

	if dService.NumUnregisterServiceCalls != 1 {
		t.Errorf("Expected service unregister to have been called once")
	}
}

func TestMembershipWithOneNode(t *testing.T) {
	dService := &DiscoveryServiceMock{
		ListUpdatedChans: make(map[string]chan []ServingService, 0),
	}
	cluster := NewClusterConnection(dService)

	err := cluster.Connect()
	if err != nil {
		log.WithError(err).Panicf("Error connecting to cluster")
	}

	if dService.NumRegisterServiceCalls != 1 {
		t.Errorf("Expected service register to have been called once")
	}

	// wait for nodes to become visible
	err = generateNodesAndWaitForMembership(dService, cluster, 1)
	if err != nil {
		log.WithError(err).Panicf("Error generating nodes")
	}

	// Check consistent hashing
	nodeNames := []string{"FoobarA", "FoobarB", "FoobarC", "FoobarD", "FoobarE", "FoobarF"}
	for _, name := range nodeNames {
		nodes, err := cluster.FindNodeForKey(name)
		if len(nodes) != 1 {
			t.Errorf("Expected one node for key, but found %d", len(nodes))
		}
		if nodes[0].Host != "testhost_0" {
			t.Errorf("Expected node to be 'testhost_0' but found '%s'", nodes[0].Host)
		}
		if err != nil {
			log.WithError(err).Panic("Error resolving nodes")
		}
	}

	cluster.Disconnect()

	if dService.NumUnregisterServiceCalls != 1 {
		t.Errorf("Expected service unregister to have been called once")
	}
}

func TestConsistentHashingForNodesDuringMembershipChange(t *testing.T) {
	dService := &DiscoveryServiceMock{
		ListUpdatedChans: make(map[string]chan []ServingService, 0),
	}
	cluster := NewClusterConnection(dService)

	err := cluster.Connect()
	if err != nil {
		log.WithError(err).Panicf("Error connecting to cluster")
	}

	if dService.NumRegisterServiceCalls != 1 {
		t.Errorf("Expected service register to have been called once")
	}

	// wait for nodes to become visible
	err = generateNodesAndWaitForMembership(dService, cluster, 5)
	if err != nil {
		log.WithError(err).Panicf("Error generating nodes")
	}

	// Store initial node hashing
	nodeMap := make(map[string][]ServingService)
	nodeNames := []string{"FoobarA", "FoobarB", "FoobarC", "FoobarD", "FoobarE", "FoobarF"}
	for _, name := range nodeNames {
		nodes, err := cluster.FindNodeForKey(name)
		if err != nil {
			log.WithError(err).Panicf("Error resolving nodes")
		}
		nodeMap[name] = nodes
	}

	if len(nodeMap) != len(nodeNames) {
		t.Errorf("Nodes not found in map")
	}

	// Update memberships
	err = generateNodesAndWaitForMembership(dService, cluster, 200)
	if err != nil {
		log.WithError(err).Panicf("Error generating nodes")
	}

	hasDifferentHost := false
	for _, name := range nodeNames {
		nodes, err := cluster.FindNodeForKey(name)
		if err != nil {
			log.WithError(err).Panicf("Error resolving nodes")
		}
		// Check if nodes are at same hosts (they should not be)
		if val, ok := nodeMap[name]; ok {
			if !cmp.Equal(val, nodes) {
				hasDifferentHost = true
				break
			}
		}
	}
	if !hasDifferentHost {
		t.Errorf("Expected hosts to change nodes have been added, but all hosts are the same")
	}

	// Back to original node set
	err = generateNodesAndWaitForMembership(dService, cluster, 5)
	if err != nil {
		log.WithError(err).Panicf("Error generating nodes")
	}
	for _, name := range nodeNames {
		nodes, err := cluster.FindNodeForKey(name)
		if err != nil {
			log.WithError(err).Panicf("Error resolving nodes")
		}
		if val, ok := nodeMap[name]; ok {
			if !cmp.Equal(val, nodes) {
				t.Errorf("Expected consistent nodes but found difference")
			}
		}
	}

	cluster.Disconnect()

	if dService.NumUnregisterServiceCalls != 1 {
		t.Errorf("Expected service unregister to have been called once")
	}
}

func generateNodesAndWaitForMembership(dService *DiscoveryServiceMock, cluster *ClusterConnection, numNodes int) error {
	// wait for nodes to become visible
	dService.GenerateMembers(numNodes)
	// TODO: This should be implemented with callbacks or similar instead of a sleep
	time.Sleep(1000 * time.Millisecond)
	return nil
}
