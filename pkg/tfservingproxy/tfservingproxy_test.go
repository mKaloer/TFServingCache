package tfservingproxy

import (
	"context"
	"net"
	"net/http"
	"testing"

	"github.com/golang/protobuf/ptypes/wrappers"
	pb "github.com/mKaloer/TFServingCache/proto/tensorflow/serving"
	log "github.com/sirupsen/logrus"
	example "github.com/tensorflow/tensorflow/tensorflow/go/core/example"
	"google.golang.org/grpc"
)

type httpMockServer struct {
	proxyServer *http.Server
	modelServer *http.Server
}

type grpcMockServer struct {
	proxyServer *GrpcProxy
	modelServer *grpc.Server
}

func setupHttpTestCache(proxyCallback func(modelName string, version string), modelCallback func()) *httpMockServer {
	handlerMock := func(req *http.Request, modelName string, version string) error {
		proxyCallback(modelName, version)
		req.URL.Path = "http://localhost:8089/foobar"
		req.URL.Scheme = "http"
		req.URL.Host = "localhost:8089"
		return nil
	}
	proxy := NewRestProxy(handlerMock)

	proxyHandler := http.NewServeMux()
	proxyHandler.HandleFunc("/", proxy.Serve())

	proxyServer := &http.Server{Addr: ":8088", Handler: proxyHandler}
	go func() {
		if err := proxyServer.ListenAndServe(); err != http.ErrServerClosed {
			log.Fatalf("Err: %v", err)
		}
	}()

	modelServerHandler := http.NewServeMux()

	modelServerHandler.HandleFunc("/", func(http.ResponseWriter, *http.Request) {
		modelCallback()
	})
	modelServer := &http.Server{Addr: ":8089", Handler: modelServerHandler}
	go func() {
		if err := modelServer.ListenAndServe(); err != http.ErrServerClosed {
			log.Fatalf("Err: %v", err)
		}
	}()

	return &httpMockServer{
		proxyServer,
		modelServer,
	}
}

func (m *httpMockServer) shutdown() {
	m.proxyServer.Shutdown(context.TODO())
	m.modelServer.Shutdown(context.TODO())
}

func setupGrpcTestCache(proxyCallback func(modelName string, version string), modelCallback func(modelName string, version int64)) *grpcMockServer {

	modelServer := grpc.NewServer()
	lis, err := net.Listen("tcp", ":8891")
	if err != nil {
		log.Fatalf("Err: %v", err)
	}
	server := mockProxyServiceServer{modelCallback}
	pb.RegisterPredictionServiceServer(modelServer, &server)
	go func() {
		modelServer.Serve(lis)
	}()

	handlerMock := func(modelName string, version string) (*grpc.ClientConn, error) {
		proxyCallback(modelName, version)
		// No connection exists - swap to write lock and connect
		conn, err := grpc.Dial(":8891",
			grpc.WithInsecure())
		if err != nil {
			log.Fatalf("Err: %v", err)
		}
		return conn, err
	}

	grpcProxy := NewGrpcProxy(handlerMock)
	go func() {
		if err := grpcProxy.Listen(8890); err != nil {
			log.Fatalf("Err: %v", err)
		}
	}()

	return &grpcMockServer{
		grpcProxy,
		modelServer,
	}
}

func (m *grpcMockServer) shutdown() {
	m.proxyServer.Close()
	m.modelServer.GracefulStop()
}

func TestHttpProxyParsesUrl(t *testing.T) {
	modelHandlerCalled := false
	proxyCallback := func(modelName string, version string) {
		modelHandlerCalled = true
		if modelName != "foobar" {
			t.Errorf("Wrong model name")
		}
		if version != "42" {
			t.Errorf("Wrong model version")
		}
	}
	modelServerCalled := false
	modelServerCallback := func() {
		modelServerCalled = true
	}
	mockServer := setupHttpTestCache(proxyCallback, modelServerCallback)
	_, err := http.Get("http://localhost:8088/v1/models/foobar/versions/42")
	if err != nil {
		log.Fatalln(err)
	}

	mockServer.shutdown()
	if !modelHandlerCalled {
		t.Errorf("Model handler (proxy) not called")
	}

	if !modelServerCalled {
		t.Errorf("Model server not called")
	}
}

func TestHttpProxyInvalidUrlCausesErr(t *testing.T) {
	modelHandlerCalled := false
	proxyCallback := func(modelName string, version string) {
		modelHandlerCalled = true
	}
	modelServerCalled := false
	modelServerCallback := func() {
		modelServerCalled = true
	}
	mockServer := setupHttpTestCache(proxyCallback, modelServerCallback)
	resp, err := http.Get("http://localhost:8088/v1/thisisabadrequest/foobar/versions/42")
	if err != nil {
		log.Fatalln(err)
	}

	mockServer.shutdown()

	if resp.StatusCode != 404 {
		t.Errorf("Expected status code 404 on invalid url")
	}

	if modelHandlerCalled {
		t.Errorf("Model handler (proxy) called on bad URL")
	}

	if modelServerCalled {
		t.Errorf("Model server called on bad url")
	}
}

func TestHttpProxyNoVersionCodeCausesErr(t *testing.T) {
	modelHandlerCalled := false
	proxyCallback := func(modelName string, version string) {
		modelHandlerCalled = true
	}
	modelServerCalled := false
	modelServerCallback := func() {
		modelServerCalled = true
	}
	mockServer := setupHttpTestCache(proxyCallback, modelServerCallback)
	resp, err := http.Get("http://localhost:8088/v1/models/foobar")
	if err != nil {
		log.Fatalln(err)
	}

	mockServer.shutdown()

	if resp.StatusCode != 400 {
		t.Errorf("Expected status code 400 on missing version url")
	}

	if modelHandlerCalled {
		t.Errorf("Model handler (proxy) called on bad URL")
	}

	if modelServerCalled {
		t.Errorf("Model server called on bad url")
	}
}

func TestGrpcProxyParsesRequest(t *testing.T) {
	modelHandlerCalled := false
	proxyCallback := func(modelName string, version string) {
		modelHandlerCalled = true
		if modelName != "foobar" {
			t.Errorf("Wrong model name")
		}
		if version != "42" {
			t.Errorf("Wrong model version")
		}
	}
	modelServerCalled := false
	modelServerCallback := func(modelName string, version int64) {
		modelServerCalled = true
		if modelName != "foobar" {
			t.Errorf("Wrong model name")
		}
		if version != 42 {
			t.Errorf("Wrong model version")
		}
	}
	mockServer := setupGrpcTestCache(proxyCallback, modelServerCallback)
	sendGrpcModelRequest("foobar", 42)

	mockServer.shutdown()
	if !modelHandlerCalled {
		t.Errorf("Model handler (proxy) not called")
	}

	if !modelServerCalled {
		t.Errorf("Model server not called")
	}
}

func sendGrpcModelRequest(modelName string, version int64) {
	conn, err := grpc.Dial(":8890", grpc.WithInsecure())
	if err != nil {
		log.WithError(err).Panic("Error")
	}
	service := pb.NewPredictionServiceClient(conn)
	_, err = service.Classify(context.Background(),
		&pb.ClassificationRequest{
			ModelSpec: &pb.ModelSpec{
				Name:          modelName,
				VersionChoice: &pb.ModelSpec_Version{Version: &wrappers.Int64Value{Value: version}},
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
}

type mockProxyServiceServer struct {
	modelServerCallback func(modelName string, modelVersion int64)
}

// Classify.
func (server *mockProxyServiceServer) Classify(ctx context.Context, req *pb.ClassificationRequest) (*pb.ClassificationResponse, error) {
	spec := req.GetModelSpec()
	server.modelServerCallback(spec.GetName(), spec.GetVersion().Value)
	return &pb.ClassificationResponse{
		ModelSpec: req.GetModelSpec(),
		Result: &pb.ClassificationResult{
			Classifications: nil,
		},
	}, nil
}

// Regress.
func (server *mockProxyServiceServer) Regress(ctx context.Context, req *pb.RegressionRequest) (*pb.RegressionResponse, error) {
	return nil, nil
}

// Predict -- provides access to loaded TensorFlow model.
func (server *mockProxyServiceServer) Predict(ctx context.Context, req *pb.PredictRequest) (*pb.PredictResponse, error) {
	return nil, nil
}

// MultiInference API for multi-headed models.
func (server *mockProxyServiceServer) MultiInference(ctx context.Context, req *pb.MultiInferenceRequest) (*pb.MultiInferenceResponse, error) {
	return nil, nil
}

// GetModelMetadata - provides access to metadata for loaded models.
func (server *mockProxyServiceServer) GetModelMetadata(ctx context.Context, req *pb.GetModelMetadataRequest) (*pb.GetModelMetadataResponse, error) {
	return nil, nil
}
