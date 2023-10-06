package diskmodelprovider

import (
	"errors"
	"io/ioutil"
	"os"
	"path"
	"strconv"

	"github.com/otiai10/copy"
	log "github.com/sirupsen/logrus"

	"github.com/mKaloer/TFServingCache/pkg/cachemanager"
)

type DiskModelProvider struct {
	BaseDir string
}

func (provider DiskModelProvider) LoadModel(modelName string, modelVersion int64, destinationDir string) (*cachemanager.Model, error) {
	log.Infof("Copying model %s:%d", modelName, modelVersion)
	srcPath, err := findSrcPathForModel(path.Join(provider.BaseDir, modelName), modelVersion)
	if err != nil {
		log.WithError(err).Errorf("Could not load model %s:%d", modelName, modelVersion)
		return nil, err
	}
	destPath := path.Join(destinationDir, modelName, strconv.FormatInt(modelVersion, 10))
	err = copy.Copy(srcPath, destPath)
	if err != nil {
		log.WithError(err).Errorf("Could not load model %s:%d", modelName, modelVersion)
		return nil, err
	}
	modelSize, err := provider.ModelSize(modelName, modelVersion)
	if err != nil {
		log.WithError(err).Errorf("Could not load model size %s:%d", modelName, modelVersion)
		return nil, err
	}

	return &cachemanager.Model{
		Identifier: cachemanager.ModelIdentifier{ModelName: modelName, Version: modelVersion},
		Path:       path.Join(modelName, strconv.FormatInt(modelVersion, 10)),
		SizeOnDisk: modelSize,
	}, nil
}

func findSrcPathForModel(modelDir string, modelVersion int64) (string, error) {
	files, err := ioutil.ReadDir(modelDir)
	if err != nil {
		return "", err
	}
	match := ""
	numMatches := 0
	for _, file := range files {
		fVersion, err := strconv.ParseInt(file.Name(), 10, 64)
		if err == nil && fVersion == modelVersion && file.IsDir() {
			numMatches++
			match = file.Name()
		}
	}
	if numMatches == 1 {

		return path.Join(modelDir, match), nil
	} else if numMatches > 1 {
		log.Warnf("Several (%d) matches for model found. Using the first match.", numMatches)
		return path.Join(modelDir, match), nil
	} else {
		return "", errors.New("No matching model found")
	}
}

func (provider DiskModelProvider) ModelSize(modelName string, modelVersion int64) (int64, error) {
	srcPath, err := findSrcPathForModel(path.Join(provider.BaseDir, modelName), modelVersion)
	if err != nil {
		return -1, err
	}
	fi, err := os.Stat(srcPath)
	if err != nil {
		return -1, err
	}
	// get the size
	size := fi.Size()
	return size, nil
}

func (provider DiskModelProvider) Check() bool {
	// Assume that disk is always healthy
	return true
}
