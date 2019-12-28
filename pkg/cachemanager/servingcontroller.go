package cachemanager

import (
	"context"
	log "github.com/sirupsen/logrus"
	"path"
	"strconv"
	pb "tensorflow_serving/apis"
	config "tensorflow_serving/config"
	storage_path "tensorflow_serving/sources/storage_path"

	"google.golang.org/grpc"
)

func ReloadConfig(models []*Model, tfServingServerModelDir string) {

	configs := createModelConfig(models, tfServingServerModelDir)
	request := &pb.ReloadConfigRequest{
		Config: &config.ModelServerConfig{
			Config: &config.ModelServerConfig_ModelConfigList{
				ModelConfigList: &config.ModelConfigList{
					Config: configs,
				},
			},
		},
	}

	conn, err := grpc.Dial(":8500", grpc.WithInsecure())
	if err != nil {
		log.Fatalf("Cannot connect to the grpc server: %v\n", err)
	}
	defer conn.Close()

	client := pb.NewModelServiceClient(conn)

	log.Debug("Updating TF config...")
	resp, err := client.HandleReloadConfigRequest(context.Background(), request)
	if err != nil {
		log.Fatalln(err)
	} else {
		log.Debug("TF config updated successfully")
	}

	log.Println(resp)
}

func createModelConfig(models []*Model, tfServingServerModelDir string) []*config.ModelConfig {
	distinctModels := map[string]*storage_path.FileSystemStoragePathSourceConfig_ServableVersionPolicy_Specific{}
	// Number of configs will be at most len(models) large (also the expected val)
	var configs = make([]*config.ModelConfig, 0, len(models))
	for _, model := range models {

		modelVersion, err := strconv.ParseInt(model.Identifier.Version, 10, 64)
		if err != nil {
			log.WithError(err).Errorf("Error converting model version to int: %s:%s", model.Identifier.ModelName, model.Identifier.Version)
			continue
		}

		// Check for existing model of same name
		existingVersions, exists := distinctModels[model.Identifier.ModelName]
		if exists {
			existingVersions.Versions = append(existingVersions.Versions, modelVersion)
		} else {
			// Create new config
			modelVersions := &storage_path.FileSystemStoragePathSourceConfig_ServableVersionPolicy_Specific{
				Versions: []int64{modelVersion},
			}
			distinctModels[model.Identifier.ModelName] = modelVersions
			configs = append(configs, &config.ModelConfig{
				Name:          model.Identifier.ModelName,
				BasePath:      path.Join(tfServingServerModelDir, model.Identifier.ModelName),
				ModelPlatform: "tensorflow",
				ModelVersionPolicy: &storage_path.FileSystemStoragePathSourceConfig_ServableVersionPolicy{
					PolicyChoice: &storage_path.FileSystemStoragePathSourceConfig_ServableVersionPolicy_Specific_{
						Specific: modelVersions,
					},
				},
			})
		}
	}
	return configs
}
