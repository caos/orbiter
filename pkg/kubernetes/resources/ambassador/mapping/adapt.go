package mapping

import (
	"github.com/caos/orbos/mntr"
	"github.com/caos/orbos/pkg/kubernetes"
	"github.com/caos/orbos/pkg/kubernetes/resources"
	"github.com/caos/orbos/pkg/labels"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

const (
	group   = "getambassador.io"
	version = "v2"
	kind    = "Mapping"
)

type CORS struct {
	Origins        string
	Methods        string
	Headers        string
	Credentials    bool
	ExposedHeaders string
	MaxAge         string
}

type Arguments struct {
	Monitor          mntr.Monitor
	Namespace        string
	ID               labels.IDLabels
	GRPC             bool
	Host             string
	Prefix           string
	Rewrite          string
	Service          string
	TimeoutMS        int
	ConnectTimeoutMS int
	CORS             *CORS
}

func AdaptFuncToEnsure(params *Arguments) (resources.QueryFunc, error) {

	spec := map[string]interface{}{
		"host":    params.Host,
		"rewrite": params.Rewrite,
		"service": params.Service,
	}
	if params.Prefix != "" {
		spec["prefix"] = params.Prefix
	}

	if params.TimeoutMS != 0 {
		spec["timeout_ms"] = params.TimeoutMS
	}
	if params.ConnectTimeoutMS != 0 {
		spec["connect_timeout_ms"] = params.ConnectTimeoutMS
	}
	if params.GRPC {
		spec["grpc"] = params.GRPC
	}

	if params.CORS != nil {
		corsMap := map[string]interface{}{
			"origins":         params.CORS.Origins,
			"methods":         params.CORS.Methods,
			"headers":         params.CORS.Headers,
			"credentials":     params.CORS.Credentials,
			"exposed_headers": params.CORS.ExposedHeaders,
			"max_age":         params.CORS.MaxAge,
		}
		spec["cors"] = corsMap
	}

	crd := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"kind":       kind,
			"apiVersion": group + "/" + version,
			"metadata": map[string]interface{}{
				"name":      params.ID.Name(),
				"namespace": params.Namespace,
				"labels":    labels.MustK8sMap(params.ID),
			},
			"spec": spec,
		}}

	return func(k8sClient kubernetes.ClientInt) (resources.EnsureFunc, error) {
		crdName := "mappings.getambassador.io"
		_, ok, err := k8sClient.CheckCRD(crdName)
		if err != nil {
			return nil, err
		}
		if !ok {
			params.Monitor.WithField("name", crdName).Info("crd definition not found, skipping")
			return func(k8sClient kubernetes.ClientInt) error { return nil }, nil
		}

		return func(k8sClient kubernetes.ClientInt) error {
			return k8sClient.ApplyNamespacedCRDResource(group, version, kind, params.Namespace, params.ID.Name(), crd)
		}, nil
	}, nil
}

func AdaptFuncToDestroy(namespace, name string) (resources.DestroyFunc, error) {
	return func(client kubernetes.ClientInt) error {
		return client.DeleteNamespacedCRDResource(group, version, kind, namespace, name)
	}, nil
}
