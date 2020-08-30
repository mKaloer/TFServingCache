package main

import (
	log "github.com/sirupsen/logrus"
	"github.com/spf13/viper"
)

func SetConfig() {
	viper.SetConfigName("config")
	viper.AddConfigPath(".")
	viper.SetConfigType("yaml")
	viper.SetEnvPrefix("tfsc")

	err := viper.ReadInConfig() // Find and read the config file
	if err != nil {             // Handle errors reading the config file
		if _, ok := err.(viper.ConfigFileNotFoundError); ok {
			log.Info("No config file found. Reading from env vars")
		} else {
			log.WithError(err).Panic("Could not read config file")
		}
	}

	// Set log level
	switch viper.GetString("logLevel") {
	case "panic":
		log.SetLevel(log.PanicLevel)
		break
	case "fatal":
		log.SetLevel(log.FatalLevel)
		break
	case "warning":
		log.SetLevel(log.WarnLevel)
		break
	case "debug":
		log.SetLevel(log.DebugLevel)
		break
	case "info":
	default:
		log.SetLevel(log.InfoLevel)
	}

}
