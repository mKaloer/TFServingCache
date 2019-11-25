package main

import (
	"strconv"

	"github.com/mKaloer/tfservingcache/pkg/taskhandler/taskhandler"
)

func main() {
	c := taskhandler.NewCluster()
	for i := 0; i < 100; i++ {
		ip := "10.23.423." + strconv.Itoa(i) + ":8080"
		c.AddNode(ip)
	}
	nodes1, err := c.FindNodeForKey("foo")
	println(err)
	for _, n := range nodes1 {
		println(n)
	}
	nodes2, err := c.FindNodeForKey("bar")
	println(err)
	for _, n := range nodes2 {
		println(n)
	}
}
