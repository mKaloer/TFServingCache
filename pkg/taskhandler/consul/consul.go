package consul

type ConsulDiscoveryService struct {
	ListUpdatedChans []chan []string
}

func NewDiscoveryService() *ConsulDiscoveryService {
	c := &ConsulDiscoveryService{
		ListUpdatedChans: make([]chan []string, 0),
	}

	return c
}

func (consul *ConsulDiscoveryService) AddNodeListUpdated(sub chan []string) {
	consul.ListUpdatedChans = append(consul.ListUpdatedChans, sub)
}

func (consul *ConsulDiscoveryService) NodeName() string {
	return "Fooo"
}
