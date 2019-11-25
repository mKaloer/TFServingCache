package main

import (
	"net/http"
	"strconv"

	"github.com/mKaloer/tfservingcache/pkg/taskhandler"
)

func main() {
	h := taskhandler.New()

	for i := 0; i < 100; i++ {
		ip := "10.23.423." + strconv.Itoa(i) + ":8080"
		h.Cluster.AddNode(ip)
	} /*
		nodes1, err := h.Cluster.FindNodeForKey("foo")
		println(err)
		for _, n := range nodes1 {
			println(n)
		}
		nodes2, err := h.Cluster.FindNodeForKey("bar")
		println(err)
		for _, n := range nodes2 {
			println(n)
		}*/
	http.HandleFunc("/hello", h.Serve())
	http.ListenAndServe(":8090", nil)
}
