package cachemanager

import (
	"context"
	"errors"
	"path"
	pb "tensorflow_serving/apis"
	config "tensorflow_serving/config"
	storage_path "tensorflow_serving/sources/storage_path"

	"github.com/golang/protobuf/ptypes/wrappers"
	log "github.com/sirupsen/logrus"

	"google.golang.org/grpc"
)

type TFServingController struct {
	grpcHost string
	restHost string
}

/*
TYPES FROM TENSORFLOW COPIED FOR CONVENIENCE
*/
type ModelVersionStatus_State int32

const (
	// Default value.
	ModelVersionStatus_UNKNOWN ModelVersionStatus_State = 0
	// The manager is tracking this servable, but has not initiated any action
	// pertaining to it.
	ModelVersionStatus_START ModelVersionStatus_State = 10
	// The manager has decided to load this servable. In particular, checks
	// around resource availability and other aspects have passed, and the
	// manager is about to invoke the loader's Load() method.
	ModelVersionStatus_LOADING ModelVersionStatus_State = 20
	// The manager has successfully loaded this servable and made it available
	// for serving (i.e. GetServableHandle(id) will succeed). To avoid races,
	// this state is not reported until *after* the servable is made
	// available.
	ModelVersionStatus_AVAILABLE ModelVersionStatus_State = 30
	// The manager has decided to make this servable unavailable, and unload
	// it. To avoid races, this state is reported *before* the servable is
	// made unavailable.
	ModelVersionStatus_UNLOADING ModelVersionStatus_State = 40
	// This servable has reached the end of its journey in the manager. Either
	// it loaded and ultimately unloaded successfully, or it hit an error at
	// some point in its lifecycle.
	ModelVersionStatus_END ModelVersionStatus_State = 50
)

func (server *TFServingController) ReloadConfig(models []*Model, tfServingServerModelDir string) error {
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

	conn, err := grpc.Dial(server.grpcHost, grpc.WithInsecure())
	if err != nil {
		log.WithError(err).Error("Cannot connect to the grpc server")
		return err
	}
	defer conn.Close()

	client := pb.NewModelServiceClient(conn)

	log.Debug("Updating TF config...")
	_, err = client.HandleReloadConfigRequest(context.Background(), request)
	if err != nil {
		log.WithError(err).Error("Error updating tf config")
		return err
	} else {
		log.Debug("TF config updated successfully")
	}

	return nil
}

func (server *TFServingController) GetModelStatus(model Model) (ModelVersionStatus_State, error) {
	conn, err := grpc.Dial(server.grpcHost, grpc.WithInsecure())
	if err != nil {
		log.WithError(err).Error("Cannot connect to the grpc server")
		return 0, err
	}
	defer conn.Close()

	client := pb.NewModelServiceClient(conn)

	log.Debug("Getting TF model status...")
	statusRequest := &pb.GetModelStatusRequest{
		ModelSpec: &pb.ModelSpec{
			Name: model.Identifier.ModelName, Version: &wrappers.Int64Value{Value: model.Identifier.Version},
		},
	}
	resp, err := client.GetModelStatus(context.Background(), statusRequest)
	if err != nil {
		log.WithError(err).Error("Error getting tf serving model status")
		return 0, err
	} else {
		log.Debug("TF model status received successfully")
	}

	if len(resp.ModelVersionStatus) > 0 {
		return modelVersionStatusStateFromTFState(resp.ModelVersionStatus[0].State), nil
	}
	return 0, errors.New("Model not found")
}

func createModelConfig(models []*Model, tfServingServerModelDir string) []*config.ModelConfig {
	distinctModels := map[string]*storage_path.FileSystemStoragePathSourceConfig_ServableVersionPolicy_Specific{}
	// Number of configs will be at most len(models) large (also the expected val)
	var configs = make([]*config.ModelConfig, 0, len(models))
	for _, model := range models {
		// Check for existing model of same name
		existingVersions, exists := distinctModels[model.Identifier.ModelName]
		if exists {
			existingVersions.Versions = append(existingVersions.Versions, model.Identifier.Version)
		} else {
			// Create new config
			modelVersions := &storage_path.FileSystemStoragePathSourceConfig_ServableVersionPolicy_Specific{
				Versions: []int64{model.Identifier.Version},
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

func modelVersionStatusStateFromTFState(state pb.ModelVersionStatus_State) ModelVersionStatus_State {
	switch state {
	case pb.ModelVersionStatus_UNKNOWN:
		return ModelVersionStatus_UNKNOWN
	case pb.ModelVersionStatus_START:
		return ModelVersionStatus_START
	case pb.ModelVersionStatus_LOADING:
		return ModelVersionStatus_LOADING
	case pb.ModelVersionStatus_AVAILABLE:
		return ModelVersionStatus_AVAILABLE
	case pb.ModelVersionStatus_UNLOADING:
		return ModelVersionStatus_UNLOADING
	case pb.ModelVersionStatus_END:
		return ModelVersionStatus_END
	default:
		return ModelVersionStatus_UNKNOWN
	}
}

func (state *ModelVersionStatus_State) String() string {
	switch *state {
	case ModelVersionStatus_UNKNOWN:
		return "UNKNOWN"
	case ModelVersionStatus_START:
		return "START"
	case ModelVersionStatus_LOADING:
		return "LOADING"
	case ModelVersionStatus_AVAILABLE:
		return "AVAILABLE"
	case ModelVersionStatus_UNLOADING:
		return "UNLOADING"
	case ModelVersionStatus_END:
		return "END"
	default:
		return "UNKNOWN"
	}
}
