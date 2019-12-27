package cachemanager

import (
	"context"
	log "github.com/sirupsen/logrus"
	pb "tensorflow_serving/apis"
	config "tensorflow_serving/config"
	storage_path "tensorflow_serving/sources/storage_path"

	"google.golang.org/grpc"
)

func ReloadConfig(models []*Model) {
	request := &pb.ReloadConfigRequest{
		Config: &config.ModelServerConfig{
			Config: &config.ModelServerConfig_ModelConfigList{
				ModelConfigList: &config.ModelConfigList{
					Config: []*config.ModelConfig{
						&config.ModelConfig{
							Name:          "foo",
							BasePath:      "/models/foo",
							ModelPlatform: "tensorflow",
							ModelVersionPolicy: &storage_path.FileSystemStoragePathSourceConfig_ServableVersionPolicy{
								PolicyChoice: &storage_path.FileSystemStoragePathSourceConfig_ServableVersionPolicy_Specific_{
									Specific: &storage_path.FileSystemStoragePathSourceConfig_ServableVersionPolicy_Specific{
										Versions: []int64{123},
									},
								},
							},
						},
						/*&config.ModelConfig{
							Name:     "bar",
							BasePath: "bar2",
						},*/
					},
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
