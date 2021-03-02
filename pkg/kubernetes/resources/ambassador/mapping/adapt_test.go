package mapping_test

import (
	"testing"

	"github.com/caos/orbos/pkg/labels"

	apixv1beta1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/caos/orbos/mntr"
	kubernetesmock "github.com/caos/orbos/pkg/kubernetes/mock"
	"github.com/caos/orbos/pkg/kubernetes/resources"
	"github.com/caos/orbos/pkg/kubernetes/resources/adaptertesting"
	"github.com/caos/orbos/pkg/kubernetes/resources/ambassador/mapping"
	"github.com/caos/orbos/pkg/labels/mocklabels"
)

func TestAdaptFuncToEnsure(t *testing.T) {

	const (
		namespace          = "test"
		virtualHost        = "api.domain"
		service            = "service:8080"
		grpc               = true
		prefix             = "/caos.zitadel.admin.api.v1.AdminService/"
		rewrite            = "/caos.zitadel.admin.api.v1.AdminService/"
		timeoutMS          = 30000
		connectTimeoutMS   = 30000
		corsOrigins        = "*"
		corsMethods        = "POST, GET, OPTIONS, DELETE, PUT"
		corsHeaders        = "*"
		corsCredentials    = true
		corsExposedHeaders = "*"
		corsMaxAge         = "86400"
	)
	var (
		monitor    = mntr.Monitor{}
		nameLabels = mocklabels.Name
		corsArgs   = &mapping.CORS{
			Origins:        corsOrigins,
			Methods:        corsMethods,
			Headers:        corsHeaders,
			Credentials:    corsCredentials,
			ExposedHeaders: corsExposedHeaders,
			MaxAge:         corsMaxAge,
		}
		expectFullSpec = func() map[string]interface{} {
			return map[string]interface{}{
				"connect_timeout_ms": timeoutMS,
				"host":               virtualHost,
				"prefix":             prefix,
				"rewrite":            rewrite,
				"service":            service,
				"timeout_ms":         timeoutMS,
				"cors": map[string]interface{}{
					"origins":         corsOrigins,
					"methods":         corsMethods,
					"headers":         corsHeaders,
					"credentials":     corsCredentials,
					"exposed_headers": corsExposedHeaders,
					"max_age":         corsMaxAge,
				},
				"grpc": grpc,
			}
		}
		testAdaptFuncToEnsure = func(cors *mapping.CORS) func() (resources.QueryFunc, error) {
			return func() (queryFunc resources.QueryFunc, err error) {
				return mapping.AdaptFuncToEnsure(
					monitor,
					namespace,
					nameLabels,
					grpc,
					virtualHost,
					prefix,
					rewrite,
					service,
					timeoutMS,
					connectTimeoutMS,
					cors)
			}
		}
		expectSpec = func(spec map[string]interface{}) func(*kubernetesmock.MockClientInt) {
			const (
				apiGroup    = "getambassador.io"
				apiVersion  = "v2"
				mappingKind = "Mapping"
			)

			return func(k8sClient *kubernetesmock.MockClientInt) {
				k8sClient.EXPECT().ApplyNamespacedCRDResource(apiGroup, apiVersion, mappingKind, namespace, nameLabels.Name(), &unstructured.Unstructured{
					Object: map[string]interface{}{
						"apiVersion": apiGroup + "/" + apiVersion,
						"kind":       mappingKind,
						"metadata": map[string]interface{}{
							"labels":    labels.MustK8sMap(nameLabels),
							"name":      nameLabels.Name(),
							"namespace": namespace,
						},
						"spec": spec,
					},
				}).Times(1)
			}
		}
	)

	adaptertesting.RunScopes("Ambassador Mapping", []func() adaptertesting.Scope{
		func() adaptertesting.Scope {
			return adaptertesting.Scope{
				Subscopes: nil,
				Case: adaptertesting.Case{
					Name:                "It should fill all the correct yaml keys with the given values",
					Adapt:               testAdaptFuncToEnsure(corsArgs),
					ExpectWhileQuerying: expectWhileQuerying,
					ExpectWhileEnsuring: expectSpec(expectFullSpec()),
					WantAdaptErr:        false,
					WantQueryErr:        false,
					WantEnsureErr:       false,
				},
			}
		},
		func() adaptertesting.Scope {
			withoutCorsSpec := expectFullSpec()
			delete(withoutCorsSpec, "cors")
			return adaptertesting.Scope{
				Subscopes: nil,
				Case: adaptertesting.Case{
					Name:                "It should omit the cors properties if cors argument is nil",
					Adapt:               testAdaptFuncToEnsure(nil),
					ExpectWhileQuerying: expectWhileQuerying,
					ExpectWhileEnsuring: expectSpec(withoutCorsSpec),
					WantAdaptErr:        false,
					WantQueryErr:        false,
					WantEnsureErr:       false,
				},
			}
		},
	}, t)
}

func expectWhileQuerying(k8sClient *kubernetesmock.MockClientInt) {
	k8sClient.EXPECT().CheckCRD("mappings.getambassador.io").Times(1).Return(&apixv1beta1.CustomResourceDefinition{}, true, nil)
}
