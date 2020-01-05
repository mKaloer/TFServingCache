package consul

import (
	"fmt"
	"time"

	"github.com/hashicorp/consul/api"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/viper"
)

type ConsulDiscoveryService struct {
	ListUpdatedChans map[string]chan []string
	ServiceName      string
	ServiceID        string
	ConsulClient     *api.Client
	ttl              time.Duration
	HealthCheckFun   func() (bool, error)
}

func NewDiscoveryService(healthCheck func() (bool, error)) (*ConsulDiscoveryService, error) {
	config := api.DefaultConfig()
	client, err := api.NewClient(config)
	if err != nil {
		return nil, err
	}

	ttl := viper.GetDuration("serviceDiscovery.heartbeatTTL") * time.Second

	serviceId := viper.GetString("serviceDiscovery.serviceId")
	if serviceId == "" {
		serviceId = viper.GetString("serviceDiscovery.serviceName")
	}

	c := &ConsulDiscoveryService{
		ListUpdatedChans: make(map[string]chan []string, 0),
		ConsulClient:     client,
		ttl:              ttl,
		ServiceName:      viper.GetString("serviceDiscovery.serviceName"),
		ServiceID:        serviceId,
		HealthCheckFun:   healthCheck,
	}

	return c, nil
}

func (consul *ConsulDiscoveryService) RegisterService() error {
	agent := consul.ConsulClient.Agent()
	serviceDef := &api.AgentServiceRegistration{
		Name: consul.ServiceName,
		ID:   consul.ServiceID,
		Port: viper.GetInt("cacheRestPort"),
		Check: &api.AgentServiceCheck{
			TTL:                            consul.ttl.String(),
			DeregisterCriticalServiceAfter: (consul.ttl * 100).String(),
		},
	}

	if err := agent.ServiceRegister(serviceDef); err != nil {
		log.WithError(err).Errorf("Could not register consul service")
		return err
	}

	go consul.updateTTL(consul.HealthCheckFun)
	updaterFunc := func() {
		for {
			res, _, err := consul.ConsulClient.Health().Service(consul.ServiceName, "", true, &api.QueryOptions{})
			if err != nil {
				log.WithError(err).Error("Error getting services")
			} else {
				passingNodes := make([]string, 0, len(res))
				for k := range res {
					id := res[k].Service.ID
					port := res[k].Service.Port
					addr := res[k].Service.Address
					if addr == "" {
						// Fallback to node addr
						addr = res[k].Node.Address
					}
					log.Debugf("Found node: %s: %s:%d", id, addr, port)
					passingNodes = append(passingNodes, fmt.Sprintf("%s:%d", addr, port))
				}
				for ch := range consul.ListUpdatedChans {
					consul.ListUpdatedChans[ch] <- passingNodes
				}
			}
			time.Sleep(5 * time.Second)
		}
	}
	go updaterFunc()

	return nil
}

func (consul *ConsulDiscoveryService) UnregisterService() error {
	err := consul.ConsulClient.Agent().ServiceDeregister(consul.ServiceID)
	if err != nil {
		log.WithError(err).Errorf("Could not unregister service: %s", consul.ServiceID)
	}
	return err
}

func (consul *ConsulDiscoveryService) AddNodeListUpdated(key string, sub chan []string) {
	consul.ListUpdatedChans[key] = sub
}

func (consul *ConsulDiscoveryService) RemoveNodeListUpdated(key string) {
	delete(consul.ListUpdatedChans, key)
}

func (consul *ConsulDiscoveryService) updateTTL(check func() (bool, error)) {
	ticker := time.NewTicker(consul.ttl / 2)
	for range ticker.C {
		consul.update(check)
	}
}

func (consul *ConsulDiscoveryService) update(check func() (bool, error)) {
	ok, err := check()
	a := consul.ConsulClient.Agent()
	checkId := "service:" + consul.ServiceID

	if !ok {
		log.WithError(err).Warn("Health check failed")
		if agentErr := a.UpdateTTL(checkId, err.Error(), "fail"); agentErr != nil {
			log.WithError(agentErr).Error("Error updating TTL")
		}
	} else {
		if agentErr := a.UpdateTTL(checkId, "", "pass"); agentErr != nil {
			log.WithError(agentErr).Error("Error updating TTL")
		}
	}
}
