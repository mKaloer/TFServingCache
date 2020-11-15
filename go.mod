module github.com/mKaloer/TFServingCache

go 1.13

require (
	github.com/aws/aws-sdk-go v1.28.6
	github.com/coreos/etcd v3.3.18+incompatible // indirect
	github.com/golang/protobuf v1.3.2
	github.com/google/go-cmp v0.4.0
	github.com/google/uuid v1.1.1
	github.com/hashicorp/consul/api v1.3.0
	github.com/otiai10/copy v1.0.2
	github.com/prometheus/client_golang v1.5.0
	github.com/prometheus/client_model v0.2.0
	github.com/prometheus/common v0.9.1
	github.com/sirupsen/logrus v1.4.2
	github.com/spf13/viper v1.6.1
	github.com/tensorflow/tensorflow/tensorflow/go/core v0.0.0-00010101000000-000000000000
	go.etcd.io/etcd v3.3.18+incompatible
	google.golang.org/grpc v1.26.0
	k8s.io/api v0.18.3
	k8s.io/apimachinery v0.18.3
	k8s.io/client-go v0.18.3
	stathat.com/c/consistent v1.0.0
)

replace github.com/tensorflow/tensorflow/tensorflow/go/core => ./proto/tensorflow/core
