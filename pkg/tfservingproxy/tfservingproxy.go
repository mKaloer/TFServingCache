package tfservingproxy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"
	"regexp"
	"strconv"

	pb "github.com/mKaloer/TFServingCache/proto/tensorflow/serving"
	log "github.com/sirupsen/logrus"
	"google.golang.org/grpc"
)

var tfServingRestURLMatch = regexp.MustCompile(`(?i)^/v1/models/(?P<modelName>[a-z0-9]+)(/versions/(?P<version>[0-9]+))?`)

// RestProxy is the proxy for the TFServing HTTP REST api that directs
// api calls to the right nodes
type RestProxy struct {
	RestProxy *httputil.ReverseProxy
}

// GrpcProxy is the proxy for the TFServing GRPC api that directs
// api calls to the right nodes
type GrpcProxy struct {
	GrpcProxy  *grpc.Server
	serverImpl *proxyServiceServer
	listener   net.Listener
}

// NewRestProxy creates a new RestProxy for TF Serving
func NewRestProxy(handler func(req *http.Request, modelName string, version string)) *RestProxy {
	director := func(req *http.Request) {
		log.Debugf("Handling URL: %s", req.URL.String())
		matches := tfServingRestURLMatch.FindStringSubmatch(req.URL.String())
		log.Debugf("Model name: '%s' Version: '%s'", matches[1], matches[3])
		handler(req, matches[1], matches[3])
	}
	h := &RestProxy{
		RestProxy: &httputil.ReverseProxy{Director: director},
	}

	return h
}
// NewGrpcProxy creates a new GrpcProxy for TF Serving
func NewGrpcProxy(clientProvider func(modelName string, version string) (*grpc.ClientConn, error)) *GrpcProxy {
	server := proxyServiceServer{
		clientProvider: clientProvider,
	}

	proxy := GrpcProxy{
		serverImpl: &server,
	}
	return &proxy
}

// Serve returns the HTTP handler function for TF serving REST api proxying
func (handler *RestProxy) Serve() func(http.ResponseWriter, *http.Request) {
	// Wrap proxy in custom function to check for invalid requests
	proxyFun := func(rw http.ResponseWriter, req *http.Request) {
		log.Debugf("Handling URL: %s", req.URL.String())
		matches := tfServingRestURLMatch.FindStringSubmatch(req.URL.String())
		log.Debugf("Model name: '%s' Version: '%s'", matches[1], matches[3])
		if matches[3] == "" {
			rw.Header().Set("Content-Type", "application/json")
			rw.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(rw).Encode(struct {
				Status  string
				Message string
			}{
				Status:  "Error",
				Message: "Model version must be provided",
			})
			return
		}
		handler.RestProxy.ServeHTTP(rw, req)
	}
	return proxyFun
}

// Listen starts the grpc server that proxies TF serving GRPC api calls
func (proxy *GrpcProxy) Listen(port int) error {
	proxy.GrpcProxy = grpc.NewServer()
	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return err
	}
	proxy.listener = lis
	pb.RegisterPredictionServiceServer(proxy.GrpcProxy, proxy.serverImpl)
	pb.RegisterSessionServiceServer(proxy.GrpcProxy, proxy.serverImpl)
	proxy.GrpcProxy.Serve(lis)
	return nil
}

// Close stops the grpc proxy ser
func (proxy *GrpcProxy) Close() error {
	return proxy.listener.Close()
}


// proxyServiceServer implements the relevant TF serving grpc methods
// and extracts model name and version and forwards the requests to a handler node
type proxyServiceServer struct {
	clientProvider func(modelName string, version string) (*grpc.ClientConn, error)
}

// Classify.
func (server *proxyServiceServer) Classify(ctx context.Context, req *pb.ClassificationRequest) (*pb.ClassificationResponse, error) {
	client, err := server.clientForSpec(req.GetModelSpec())
	if err != nil {
		log.WithError(err).Error("Could not get grpc client")
		return nil, err
	}
	service := pb.NewPredictionServiceClient(client)
	return service.Classify(ctx, req)
}

// Regress.
func (server *proxyServiceServer) Regress(ctx context.Context, req *pb.RegressionRequest) (*pb.RegressionResponse, error) {
	client, err := server.clientForSpec(req.GetModelSpec())
	if err != nil {
		log.WithError(err).Error("Could not get grpc client")
		return nil, err
	}
	service := pb.NewPredictionServiceClient(client)
	return service.Regress(ctx, req)
}

// Predict -- provides access to loaded TensorFlow model.
func (server *proxyServiceServer) Predict(ctx context.Context, req *pb.PredictRequest) (*pb.PredictResponse, error) {
	client, err := server.clientForSpec(req.GetModelSpec())
	if err != nil {
		log.WithError(err).Error("Could not get grpc client")
		return nil, err
	}
	service := pb.NewPredictionServiceClient(client)
	return service.Predict(ctx, req)
}

// MultiInference API for multi-headed models.
func (server *proxyServiceServer) MultiInference(ctx context.Context, req *pb.MultiInferenceRequest) (*pb.MultiInferenceResponse, error) {
	return nil, errors.New("MultiInference not supported")
}

// GetModelMetadata - provides access to metadata for loaded models.
func (server *proxyServiceServer) GetModelMetadata(ctx context.Context, req *pb.GetModelMetadataRequest) (*pb.GetModelMetadataResponse, error) {
	client, err := server.clientForSpec(req.GetModelSpec())
	if err != nil {
		log.WithError(err).Error("Could not get grpc client")
		return nil, err
	}
	service := pb.NewPredictionServiceClient(client)
	return service.GetModelMetadata(ctx, req)
}

func (server *proxyServiceServer) SessionRun(ctx context.Context, req *pb.SessionRunRequest) (*pb.SessionRunResponse, error) {
	client, err := server.clientForSpec(req.GetModelSpec())
	if err != nil {
		log.WithError(err).Error("Could not get grpc client")
		return nil, err
	}
	service := pb.NewSessionServiceClient(client)
	return service.SessionRun(ctx, req)
}

func (server *proxyServiceServer) clientForSpec(modelSpec *pb.ModelSpec) (*grpc.ClientConn, error) {
	modelName := modelSpec.GetName()
	modelVersion := strconv.FormatInt(modelSpec.GetVersion().GetValue(), 10)
	return server.clientProvider(modelName, modelVersion)
}
