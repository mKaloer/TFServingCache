package diskmodelprovider

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	log "github.com/sirupsen/logrus"
)

func createDummyModelFile(modelRepo string, name string, version string) {
	modelDir := filepath.Join(modelRepo, name, version)
	err := os.MkdirAll(modelDir, os.ModePerm)
	if err != nil {
		log.WithError(err).Panicf("Error creating model file")
	}
	err = os.Mkdir(filepath.Join(modelDir, "assets"), os.ModePerm)
	if err != nil {
		log.WithError(err).Panicf("Error creating model file")
	}
	err = os.Mkdir(filepath.Join(modelDir, "variables"), os.ModePerm)
	if err != nil {
		log.WithError(err).Panicf("Error creating model file")
	}
	_, err = os.Create(filepath.Join(modelDir, "saved_model.pb"))
	if err != nil {
		log.WithError(err).Panicf("Error creating model file")
	}
}

func TestDiskModelProviderLoadsCorrectModel(t *testing.T) {
	modelDir, err := ioutil.TempDir("", ".testModelDir")
	if err != nil {
		log.WithError(err).Panicf("Error creating model file")
	}
	modelDestDir, err := ioutil.TempDir("", ".testModelDestDir")
	defer os.RemoveAll(modelDir)
	defer os.RemoveAll(modelDestDir)
	modelName := "myModel"
	modelVersion := 42
	createDummyModelFile(modelDir, modelName, strconv.Itoa(modelVersion))
	createDummyModelFile(modelDir, modelName, "43")
	createDummyModelFile(modelDir, modelName, "4")
	createDummyModelFile(modelDir, modelName, "2")
	createDummyModelFile(modelDir, modelName, "0")
	createDummyModelFile(modelDir, "someDifferentModel", "22")
	createDummyModelFile(modelDir, "someDifferentModel", "42")

	provider := DiskModelProvider{BaseDir: modelDir}

	model, err := provider.LoadModel(modelName, int64(modelVersion), modelDestDir)

	if model.Identifier.ModelName != modelName {
		t.Errorf("Wrong model name after load")
	}
	if model.Identifier.Version != int64(modelVersion) {
		t.Errorf("Wrong model version after load")
	}
}

func TestDiskModelProviderLoadsCanMatchPrefixZeros(t *testing.T) {
	modelDir, err := ioutil.TempDir("", ".testModelDir")
	if err != nil {
		log.WithError(err).Panicf("Error creating model file")
	}
	modelDestDir, err := ioutil.TempDir("", ".testModelDestDir")
	defer os.RemoveAll(modelDir)
	defer os.RemoveAll(modelDestDir)
	modelName := "myModel"
	modelVersion := 42
	createDummyModelFile(modelDir, modelName, "000000042")
	createDummyModelFile(modelDir, modelName, "000000043")
	createDummyModelFile(modelDir, modelName, "41")

	provider := DiskModelProvider{BaseDir: modelDir}

	model, err := provider.LoadModel(modelName, int64(modelVersion), modelDestDir)

	if model.Identifier.ModelName != modelName {
		t.Errorf("Wrong model name after load")
	}
	if model.Identifier.Version != int64(modelVersion) {
		t.Errorf("Wrong model version after load")
	}
}
