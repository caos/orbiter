package certificate

import (
	"github.com/caos/orbos/internal/operator/core"
	"github.com/caos/orbos/internal/operator/database/kinds/databases/managed/certificate/client"
	"github.com/caos/orbos/internal/operator/database/kinds/databases/managed/certificate/node"
	"github.com/caos/orbos/mntr"
	"github.com/caos/orbos/pkg/kubernetes"
	"github.com/caos/orbos/pkg/labels"
)

var (
	nodeSecret = "cockroachdb.node"
)

func AdaptFunc(
	monitor mntr.Monitor,
	namespace string,
	componentLabels *labels.Component,
	clusterDns string,
) (
	core.QueryFunc,
	core.DestroyFunc,
	func(user string) (core.QueryFunc, error),
	func(user string) (core.DestroyFunc, error),
	func(k8sClient kubernetes.ClientInt) ([]string, error),
	error,
) {
	cMonitor := monitor.WithField("type", "certificates")

	queryNode, destroyNode, err := node.AdaptFunc(
		cMonitor,
		namespace,
		labels.MustForName(componentLabels, nodeSecret),
		clusterDns,
	)
	if err != nil {
		return nil, nil, nil, nil, nil, err
	}

	queriers := []core.QueryFunc{
		queryNode,
	}

	destroyers := []core.DestroyFunc{
		destroyNode,
	}

	return func(k8sClient kubernetes.ClientInt, queried map[string]interface{}) (core.EnsureFunc, error) {
			return core.QueriersToEnsureFunc(cMonitor, false, queriers, k8sClient, queried)
		},
		core.DestroyersToDestroyFunc(cMonitor, destroyers),
		func(user string) (core.QueryFunc, error) {
			query, _, err := client.AdaptFunc(
				cMonitor,
				namespace,
				componentLabels,
			)
			if err != nil {
				return nil, err
			}
			queryClient := query(user)

			return func(k8sClient kubernetes.ClientInt, queried map[string]interface{}) (core.EnsureFunc, error) {
				_, err := queryNode(k8sClient, queried)
				if err != nil {
					return nil, err
				}

				return queryClient(k8sClient, queried)
			}, nil
		},
		func(user string) (core.DestroyFunc, error) {
			_, destroy, err := client.AdaptFunc(
				cMonitor,
				namespace,
				componentLabels,
			)
			if err != nil {
				return nil, err
			}

			return destroy(user), nil
		},
		func(k8sClient kubernetes.ClientInt) ([]string, error) {
			return client.QueryCertificates(namespace, labels.DeriveComponentSelector(componentLabels, false), k8sClient)
		},
		nil
}
