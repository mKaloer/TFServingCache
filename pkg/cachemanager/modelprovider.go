package cachemanager

import log "github.com/sirupsen/logrus"

type ModelProvider interface {
	FetchModel(modelName string, modelVersion string) Model
	ModelSize(modelName string, modelVersion string) uint32
}

type DiskModelProvider struct {
	BaseDir string
}

func (provider DiskModelProvider) FetchModel(modelName string, modelVersion string) Model {
	log.Infof("WOOOO FETCHING STUFF")
	return Model{
		identifier: ModelIdentifier{ModelName: modelName, Version: modelVersion},
		path:       "fooo/bar",
		sizeOnDisk: 100000,
	}
}

func (provider DiskModelProvider) ModelSize(modelName string, modelVersion string) uint32 {
	return 100000
}
