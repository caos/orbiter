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

func AdaptFuncToEnsure(
	monitor mntr.Monitor,
	namespace string,
	id labels.IDLabels,
	grpc bool,
	host,
	prefix,
	rewrite,
	service string,
	timeoutMS,
	connectTimeoutMS int,
	cors *CORS) (resources.QueryFunc, error) {

	spec := map[string]interface{}{
		"host":    host,
		"rewrite": rewrite,
		"service": service,
	}
	if prefix != "" {
		spec["prefix"] = prefix
	}

	if timeoutMS != 0 {
		spec["timeout_ms"] = timeoutMS
	}
	if connectTimeoutMS != 0 {
		spec["connect_timeout_ms"] = connectTimeoutMS
	}
	if grpc {
		spec["grpc"] = grpc
	}

	if cors != nil {
		corsMap := map[string]interface{}{
			"origins":         cors.Origins,
			"methods":         cors.Methods,
			"headers":         cors.Headers,
			"credentials":     cors.Credentials,
			"exposed_headers": cors.ExposedHeaders,
			"max_age":         cors.MaxAge,
		}
		spec["cors"] = corsMap
	}

	crd := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"kind":       kind,
			"apiVersion": group + "/" + version,
			"metadata": map[string]interface{}{
				"name":      id.Name(),
				"namespace": namespace,
				"labels":    labels.MustK8sMap(id),
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
			monitor.WithField("name", crdName).Info("crd definition not found, skipping")
			return func(k8sClient kubernetes.ClientInt) error { return nil }, nil
		}

		return func(k8sClient kubernetes.ClientInt) error {
			return k8sClient.ApplyNamespacedCRDResource(group, version, kind, namespace, id.Name(), crd)
		}, nil
	}, nil
}

func AdaptFuncToDestroy(namespace, name string) (resources.DestroyFunc, error) {
	return func(client kubernetes.ClientInt) error {
		return client.DeleteNamespacedCRDResource(group, version, kind, namespace, name)
	}, nil
}
