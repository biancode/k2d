package namespaces

import (
	"fmt"
	"net/http"

	"github.com/emicklei/go-restful/v3"
	"github.com/portainer/k2d/internal/api/utils"
	"github.com/portainer/k2d/internal/controller"
	"github.com/portainer/k2d/internal/types"
	httputils "github.com/portainer/k2d/pkg/http"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/uuid"
)

func (svc NamespaceService) CreateNamespace(r *restful.Request, w *restful.Response) {
	namespace := &corev1.Namespace{}

	err := httputils.ParseJSONBody(r.Request, &namespace)
	if err != nil {
		utils.HttpError(r, w, http.StatusBadRequest, fmt.Errorf("unable to parse request body: %w", err))
		return
	}

	dryRun := r.QueryParameter("dryRun") != ""
	if dryRun {
		w.WriteAsJson(namespace)
		return
	}

	svc.operations <- controller.NewOperation(namespace, controller.HighPriorityOperation, r.HeaderParameter(types.RequestIDHeader))

	namespace.CreationTimestamp = metav1.Now()
	namespace.UID = uuid.NewUUID()
	namespace.ResourceVersion = "1"

	w.WriteAsJson(namespace)
}
