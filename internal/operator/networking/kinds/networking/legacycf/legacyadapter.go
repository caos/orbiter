package legacycf

import (
	"github.com/caos/orbos/internal/operator/core"
	"github.com/caos/orbos/internal/operator/networking/kinds/networking/legacycf/app"
	"github.com/caos/orbos/internal/operator/networking/kinds/networking/legacycf/config"
	"github.com/caos/orbos/mntr"
	"github.com/caos/orbos/pkg/kubernetes"
	"github.com/caos/orbos/pkg/labels"
	"github.com/pkg/errors"
)

func adaptFunc(
	monitor mntr.Monitor,
	cfg *config.InternalConfig,
) (
	core.QueryFunc,
	core.DestroyFunc,
	core.EnsureFunc,
	error,
) {
	return func(_ kubernetes.ClientInt, _ map[string]interface{}) (core.EnsureFunc, error) {
			return func(k8sClient kubernetes.ClientInt) error {

				groups := make(map[string][]string, 0)
				if cfg.Groups != nil {
					for _, group := range cfg.Groups {
						groups[group.Name] = group.List
					}
				}

				apps, err := app.New(cfg.AccountName, cfg.Credentials.User.Value, cfg.Credentials.APIKey.Value, cfg.Credentials.UserServiceKey.Value, groups, cfg.Prefix)
				if err != nil {
					return err
				}

				caSecretLabels := labels.MustForName(labels.MustForComponent(cfg.Labels, "cloudflare"), cfg.OriginCASecretName)
				for _, domain := range cfg.Domains {
					err = apps.Ensure(
						cfg.ID,
						k8sClient,
						cfg.Namespace,
						domain.Domain,
						domain.Subdomains,
						domain.Rules,
						caSecretLabels,
						domain.LoadBalancer,
						domain.FloatingIP,
					)
					if err != nil {
						return err
					}
				}
				return nil
			}, nil
		}, func(k8sClient kubernetes.ClientInt) error {
			//TODO
			return nil
		},
		func(k8sClient kubernetes.ClientInt) error {
			monitor.Info("waiting for certificate to be created")
			if err := k8sClient.WaitForSecret(cfg.Namespace, cfg.OriginCASecretName, 60); err != nil {
				return errors.Wrap(err, "error while waiting for certificate secret to be created")
			}
			monitor.Info("certificateis created")
			return nil
		}, nil
}
