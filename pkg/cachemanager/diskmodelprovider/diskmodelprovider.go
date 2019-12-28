package diskmodelprovider

import (
	"os"
	"path"

	log "github.com/sirupsen/logrus"
	"github.com/otiai10/copy"

	"github.com/mKaloer/tfservingcache/pkg/cachemanager"
)

type DiskModelProvider struct {
	BaseDir string
}

func (provider DiskModelProvider) LoadModel(modelName string, modelVersion string, destinationDir string) (cachemanager.Model, error) {
	log.Infof("Copying model %s:%s", modelName, modelVersion)
	srcPath := path.Join(provider.BaseDir, modelName, modelVersion)
	destPath := path.Join(destinationDir, modelName, modelVersion)
	err := copy.Copy(srcPath, destPath)
	if err != nil {
		log.WithError(err).Errorf("Could not load model %s:%s", modelName, modelVersion)
		return cachemanager.Model{}, err
	}
	modelSize, err := provider.ModelSize(modelName, modelVersion)
	if err != nil {
		log.WithError(err).Errorf("Could not load model size %s:%s", modelName, modelVersion)
		return cachemanager.Model{}, err
	}

	return cachemanager.Model{
		Identifier: cachemanager.ModelIdentifier{ModelName: modelName, Version: modelVersion},
		Path:       path.Join(modelName, modelVersion),
		SizeOnDisk: modelSize,
	}, nil
}

func (provider DiskModelProvider) ModelSize(modelName string, modelVersion string) (int64, error) {
	srcPath := path.Join(provider.BaseDir, modelName, modelVersion)
	fi, err := os.Stat(srcPath)
	if err != nil {
		return -1, err
	}
	// get the size
	size := fi.Size()
	return size, nil
}
