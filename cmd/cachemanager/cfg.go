package main

import "flag"

type Config struct {
	ModelRepoDir          string
	HostServingModelDir   string
	ServerServingModelDir string
	ServingGrpcHost       string
	ServingRestHost       string
	ModelCacheSize        int64
	RestPort              int
	GrpcPort              int
}

func SetConfig() *Config {
	configs := Config{}
	flag.StringVar(&configs.ModelRepoDir, "modelRepo", "./model_repo", "Model repo path.")
	flag.StringVar(&configs.HostServingModelDir, "hostServingModelDir", "./models", "Model directory path on host.")
	flag.StringVar(&configs.ServerServingModelDir, "serverServingModelDir", "/models", "Model directory path on TF serving server.")
	flag.Int64Var(&configs.ModelCacheSize, "cacheSize", 1000000000, "Cache size in bytes.")
	flag.StringVar(&configs.ServingGrpcHost, "servingGrpc", "localhost:8500", "TF serving GRPC host.")
	flag.StringVar(&configs.ServingRestHost, "servingRest", "http://localhost:8501", "TF serving REST host.")
	flag.IntVar(&configs.RestPort, "restPort", 8091, "Port for REST cache service.")
	flag.IntVar(&configs.GrpcPort, "grpcPort", 8092, "Port for GRPC cache service.")
	return &configs
}
