package etcd

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/mKaloer/TFServingCache/pkg/taskhandler"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/viper"
	"go.etcd.io/etcd/clientv3"
)

type EtcdDiscoveryService struct {
	ListUpdatedChans map[string]chan []taskhandler.ServingService
	ServiceName      string
	ServiceId        string
	EtcdClient       *clientv3.Client
	ttl              time.Duration
	HealthCheckFun   func() (bool, error)
	serviceKey       string
	outboundIp       string
}

func NewDiscoveryService(healthCheck func() (bool, error)) (*EtcdDiscoveryService, error) {
	cfg := clientv3.Config{
		Endpoints:   viper.GetStringSlice("serviceDiscovery.endpoints"),
		DialTimeout: 5 * time.Second,
		Username:    viper.GetString("serviceDiscovery.authorization.username"),
		Password:    viper.GetString("serviceDiscovery.authorization.password"),
	}
	c, err := clientv3.New(cfg)
	if err != nil {
		return nil, err
	}

	ttl := viper.GetDuration("serviceDiscovery.heartbeatTTL") * time.Second
	ip := getOutboundIP().String()
	service := &EtcdDiscoveryService{
		ListUpdatedChans: make(map[string]chan []taskhandler.ServingService, 0),
		EtcdClient:       c,
		ttl:              ttl,
		ServiceName:      viper.GetString("serviceDiscovery.serviceName"),
		ServiceId:        uuid.New().String(),
		HealthCheckFun:   healthCheck,
		outboundIp:       ip,
	}

	service.serviceKey = fmt.Sprintf("/service/%s/%s", service.ServiceName, service.ServiceId)

	return service, nil
}

func (service *EtcdDiscoveryService) RegisterService() error {
	go service.updateTTL(service.HealthCheckFun)
	updaterFunc := func() {
		watchChan := service.EtcdClient.Watch(context.Background(), "/service/"+service.ServiceName, clientv3.WithPrefix())
		nodeMap := make(map[string]string, 0)
		for {
			updates := <-watchChan
			isUpdated := false
			if updates.Err() != nil {
				log.WithError(updates.Err()).Error("Error reading channel from etcd")
				time.Sleep(5 * time.Second)
			} else {
				for k := range updates.Events {
					event := updates.Events[k]
					keyStr := string(event.Kv.Key)
					if event.IsCreate() || event.IsModify() {
						val, exists := nodeMap[keyStr]
						valStr := string(event.Kv.Value)
						if !exists || val != string(event.Kv.Value) {
							// Update node
							nodeMap[keyStr] = valStr
							isUpdated = true
						}
					} else if event.Type == clientv3.EventTypeDelete {
						// Delete node
						delete(nodeMap, keyStr)
						isUpdated = true
					}
				}
				if isUpdated {
					memberList := make([]taskhandler.ServingService, 0, len(nodeMap))
					for k := range nodeMap {
						serviceParts := strings.Split(nodeMap[k], ":")
						restPort, err := strconv.Atoi(serviceParts[1])
						if err != nil {
							log.WithError(err).Errorf("Invalid rest port: %s", serviceParts[1])
						}
						grpcPort, err := strconv.Atoi(serviceParts[2])
						if err != nil {
							log.WithError(err).Errorf("Invalid grpc port: %s", serviceParts[2])
						}
						memberList = append(memberList, taskhandler.ServingService{
							Host:     serviceParts[0],
							RestPort: restPort,
							GrpcPort: grpcPort,
						})
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

func (service *EtcdDiscoveryService) UnregisterService() error {
	_, err := service.EtcdClient.KV.Delete(context.Background(), service.serviceKey)
	if err != nil {
		log.WithError(err).Error("Could not set etc.d key")
	}
	return err
}

func (service *EtcdDiscoveryService) AddNodeListUpdated(key string, sub chan []taskhandler.ServingService) {
	service.ListUpdatedChans[key] = sub
}

func (service *EtcdDiscoveryService) RemoveNodeListUpdated(key string) {
	delete(service.ListUpdatedChans, key)
}

func (service *EtcdDiscoveryService) updateTTL(check func() (bool, error)) {
	ticker := time.NewTicker(service.ttl / 2)
	restPort := viper.GetInt("cacheRestPort")
	grpcPort := viper.GetInt("cacheGrpcPort")
	for range ticker.C {
		lease, err := service.EtcdClient.Lease.Grant(context.Background(), int64(service.ttl.Seconds()))
		if err != nil {
			log.WithError(err).Error("Could not set etc.d key")
		}
		_, err = service.EtcdClient.KV.Put(context.Background(), service.serviceKey, fmt.Sprintf("%s:%d:%d", service.outboundIp, restPort, grpcPort), clientv3.WithLease(lease.ID))
		if err != nil {
			log.WithError(err).Error("Could not set etc.d key")
		}
	}
}

// Get preferred outbound ip of this machine
// Source: https://stackoverflow.com/questions/23558425/how-do-i-get-the-local-ip-address-in-go
func getOutboundIP() net.IP {
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		if viper.GetBool("serviceDiscovery.allowLocalhost") {
			return net.ParseIP("127.0.0.1")
		} else {
			log.Fatal("Could not get unboind ip: %v", err)
		}
	}
	defer conn.Close()

	localAddr := conn.LocalAddr().(*net.UDPAddr)

	return localAddr.IP
}
