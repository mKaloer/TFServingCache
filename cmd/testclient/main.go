package main

import (
	"context"
	"github.com/golang/protobuf/ptypes/wrappers"
	pb "github.com/mKaloer/TFServingCache/proto/tensorflow/serving"
	log "github.com/sirupsen/logrus"
	example "github.com/tensorflow/tensorflow/tensorflow/go/core/example"
	"google.golang.org/grpc"
)

func main() {
	conn, err := grpc.Dial(":8100", grpc.WithInsecure())
	if err != nil {
		log.WithError(err).Panic("Error")
	}
	service := pb.NewPredictionServiceClient(conn)

	res, err := service.Classify(context.Background(),
		&pb.ClassificationRequest{
			ModelSpec: &pb.ModelSpec{
				Name:          "model1",
				VersionChoice: &pb.ModelSpec_Version{Version: &wrappers.Int64Value{Value: 123}},
			},
			Input: &pb.Input{
				Kind: &pb.Input_ExampleList{
					ExampleList: &pb.ExampleList{
						Examples: []*example.Example{
							&example.Example{
								Features: &example.Features{},
							},
						},
					},
				},
			},
		})

	if err != nil {
		log.WithError(err).Panicf("Error classifying")
	}
	log.Info(res.String())
}
