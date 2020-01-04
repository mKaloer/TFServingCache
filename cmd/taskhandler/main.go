package main

import (
	"fmt"
	"net/http"

	"github.com/mKaloer/TFServingCache/pkg/taskhandler/consul"
	"github.com/mKaloer/tfservingcache/pkg/taskhandler"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/viper"
)

func main() {

	SetConfig()

	dService, err := consul.NewDiscoveryService(healthCheck)
	if err != nil {
		log.WithError(err).Fatal("Could not create discovery service")
	}
	tHandler := taskhandler.New(dService)
	err = tHandler.ConnectToCluster()
	if err != nil {
		log.WithError(err).Fatal("Could not create discovery service")
	}
	defer tHandler.DisconnectFromCluster()
	http.HandleFunc("/v1/models/", tHandler.ServeRest())
	http.ListenAndServe(fmt.Sprintf(":%d", viper.GetInt("restPort")), nil)

}

func healthCheck() (bool, error) {
	return true, nil
}
