package kubernetes

import (
	"bytes"
	"fmt"
	"io"
	"strings"
	"text/template"
	"time"

	"github.com/caos/orbos/pkg/git"

	"github.com/caos/orbos/internal/executables"
	"github.com/caos/orbos/pkg/kubernetes"
	"github.com/pkg/errors"

	"github.com/caos/orbos/internal/operator/orbiter/kinds/clusters/core/infra"
	"github.com/caos/orbos/mntr"
)

type CloudIntegration int

func join(
	monitor mntr.Monitor,
	clusterID string,
	joining *initializedMachine,
	joinAt infra.Machine,
	desired DesiredV0,
	kubeAPI *infra.Address,
	joinToken string,
	kubernetesVersion KubernetesVersion,
	certKey string,
	client *kubernetes.Client,
	imageRepository string,
	gitClient *git.Client,
	providerK8sSpec infra.Kubernetes) (*string, error) {

	monitor = monitor.WithFields(map[string]interface{}{
		"machine": joining.infra.ID(),
		"tier":    joining.pool.tier,
	})

	applyResources := providerK8sSpec.Apply

	switch desired.Spec.Networking.Network {
	case "cilium":
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
			return nil, fmt.Errorf("network file %s is empty or not found in git repository", desired.Spec.Networking.Network)
		}

		applyResources = concatYAML(applyResources, bytes.NewReader(networkFile))
	}

	kubeadmCfgPath := "/etc/kubeadm/config.yaml"
	cloudCfgPath := "/var/orbiter/cloud-config"

	kubeadmCfg := new(bytes.Buffer)
	defer kubeadmCfg.Reset()

	if err := template.Must(template.New("").Parse(`kind: ClusterConfiguration
apiVersion: kubeadm.k8s.io/v1beta2
apiServer:
  timeoutForControlPlane: 4m0s
  certSANs:
  - "{{ .CertSAN }}"{{if and .ProviderK8sSpec.CloudController.Supported (ne .ProviderK8sSpec.CloudController.ProviderName "external") }}
  extraArgs:
    cloud-provider: "{{ .ProviderK8sSpec.CloudController.ProviderName }}"
    cloud-config: "{{ .CloudConfigPath }}"
  extraVolumes:
  - name: cloud
    hostPath: "{{ .CloudConfigPath }}"
    mountPath: "{{ .CloudConfigPath }}"
controllerManager:
  extraArgs:
    cloud-provider: "{{ .ProviderK8sSpec.CloudController.ProviderName }}"
    cloud-config: "{{ .CloudConfigPath }}"
  extraVolumes:
  - name: cloud
    hostPath: "{{ .CloudConfigPath }}"
    mountPath: "{{ .CloudConfigPath }}"{{ end }}
certificatesDir: /etc/kubernetes/pki
clusterName: "{{ .ClusterName }}"
controlPlaneEndpoint: "{{ .ControlPlaneEndpoint }}"
dns:
  type: CoreDNS
etcd:
  local:
    imageRepository: "{{ .ImageRepository }}"
    dataDir: /var/lib/etcd
    extraArgs:
      listen-metrics-urls: http://0.0.0.0:2381
imageRepository: "{{ .ImageRepository }}"
kubernetesVersion: "{{ .KubernetesVersion }}"
networking:
  dnsDomain: "{{ .DNSDomain }}"
  podSubnet: "{{ .PodSubnet }}"
  serviceSubnet: "{{ .ServiceSubnet }}"
scheduler: {}

---

kind: KubeletConfiguration
apiVersion: kubelet.config.k8s.io/v1beta1
cgroupDriver: systemd

---{{if .JoinAt }}
kind: JoinConfiguration
apiVersion: kubeadm.k8s.io/v1beta2
caCertPath: /etc/kubernetes/pki/ca.crt
discovery:
  bootstrapToken:
    apiServerEndpoint: {{ .JoinAt.IP }}:{{ .BindPort }}
    token: {{ .Token }}
    unsafeSkipCAVerification: true
  timeout: 5m0s
nodeRegistration:
  kubeletExtraArgs:
    node-ip: "{{ .Node.IP }}"{{if .ProviderK8sSpec.CloudController.Supported}}
    cloud-provider: "{{ .ProviderK8sSpec.CloudController.ProviderName }}"
    cloud-config: "{{ .CloudConfigPath }}"{{end}}
  name: "{{ .Node.ID }}"{{if .IsControlPlane }}
controlPlane:
  localAPIEndpoint:
    advertiseAddress: "{{ .Node.IP }}"
    bindPort: {{ .BindPort }}
  certificateKey: {{.CertKey}}{{end}}{{else}}
kind: InitConfiguration
apiVersion: kubeadm.k8s.io/v1beta2
bootstrapTokens:
- groups:
  - system:bootstrappers:kubeadm:default-node-token
  token: {{ .Token }}
  ttl: 10m0s
  usages:
  - signing
  - authentication
localAPIEndpoint:
  advertiseAddress: "{{ .Node.IP }}"
  bindPort: {{ .BindPort }}
nodeRegistration:
#	criSocket: /var/run/dockershim.sock
  name:  "{{ .Node.ID }}"
  kubeletExtraArgs:
    node-ip: "{{ .Node.IP }}"{{if .ProviderK8sSpec.CloudController.Supported }}
    cloud-provider: "{{ .ProviderK8sSpec.CloudController.ProviderName }}"
    cloud-config: "{{ .CloudConfigPath }}"{{end}}
  taints:
  - effect: NoSchedule
    key: node-role.kubernetes.io/master
{{end}}
`)).Execute(kubeadmCfg, struct {
		Token                string
		Node                 infra.Machine
		BindPort             uint16
		CertSAN              string
		ClusterName          string
		ControlPlaneEndpoint string
		ImageRepository      string
		KubernetesVersion    string
		DNSDomain            string
		PodSubnet            string
		ServiceSubnet        string
		JoinAt               infra.Machine
		IsControlPlane       bool
		CertKey              string
		ProviderK8sSpec      infra.Kubernetes
		CloudConfigPath      string
	}{
		Token:                joinToken,
		Node:                 joining.infra,
		BindPort:             kubeAPI.BackendPort,
		CertSAN:              kubeAPI.Location,
		ClusterName:          clusterID,
		ControlPlaneEndpoint: kubeAPI.String(),
		ImageRepository:      imageRepository,
		KubernetesVersion:    kubernetesVersion.String(),
		DNSDomain:            desired.Spec.Networking.DNSDomain,
		PodSubnet:            string(desired.Spec.Networking.PodCidr),
		ServiceSubnet:        string(desired.Spec.Networking.ServiceCidr),
		JoinAt:               joinAt,
		IsControlPlane:       joining.pool.tier == Controlplane,
		CertKey:              certKey,
		ProviderK8sSpec:      providerK8sSpec,
		CloudConfigPath:      cloudCfgPath,
	}); err != nil {
		return nil, err
	}

	if err := infra.Try(monitor, time.NewTimer(7*time.Second), 2*time.Second, joining.infra, func(cmp infra.Machine) error {
		return cmp.WriteFile(kubeadmCfgPath, kubeadmCfg, 600)
	}); err != nil {
		return nil, err
	}
	monitor.WithFields(map[string]interface{}{
		"path": kubeadmCfgPath,
	}).Debug("Written file")

	if providerK8sSpec.CloudController.Supported && providerK8sSpec.CloudController.CloudConfig != nil {
		if err := infra.Try(monitor, time.NewTimer(7*time.Second), 2*time.Second, joining.infra, func(cmp infra.Machine) error {
			return cmp.WriteFile(cloudCfgPath, providerK8sSpec.CloudController.CloudConfig(joining.infra), 600)
		}); err != nil {
			return nil, err
		}
		monitor.WithFields(map[string]interface{}{
			"path": cloudCfgPath,
		}).Debug("Written file")
	}

	cmd := "sudo kubeadm reset -f && sudo rm -rf /var/lib/etcd"
	resetStdout, err := joining.infra.Execute(nil, cmd)
	if err != nil {
		return nil, errors.Wrapf(err, "executing %s failed", cmd)
	}
	monitor.WithFields(map[string]interface{}{
		"stdout": string(resetStdout),
	}).Debug("Cleaned up machine")

	if joinAt != nil {
		cmd := fmt.Sprintf("sudo kubeadm join --ignore-preflight-errors=Port-%d %s:%d --config %s", kubeAPI.BackendPort, joinAt.IP(), kubeAPI.FrontendPort, kubeadmCfgPath)
		joinStdout, err := joining.infra.Execute(nil, cmd)
		if err != nil {
			return nil, errors.Wrapf(err, "executing %s failed", cmd)
		}

		monitor.WithFields(map[string]interface{}{
			"stdout": string(joinStdout),
		}).Debug("Executed kubeadm join")

		if err := joining.pool.infra.EnsureMember(joining.infra); err != nil {
			return nil, err
		}

		joining.currentMachine.Joined = true
		monitor.Changed("Node joined")
		dnsPods, err := client.ListPods("kube-system", map[string]string{"k8s-app": "kube-dns"})
		if err != nil {
			return nil, err
		}

		if len(dnsPods.Items) > 1 {
			err = client.DeletePod("kube-system", dnsPods.Items[0].Name)
		}

		return nil, err
	}

	if err := joining.pool.infra.EnsureMember(joining.infra); err != nil {
		return nil, err
	}

	initCmd := fmt.Sprintf(`\
sudo kubeadm init --ignore-preflight-errors=Port-%d --config %s && \
mkdir -p ${HOME}/.kube && yes | sudo cp -rf /etc/kubernetes/admin.conf ${HOME}/.kube/config && \
sudo chown $(id -u):$(id -g) ${HOME}/.kube/config && \
kubectl -n kube-system patch deployment coredns --type='json' \
-p='[{"op": "add", "path": "/spec/template/spec/tolerations/0", "value": {"effect": "NoSchedule", key: "node.cloudprovider.kubernetes.io/uninitialized", value: "true" } }]'`, kubeAPI.BackendPort, kubeadmCfgPath)
	initStdout, err := joining.infra.Execute(nil, initCmd)
	if err != nil {
		return nil, fmt.Errorf(`error initializing kubernetes by executing command (%s): %s: %w`, initCmd, initStdout, err)
	}
	monitor.WithFields(map[string]interface{}{
		"stdout": string(initStdout),
	}).Debug("Executed kubeadm init")

	kubeconfigBuf := new(bytes.Buffer)
	defer kubeconfigBuf.Reset()
	if err := joining.infra.ReadFile("${HOME}/.kube/config", kubeconfigBuf); err != nil {
		return nil, err
	}

	joining.currentMachine.Joined = true
	monitor.Changed("Cluster initialized")

	kc := strings.ReplaceAll(kubeconfigBuf.String(), "kubernetes-admin", strings.Join([]string{clusterID, "admin"}, "-"))

	if applyResources != nil {
		if out, err := joining.infra.Execute(applyResources, "kubectl create -f -"); err != nil {
			return nil, fmt.Errorf("error applying initial resources: %w: %s", err, string(out))
		}
	}

	return &kc, nil
}

func concatYAML(head, tail io.Reader) io.Reader {
	if head == nil {
		return tail
	}
	if tail == nil {
		return head
	}
	return io.MultiReader(head, strings.NewReader("---\n\n"), tail)
}
