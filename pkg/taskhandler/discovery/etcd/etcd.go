package etcd

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/viper"
	"go.etcd.io/etcd/clientv3"
)

type EtcdDiscoveryService struct {
	ListUpdatedChans map[string]chan []string
	ServiceName      string
	ServiceId        string
	EtcdClient       *clientv3.Client
	ttl              time.Duration
	HealthCheckFun   func() (bool, error)
	serviceKey       string
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
	service := &EtcdDiscoveryService{
		ListUpdatedChans: make(map[string]chan []string, 0),
		EtcdClient:       c,
		ttl:              ttl,
		ServiceName:      viper.GetString("serviceDiscovery.serviceName"),
		ServiceId:        uuid.New().String(),
		HealthCheckFun:   healthCheck,
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
					memberList := make([]string, 0, len(nodeMap))
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

func (service *EtcdDiscoveryService) UnregisterService() error {
	_, err := service.EtcdClient.KV.Delete(context.Background(), service.serviceKey)
	if err != nil {
		log.WithError(err).Error("Could not set etc.d key")
	}
	return err
}

func (service *EtcdDiscoveryService) AddNodeListUpdated(key string, sub chan []string) {
	service.ListUpdatedChans[key] = sub
}

func (service *EtcdDiscoveryService) RemoveNodeListUpdated(key string) {
	delete(service.ListUpdatedChans, key)
}

func (service *EtcdDiscoveryService) updateTTL(check func() (bool, error)) {
	ticker := time.NewTicker(service.ttl / 2)
	port := viper.GetInt("cacheRestPort")
	for range ticker.C {
		lease, err := service.EtcdClient.Lease.Grant(context.Background(), int64(service.ttl.Seconds()))
		if err != nil {
			log.WithError(err).Error("Could not set etc.d key")
		}
		_, err = service.EtcdClient.KV.Put(context.Background(), service.serviceKey, fmt.Sprintf("myip:%d", port), clientv3.WithLease(lease.ID))
		if err != nil {
			log.WithError(err).Error("Could not set etc.d key")
		}
	}
}
