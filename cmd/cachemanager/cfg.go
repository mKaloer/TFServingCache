package main

import (
	log "github.com/sirupsen/logrus"
	"github.com/spf13/viper"
)

func SetConfig() {
	viper.SetConfigName("config")
	viper.AddConfigPath(".")
	viper.SetConfigType("yaml")

	err := viper.ReadInConfig() // Find and read the config file
	if err != nil {             // Handle errors reading the config file
		log.WithError(err).Panic("Could not read config file")
	}

}
