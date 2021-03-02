package adaptertesting

import (
	"fmt"
	"testing"

	"github.com/caos/orbos/pkg/kubernetes/resources"

	apixv1beta1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	kubernetesmock "github.com/caos/orbos/pkg/kubernetes/mock"
	"github.com/golang/mock/gomock"
)

type Case struct {
	Name                string
	Adapt               func() (resources.QueryFunc, error)
	ExpectWhileQuerying func(k8sClient *kubernetesmock.MockClientInt)
	ExpectWhileEnsuring func(k8sClient *kubernetesmock.MockClientInt)
	WantAdaptErr        bool
	WantQueryErr        bool
	WantEnsureErr       bool
}

type Scope struct {
	SubscopesDescription string
	Subscopes            []func() Scope
	Case                 Case
}

func RunScopes(describe string, scopes []func() Scope, t *testing.T) {
	for _, scope := range scopes {
		resolvedScope := scope()
		Run(describe, resolvedScope.Case, t)
		RunScopes(resolvedScope.SubscopesDescription, resolvedScope.Subscopes, t)
	}
}

func Run(describe string, testCase Case, t *testing.T) {
	t.Run(fmt.Sprintf("%s/%s", describe, testCase.Name), func(t *testing.T) {

		query, err := testCase.Adapt()
		if (err != nil) != testCase.WantAdaptErr {
			t.Fatalf("AdaptFuncToEnsure() error = %v, WantAdaptErr %v", err, testCase.WantAdaptErr)
		}

		queryController := gomock.NewController(t)
		queryK8sClient := kubernetesmock.NewMockClientInt(queryController)
		testCase.ExpectWhileQuerying(queryK8sClient)
		ensure, err := query(queryK8sClient)
		queryController.Finish()
		if (err != nil) != testCase.WantQueryErr {
			t.Fatalf("AdaptFuncToEnsure() error = %v, WantQueryErr %v", err, testCase.WantQueryErr)
		}

		ensureController := gomock.NewController(t)
		ensureK8sClient := kubernetesmock.NewMockClientInt(ensureController)
		testCase.ExpectWhileEnsuring(ensureK8sClient)
		err = ensure(ensureK8sClient)
		ensureController.Finish()
		if (err != nil) != testCase.WantEnsureErr {
			t.Fatalf("AdaptFuncToEnsure() error = %v, WantEnsureErr %v", err, testCase.WantEnsureErr)
		}
	})
}

func expectAppliedCRD(k8sClient *kubernetesmock.MockClientInt, testCase, namespace, crdName, resourceApiGroup, resourceApiVersion, resourceKind, resourceName string, resource *unstructured.Unstructured) {
	var (
		namespaceMatcher          = ExpectValue(testCase, namespace)
		crdNameMatcher            = ExpectValue(testCase, crdName)
		resourceApiGroupMatcher   = ExpectValue(testCase, resourceApiGroup)
		resourceApiVersionMatcher = ExpectValue(testCase, resourceApiVersion)
		resourceKindMatcher       = ExpectValue(testCase, resourceKind)
		resourceNameMatcher       = ExpectValue(testCase, resourceName)
		resourceMatcher           = ExpectValue(testCase, resource)
	)
	k8sClient.EXPECT().CheckCRD(crdNameMatcher).Times(1).Return(&apixv1beta1.CustomResourceDefinition{}, true, nil)
	k8sClient.EXPECT().ApplyNamespacedCRDResource(resourceApiGroupMatcher, resourceApiVersionMatcher, resourceKindMatcher, namespaceMatcher, resourceNameMatcher, resourceMatcher).Times(1)
}
