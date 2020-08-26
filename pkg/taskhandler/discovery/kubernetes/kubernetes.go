package kubernetes

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"strings"
	"time"

	"github.com/mKaloer/TFServingCache/pkg/taskhandler"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/viper"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sWatch "k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

type K8sPodInfo struct {
	PodName        string
	ReplicaSetName string
}

type K8sDiscoveryService struct {
	ListUpdatedChans map[string]chan []taskhandler.ServingService
	Namespace        string
	PodInfo          K8sPodInfo
	LabelSelector    string
	K8sClient        *kubernetes.Clientset
}

func NewDiscoveryService() (*K8sDiscoveryService, error) {
	cfg, err := rest.InClusterConfig()
	if err != nil {
		return nil, err
	}

	clientset, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, err
	}
	namespace, err := k8sNamespace()
	if err != nil {
		return nil, err
	}
	podName, err := os.Hostname()
	if err != nil {
		return nil, err
	}

	podInfo, err := k8sFindPodInfo(clientset, namespace, podName)
	if err != nil {
		return nil, err
	}

	labelSelector := viper.GetString("serviceDiscovery.k8s.labelSelector")
	service := &K8sDiscoveryService{
		ListUpdatedChans: make(map[string]chan []taskhandler.ServingService, 0),
		K8sClient:        clientset,
		Namespace:        namespace,
		PodInfo:          *podInfo,
		LabelSelector:    labelSelector,
	}

	return service, nil
}

func (service *K8sDiscoveryService) RegisterService() error {
	updaterFunc := func() error {

		watch, err := service.K8sClient.CoreV1().Endpoints(service.Namespace).Watch(context.TODO(), metav1.ListOptions{
			LabelSelector: service.LabelSelector,
		})
		if err != nil {
			return err
		}

		nodeMap := make(map[string]taskhandler.ServingService, 0)
		for {
			updates := <-watch.ResultChan()
			endpoints, ok := updates.Object.(*v1.Endpoints)
			isUpdated := false
			if !ok {
				log.Error("Error reading object from K8S")
				time.Sleep(5 * time.Second)
			} else {
				for _, sub := range endpoints.Subsets {
					if updates.Type == k8sWatch.Added || updates.Type == k8sWatch.Modified {
						// Add address can be either Rest or gRPC address
						for addrIdx, addr := range sub.Addresses {
							service, exists := nodeMap[addr.IP]
							port := sub.Ports[addrIdx]
							grpcPort := 0
							httpPort := 0
							if port.Name == "grpc" {
								grpcPort = int(port.Port)
							}
							if port.Name == "http" {
								httpPort = int(port.Port)
							}

							if !exists {
								// Add node
								nodeMap[addr.Hostname] = taskhandler.ServingService{
									Host:     addr.IP,
									GrpcPort: grpcPort,
									RestPort: httpPort,
								}
								isUpdated = true
							} else {
								// Update node
								if grpcPort > 0 && grpcPort != service.GrpcPort {
									service.GrpcPort = grpcPort
								}
								if httpPort > 0 && httpPort != service.RestPort {
									service.RestPort = httpPort
								}
								if addr.IP != service.Host {
									service.Host = addr.IP
								}
							}
						}
					} else if updates.Type == k8sWatch.Deleted {
						for _, addr := range sub.Addresses {
							delete(nodeMap, addr.Hostname)
							isUpdated = true
						}
					}
				}
				if isUpdated {
					memberList := make([]taskhandler.ServingService, 0, len(nodeMap))
					for k := range nodeMap {
						memberList = append(memberList, nodeMap[k])
						log.Debugf("Found node: %s: %s", k, nodeMap[k])
					}
					for ch := range service.ListUpdatedChans {
						service.ListUpdatedChans[ch] <- memberList
					}

				}
			}
		}
	}
	go updaterFunc()

	return nil
}

func (service *K8sDiscoveryService) UnregisterService() error {
	// Let k8s handle this by itself
	return nil
}

func (service *K8sDiscoveryService) AddNodeListUpdated(key string, sub chan []taskhandler.ServingService) {
	service.ListUpdatedChans[key] = sub
}

func (service *K8sDiscoveryService) RemoveNodeListUpdated(key string) {
	delete(service.ListUpdatedChans, key)
}

// Gets namespace for current pod
// https://github.com/gkarthiks/k8s-discovery/blob/8d8ac6a89d279773603f1a73a8401f2d1d0cf9e7/discovery.go#L93
func k8sNamespace() (string, error) {
	data, err := ioutil.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/namespace")
	if err == nil {
		if ns := strings.TrimSpace(string(data)); len(ns) > 0 {
			return ns, nil
		} else {
			return "", err
		}
	} else {
		return "", err
	}
}

// Finds the name of the ReplicaSet for the given pod
func k8sFindPodInfo(k8sClient *kubernetes.Clientset, namespace string, podName string) (*K8sPodInfo, error) {
	// Find current service
	pod, err := k8sClient.CoreV1().Pods(namespace).Get(context.TODO(), podName, metav1.GetOptions{})
	if err != nil {
		log.WithError(err).Error("Could not find current pod")
		return nil, err
	}
	var replicasetName *string = nil
	for _, ref := range pod.OwnerReferences {
		if ref.Kind == "ReplicaSet" && *ref.Controller {
			replicasetName = &ref.Name
			break
		}
	}
	if replicasetName == nil {
		return nil, fmt.Errorf("Could not find ReplicaSet for pod: %s", podName)
	}

	return &K8sPodInfo{
		PodName:        podName,
		ReplicaSetName: *replicasetName,
	}, nil
}
