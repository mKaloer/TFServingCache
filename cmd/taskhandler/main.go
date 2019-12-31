package main

import (
	"fmt"
	"net/http"

	"github.com/mKaloer/TFServingCache/pkg/taskhandler/consul"
	"github.com/mKaloer/tfservingcache/pkg/taskhandler"
)

func main() {

	config := SetConfig()

	dService := consul.NewDiscoveryService()
	tHandler := taskhandler.New(dService)
	http.HandleFunc("/v1/models/", tHandler.ServeRest())
	http.ListenAndServe(fmt.Sprintf(":%d", config.RestPort), nil)

	for i := 0; i < 100; i++ {
		//ip := "10.23.423." + strconv.Itoa(i) + ":8080"

	}
	http.HandleFunc("/v1/models/", tHandler.ServeRest())
	http.ListenAndServe(fmt.Sprintf(":%d", config.RestPort), nil)
}
