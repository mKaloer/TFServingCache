package cachemanager

type ModelProvider interface {
	LoadModel(modelName string, modelVersion int64, destinationDir string) (*Model, error)
	ModelSize(modelName string, modelVersion int64) (int64, error)
	Check() bool
}
