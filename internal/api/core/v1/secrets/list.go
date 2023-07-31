package secrets

import (
	"fmt"
	"net/http"

	"github.com/emicklei/go-restful/v3"
	"github.com/portainer/k2d/internal/api/utils"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

func (svc SecretService) ListSecrets(r *restful.Request, w *restful.Response) {
	secretList, err := svc.adapter.ListSecrets()
	if err != nil {
		utils.HttpError(r, w, http.StatusInternalServerError, fmt.Errorf("unable to list secrets: %w", err))
		return
	}

	utils.WriteListBasedOnAcceptHeader(r, w, &secretList, func() runtime.Object {
		return &corev1.SecretList{
			TypeMeta: metav1.TypeMeta{
				Kind:       "SecretList",
				APIVersion: "v1",
			},
		}
	}, svc.adapter.ConvertObjectToVersionedObject)
}
