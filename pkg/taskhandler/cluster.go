package taskhandler

import (
	"stathat.com/c/consistent"
)

type ClusterIpList struct {
	consistent *consistent.Consistent
}

func NewCluster() *ClusterIpList {
	cluster := &ClusterIpList{
		consistent: consistent.New(),
	}
	return cluster
}

func (cluster *ClusterIpList) FindNodeForKey(key string) ([]string, error) {
	nodes, err := cluster.consistent.GetN(key, 3)
	if err != nil {
		return nil, err
	}
	return nodes, nil
}

func (cluster *ClusterIpList) AddNode(node string) {
	cluster.consistent.Add(node)
}

func (cluster *ClusterIpList) RemoveNode(node string) {
	cluster.consistent.Remove(node)
}
