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

	"google.golang.org/grpc/health"
	healthgrpc "google.golang.org/grpc/health/grpc_health_v1"

	pb "github.com/mKaloer/TFServingCache/proto/tensorflow/serving"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	log "github.com/sirupsen/logrus"
	"google.golang.org/grpc"
)

var tfServingRestURLMatch = regexp.MustCompile(`(?i)^/v1/models/(?P<modelName>[^/]+)(/versions/(?P<version>[0-9]+))?`)
var promRequestsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "tfservingcache_proxy_requests_total",
	Help: "The total number of requests",
}, []string{"protocol"})
var promRequestsFailed = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "tfservingcache_proxy_failures_total",
	Help: "The total number of failed requests",
}, []string{"protocol"})

// RestProxy is the proxy for the TFServing HTTP REST api that directs
// api calls to the right nodes
type RestProxy struct {
	RestProxy      *httputil.ReverseProxy
	successCounter *prometheus.CounterVec
	errorCounter   *prometheus.CounterVec
}

// GrpcProxy is the proxy for the TFServing GRPC api that directs
// api calls to the right nodes
type GrpcProxy struct {
	GrpcProxy   *grpc.Server
	serverImpl  *proxyServiceServer
	listener    net.Listener
	healthcheck *health.Server
}

// NewRestProxy creates a new RestProxy for TF Serving
func NewRestProxy(handler func(req *http.Request, modelName string, version string) error) *RestProxy {
	promRequestsTotal.WithLabelValues("rest")
	promRequestsFailed.WithLabelValues("rest")

	director := func(req *http.Request) {
		log.Debugf("Handling URL: %s", req.URL.String())
		matches := tfServingRestURLMatch.FindStringSubmatch(req.URL.String())
		log.Debugf("Model name: '%s' Version: '%s'", matches[1], matches[3])
		err := handler(req, matches[1], matches[3])
		if err != nil {
			promRequestsFailed.WithLabelValues("rest").Inc()
		} else {
			promRequestsFailed.WithLabelValues("rest").Inc()
		}
	}
	h := &RestProxy{
		RestProxy: &httputil.ReverseProxy{Director: director},
	}

	return h
}

// NewGrpcProxy creates a new GrpcProxy for TF Serving
func NewGrpcProxy(clientProvider func(modelName string, version string) (*grpc.ClientConn, error)) *GrpcProxy {
	promRequestsTotal.WithLabelValues("grpc")
	promRequestsFailed.WithLabelValues("grpc")

	server := proxyServiceServer{
		clientProvider: clientProvider,
	}

	proxy := GrpcProxy{
		serverImpl:  &server,
		healthcheck: health.NewServer(),
	}
	return &proxy
}

// Serve returns the HTTP handler function for TF serving REST api proxying
func (handler *RestProxy) Serve() func(http.ResponseWriter, *http.Request) {
	// Wrap proxy in custom function to check for invalid requests
	proxyFun := func(rw http.ResponseWriter, req *http.Request) {
		promRequestsTotal.WithLabelValues("rest").Inc()
		log.Debugf("Handling URL: %s", req.URL.String())
		matches := tfServingRestURLMatch.FindStringSubmatch(req.URL.String())
		if len(matches) == 0 {
			rw.Header().Set("Content-Type", "application/json")
			rw.WriteHeader(http.StatusNotFound)
			json.NewEncoder(rw).Encode(struct {
				Status  string
				Message string
			}{
				Status:  "Error",
				Message: "Not found",
			})
			promRequestsFailed.WithLabelValues("rest").Inc()
			return
		}
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
			promRequestsFailed.WithLabelValues("rest").Inc()
			return
		}
		log.Debugf("Model name: '%s' Version: '%s'", matches[1], matches[3])
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

	healthgrpc.RegisterHealthServer(proxy.GrpcProxy, proxy.healthcheck)

	proxy.GrpcProxy.Serve(lis)
	return nil
}

func (proxy *GrpcProxy) SetHealth(isHealthy bool) {
	if isHealthy {
		proxy.healthcheck.SetServingStatus("", healthgrpc.HealthCheckResponse_SERVING)
	} else {
		proxy.healthcheck.SetServingStatus("", healthgrpc.HealthCheckResponse_NOT_SERVING)
	}
}

// Close stops the grpc proxy ser
func (proxy *GrpcProxy) Close() error {
	err := proxy.listener.Close()
	proxy.GrpcProxy.GracefulStop()
	return err
}

// proxyServiceServer implements the relevant TF serving grpc methods
// and extracts model name and version and forwards the requests to a handler node
type proxyServiceServer struct {
	clientProvider func(modelName string, version string) (*grpc.ClientConn, error)
}

// Classify.
func (server *proxyServiceServer) Classify(ctx context.Context, req *pb.ClassificationRequest) (*pb.ClassificationResponse, error) {
	promRequestsTotal.WithLabelValues("grpc").Inc()
	client, err := server.clientForSpec(req.GetModelSpec())
	if err != nil {
		promRequestsFailed.WithLabelValues("grpc").Inc()
		log.WithError(err).Error("Could not get grpc client")
		return nil, err
	}
	service := pb.NewPredictionServiceClient(client)
	res, err := service.Classify(ctx, req)
	return res, err
}

// Regress.
func (server *proxyServiceServer) Regress(ctx context.Context, req *pb.RegressionRequest) (*pb.RegressionResponse, error) {
	promRequestsTotal.WithLabelValues("grpc").Inc()
	client, err := server.clientForSpec(req.GetModelSpec())
	if err != nil {
		log.WithError(err).Error("Could not get grpc client")
		promRequestsFailed.WithLabelValues("grpc").Inc()
		return nil, err
	}
	service := pb.NewPredictionServiceClient(client)
	res, err := service.Regress(ctx, req)
	return res, err
}

// Predict -- provides access to loaded TensorFlow model.
func (server *proxyServiceServer) Predict(ctx context.Context, req *pb.PredictRequest) (*pb.PredictResponse, error) {
	promRequestsTotal.WithLabelValues("grpc").Inc()
	client, err := server.clientForSpec(req.GetModelSpec())
	if err != nil {
		log.WithError(err).Error("Could not get grpc client")
		promRequestsFailed.WithLabelValues("grpc").Inc()
		return nil, err
	}
	service := pb.NewPredictionServiceClient(client)
	res, err := service.Predict(ctx, req)
	return res, err
}

// MultiInference API for multi-headed models.
func (server *proxyServiceServer) MultiInference(ctx context.Context, req *pb.MultiInferenceRequest) (*pb.MultiInferenceResponse, error) {
	return nil, errors.New("MultiInference not supported")
}

// GetModelMetadata - provides access to metadata for loaded models.
func (server *proxyServiceServer) GetModelMetadata(ctx context.Context, req *pb.GetModelMetadataRequest) (*pb.GetModelMetadataResponse, error) {
	promRequestsTotal.WithLabelValues("grpc").Inc()
	client, err := server.clientForSpec(req.GetModelSpec())
	if err != nil {
		log.WithError(err).Error("Could not get grpc client")
		promRequestsFailed.WithLabelValues("grpc").Inc()
		return nil, err
	}
	service := pb.NewPredictionServiceClient(client)
	res, err := service.GetModelMetadata(ctx, req)
	return res, err
}

func (server *proxyServiceServer) SessionRun(ctx context.Context, req *pb.SessionRunRequest) (*pb.SessionRunResponse, error) {
	promRequestsTotal.WithLabelValues("grpc").Inc()
	client, err := server.clientForSpec(req.GetModelSpec())
	if err != nil {
		log.WithError(err).Error("Could not get grpc client")
		promRequestsFailed.WithLabelValues("grpc").Inc()
		return nil, err
	}
	service := pb.NewSessionServiceClient(client)
	res, err := service.SessionRun(ctx, req)
	return res, err
}

func (server *proxyServiceServer) clientForSpec(modelSpec *pb.ModelSpec) (*grpc.ClientConn, error) {
	modelName := modelSpec.GetName()
	modelVersion := strconv.FormatInt(modelSpec.GetVersion().GetValue(), 10)
	return server.clientProvider(modelName, modelVersion)
}
