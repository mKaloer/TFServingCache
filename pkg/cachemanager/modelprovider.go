package cachemanager

type ModelProvider interface {
	LoadModel(modelName string, modelVersion string, destinationDir string) (Model, error)
	ModelSize(modelName string, modelVersion string) (int64, error)
}
