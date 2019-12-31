package main

import "flag"

type Config struct {
	RestPort int
	GrpcPort int
}

func SetConfig() *Config {
	configs := Config{}
	flag.IntVar(&configs.RestPort, "restPort", 8090, "Port for REST taskhandler service.")
	flag.IntVar(&configs.GrpcPort, "grpcPort", 8091, "Port for GRPC taskhandler service.")
	return &configs
}
