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
	"k8s.io/apimachinery/pkg/labels"
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
	// Selector that defines the k8s service
	FieldSelector string
	K8sClient     *kubernetes.Clientset
	// Name of gRPC cache port in k8s service
	grpcCachePortName string
	// Name of REST cache port in k8s service
	httpCachePortName string
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

	fieldSelector := viper.GetStringMapString("serviceDiscovery.k8s.fieldSelector")
	fieldSelectorString := labels.SelectorFromSet(fieldSelector).String()

	service := &K8sDiscoveryService{
		ListUpdatedChans:  make(map[string]chan []taskhandler.ServingService, 0),
		K8sClient:         clientset,
		Namespace:         namespace,
		PodInfo:           *podInfo,
		FieldSelector:     fieldSelectorString,
		grpcCachePortName: viperTryGetString("serviceDiscovery.k8s.portNames.grpcCache", "grpccache"),
		httpCachePortName: viperTryGetString("serviceDiscovery.k8s.portNames.httpCache", "httpcache"),
	}

	return service, nil
}

func (service *K8sDiscoveryService) RegisterService() error {
	updaterFunc := func() error {
		watch, err := service.K8sClient.CoreV1().Endpoints(service.Namespace).Watch(context.TODO(), metav1.ListOptions{
			FieldSelector: service.FieldSelector,
		})
		if err != nil {
			log.WithError(err).Error("Error subscribing to k8s")
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
				if updates.Type == k8sWatch.Added || updates.Type == k8sWatch.Modified {
					for _, sub := range endpoints.Subsets {
						// Entire list of nodes is sent every time - so keep track of delta
						nodeMap = make(map[string]taskhandler.ServingService, 0)
						for _, addr := range sub.Addresses {
							grpcCachePort := 0
							httpCachePort := 0
							for _, port := range sub.Ports {
								if port.Name == service.grpcCachePortName {
									grpcCachePort = int(port.Port)
								} else if port.Name == service.httpCachePortName {
									httpCachePort = int(port.Port)
								}
							}

							nodeMap[addr.IP] = taskhandler.ServingService{
								Host:     addr.IP,
								GrpcPort: grpcCachePort,
								RestPort: httpCachePort,
							}
							isUpdated = true
						}
					}
				} else if updates.Type == k8sWatch.Deleted {
					// Endpoint deleted - no nodes available
					nodeMap = make(map[string]taskhandler.ServingService, 0)
					isUpdated = true
				}
				if isUpdated {
					memberList := make([]taskhandler.ServingService, 0, len(nodeMap))
					for k := range nodeMap {
						memberList = append(memberList, nodeMap[k])
						log.WithFields(log.Fields{
							"host":     k,
							"grpcPort": nodeMap[k].GrpcPort,
							"httpPort": nodeMap[k].RestPort,
						}).Debug("Found node")
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

func viperTryGetString(key string, defaultVal string) string {
	if viper.IsSet(key) {
		return viper.GetString(key)
	}
	return defaultVal
}
