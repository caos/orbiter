package kubernetes

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"strings"
	"text/template"

	macherrs "k8s.io/apimachinery/pkg/api/errors"

	"github.com/caos/orbos/internal/executables"
	"github.com/caos/orbos/internal/operator/orbiter/kinds/clusters/core/infra"
	"github.com/caos/orbos/mntr"
	"github.com/caos/orbos/pkg/git"
	"github.com/caos/orbos/pkg/kubernetes"
)

func ensureK8sPlugins(
	monitor mntr.Monitor,
	gitClient *git.Client,
	k8sClient kubernetes.ClientInt,
	desired DesiredV0,
	providerK8sSpec infra.Kubernetes,
) (err error) {

	k8sPluginsMonitor := monitor

	defer func() {
		if err != nil {
			err = fmt.Errorf("ensuring kubernetes plugin resources failed: %w", err)
		}
	}()

	applyResources, err := providerK8sSpec.CleanupAndApply(k8sClient)
	if err != nil {
		return err
	}
	if applyResources != nil {
		k8sPluginsMonitor = k8sPluginsMonitor.WithField("cloudcontrollermanager", providerK8sSpec.CloudController.ProviderName)
	}

	switch desired.Spec.Networking.Network {
	case "cilium":

		var create bool
		deployment, err := k8sClient.GetDeployment("kube-system", "cilium-operator")
		if macherrs.IsNotFound(err) {
			create = true
			err = nil
		}
		if err != nil {
			return err
		}

		expectVersion := "v1.6.3"
		if !create && strings.Split(deployment.Spec.Template.Spec.Containers[0].Image, ":")[1] == expectVersion {
			k8sPluginsMonitor.WithField("version", expectVersion).Debug("Calico is already ensured")
			break
		}

		k8sPluginsMonitor = k8sPluginsMonitor.WithField("cilium", expectVersion)

		buf := new(bytes.Buffer)
		defer buf.Reset()

		istioReg := desired.Spec.CustomImageRegistry
		if istioReg != "" && !strings.HasSuffix(istioReg, "/") {
			istioReg += "/"
		}

		ciliumReg := desired.Spec.CustomImageRegistry
		if ciliumReg == "" {
			ciliumReg = "docker.io"
		}

		template.Must(template.New("").Parse(string(executables.PreBuilt("cilium.yaml")))).Execute(buf, struct {
			IstioProxyImageRegistry string
			CiliumImageRegistry     string
		}{
			IstioProxyImageRegistry: istioReg,
			CiliumImageRegistry:     ciliumReg,
		})
		applyResources = concatYAML(applyResources, buf)

	case "calico":

		var create bool
		deployment, err := k8sClient.GetDeployment("kube-system", "calico-kube-controllers")
		if macherrs.IsNotFound(err) {
			create = true
			err = nil
		}
		if err != nil {
			return err
		}

		expectVersion := "v3.18.2"
		if !create && strings.Split(deployment.Spec.Template.Spec.Containers[0].Image, ":")[1] == expectVersion {
			k8sPluginsMonitor.WithField("version", expectVersion).Debug("Calico is already ensured")
			break
		}

		k8sPluginsMonitor = k8sPluginsMonitor.WithField("calico", expectVersion)

		reg := desired.Spec.CustomImageRegistry
		if reg != "" && !strings.HasSuffix(reg, "/") {
			reg += "/"
		}

		buf := new(bytes.Buffer)
		defer buf.Reset()
		template.Must(template.New("").Parse(string(executables.PreBuilt("calico.yaml")))).Execute(buf, struct {
			ImageRegistry string
		}{
			ImageRegistry: reg,
		})
		applyResources = concatYAML(applyResources, buf)
	case "":
	default:
		networkFile := gitClient.Read(desired.Spec.Networking.Network)
		if len(networkFile) == 0 {
			return fmt.Errorf("network file %s is empty or not found in git repository", desired.Spec.Networking.Network)
		}

		applyResources = concatYAML(applyResources, bytes.NewReader(networkFile))
	}
	if applyResources == nil {
		return nil
	}
	data, err := ioutil.ReadAll(applyResources)
	if err != nil {
		return err
	}
	if err := k8sClient.ApplyPlainYAML(monitor, data); err != nil {
		return err
	}
	k8sPluginsMonitor.Info("Kubernetes plugins successfully reconciled")
	return nil
}
