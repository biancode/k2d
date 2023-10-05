package events

import (
	"github.com/emicklei/go-restful/v3"
	"github.com/portainer/k2d/internal/adapter"
	"github.com/portainer/k2d/internal/api/utils"
)

type EventService struct {
	adapter *adapter.KubeDockerAdapter
}

func NewEventService(adapter *adapter.KubeDockerAdapter) EventService {
	return EventService{
		adapter: adapter,
	}
}

func (svc EventService) RegisterEventAPI(ws *restful.WebService) {
	ws.Route(ws.GET("/v1/events").
		To(svc.ListEvents))

	ws.Route(ws.GET("/v1/namespaces/{namespace}/events").
		Filter(utils.NamespaceValidation(svc.adapter)).
		To(svc.ListEvents))
}
