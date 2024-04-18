package cachemanager

import (
	"context"
	"errors"
	"path"
	"time"

	serving "github.com/mKaloer/TFServingCache/proto/tensorflow/serving"
	"github.com/spf13/viper"

	"github.com/golang/protobuf/ptypes/wrappers"
	log "github.com/sirupsen/logrus"

	"google.golang.org/grpc"
	"google.golang.org/grpc/backoff"
)

type TFServingController struct {
	grpcHost             string
	restHost             string
	grpcClient           *grpc.ClientConn
	healthProbeModelName string
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

func NewTFServingController(grpcHost string, restHost string) (*TFServingController, error) {
	controller := &TFServingController{
		grpcHost:             grpcHost,
		restHost:             restHost,
		healthProbeModelName: viper.GetString("healthprobe.modelName"),
	}

	maxGrpcMsgSize := viper.GetInt("serving.grpcMaxMsgSize")
	if maxGrpcMsgSize == 0 {
		maxGrpcMsgSize = 16 * 1024 * 1024
	}
	// Connect to serving
	client, err := grpc.Dial(grpcHost,
		grpc.WithInsecure(),
		grpc.WithTimeout(viper.GetDuration("serving.grpcConfigTimeout")*time.Second),
		grpc.WithConnectParams(grpc.ConnectParams{Backoff: backoff.DefaultConfig}),
		grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(maxGrpcMsgSize), grpc.MaxCallSendMsgSize(maxGrpcMsgSize)),
	)

	if err != nil {
		log.WithError(err).Error("Could not connect to TF serving GRPC")
		return nil, err
	}
	controller.grpcClient = client

	return controller, nil
}

func (server *TFServingController) Close() error {
	return server.grpcClient.Close()
}

func (server *TFServingController) ReloadConfig(models []*Model, tfServingServerModelDir string) error {
	configs := createModelConfig(models, tfServingServerModelDir)

	request := &serving.ReloadConfigRequest{
		Config: &serving.ModelServerConfig{
			Config: &serving.ModelServerConfig_ModelConfigList{
				ModelConfigList: &serving.ModelConfigList{
					Config: configs,
				},
			},
		},
	}

	client := serving.NewModelServiceClient(server.grpcClient)

	log.Debug("Updating TF serving...")
	_, err := client.HandleReloadConfigRequest(context.Background(), request)
	if err != nil {
		log.WithError(err).Error("Error updating tf config")
		return err
	}

	log.Debug("TF config updated successfully")
	return nil
}

func (server *TFServingController) GetModelStatus(model Model) (ModelVersionStatus_State, error) {
	client := serving.NewModelServiceClient(server.grpcClient)

	log.Debug("Getting TF model status...")
	statusRequest := &serving.GetModelStatusRequest{
		ModelSpec: &serving.ModelSpec{
			Name: model.Identifier.ModelName, VersionChoice: &serving.ModelSpec_Version{Version: &wrappers.Int64Value{Value: model.Identifier.Version}},
		},
	}
	resp, err := client.GetModelStatus(context.Background(), statusRequest)
	if err != nil {
		// We suppress the log when retrieving model status for the healthcheck model.
		if model.Identifier.ModelName != server.healthProbeModelName {
			log.WithError(err).Error("Error getting tf serving model status")
		}
		return 0, err
	}

	log.Debug("TF model status received successfully")

	if len(resp.ModelVersionStatus) > 0 {
		return modelVersionStatusStateFromTFState(resp.ModelVersionStatus[0].State), nil
	}
	return 0, errors.New("Model not found")
}

func (server *TFServingController) GetModelStates() ([]ModelVersionStatus_State, error) {
	client := serving.NewModelServiceClient(server.grpcClient)

	log.Debug("Getting TF models status...")
	resp, err := client.GetModelStatus(context.Background(), &serving.GetModelStatusRequest{})
	if err != nil {
		log.WithError(err).Error("Error getting tf serving model status")
		return nil, err
	} else {
		log.Debug("TF model status received successfully")
	}

	models := make([]ModelVersionStatus_State, 0)
	for i := range resp.ModelVersionStatus {
		models = append(models, modelVersionStatusStateFromTFState(resp.ModelVersionStatus[i].State))
	}
	return models, nil
}

func createModelConfig(models []*Model, tfServingServerModelDir string) []*serving.ModelConfig {
	distinctModels := map[string]*serving.FileSystemStoragePathSourceConfig_ServableVersionPolicy_Specific{}
	// Number of configs will be at most len(models) large (also the expected val)
	var configs = make([]*serving.ModelConfig, 0, len(models))
	for _, model := range models {
		// Check for existing model of same name
		existingVersions, exists := distinctModels[model.Identifier.ModelName]
		if exists {
			existingVersions.Versions = append(existingVersions.Versions, model.Identifier.Version)
		} else {
			// Create new config
			modelVersions := &serving.FileSystemStoragePathSourceConfig_ServableVersionPolicy_Specific{
				Versions: []int64{model.Identifier.Version},
			}
			distinctModels[model.Identifier.ModelName] = modelVersions
			configs = append(configs, &serving.ModelConfig{
				Name:          model.Identifier.ModelName,
				BasePath:      path.Join(tfServingServerModelDir, model.Identifier.ModelName),
				ModelPlatform: "tensorflow",
				ModelVersionPolicy: &serving.FileSystemStoragePathSourceConfig_ServableVersionPolicy{
					PolicyChoice: &serving.FileSystemStoragePathSourceConfig_ServableVersionPolicy_Specific_{
						Specific: modelVersions,
					},
				},
			})
		}
	}
	return configs
}

func modelVersionStatusStateFromTFState(state serving.ModelVersionStatus_State) ModelVersionStatus_State {
	switch state {
	case serving.ModelVersionStatus_UNKNOWN:
		return ModelVersionStatus_UNKNOWN
	case serving.ModelVersionStatus_START:
		return ModelVersionStatus_START
	case serving.ModelVersionStatus_LOADING:
		return ModelVersionStatus_LOADING
	case serving.ModelVersionStatus_AVAILABLE:
		return ModelVersionStatus_AVAILABLE
	case serving.ModelVersionStatus_UNLOADING:
		return ModelVersionStatus_UNLOADING
	case serving.ModelVersionStatus_END:
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
