package kubernetes

import (
	"errors"
	"strconv"
	"strings"

	"github.com/caos/orbos/internal/operator/nodeagent/dep/sysctl"

	"github.com/caos/orbos/internal/operator/common"
	"github.com/caos/orbos/mntr"
)

type KubernetesVersion int

const (
	Unknown KubernetesVersion = iota
	V1x15x0
	V1x15x1
	V1x15x2
	V1x15x3
	V1x15x4
	V1x15x5
	V1x15x6
	V1x15x7
	V1x15x8
	V1x15x9
	V1x15x10
	V1x15x11
	V1x15x12
	V1x16x0
	V1x16x1
	V1x16x2
	V1x16x3
	V1x16x4
	V1x16x5
	V1x16x6
	V1x16x7
	V1x16x8
	V1x16x9
	V1x16x10
	V1x16x11
	V1x16x12
	V1x16x13
	V1x16x14
	V1x16x15
	V1x17x0
	V1x17x1
	V1x17x2
	V1x17x3
	V1x17x4
	V1x17x5
	V1x17x6
	V1x17x7
	V1x17x8
	V1x17x9
	V1x17x10
	V1x17x11
	V1x17x12
	V1x17x13
	V1x17x14
	V1x17x15
	V1x17x16
	V1x17x17
	V1x18x0
	V1x18x1
	V1x18x2
	V1x18x3
	V1x18x4
	V1x18x5
	V1x18x6
	V1x18x7
	V1x18x8
	V1x18x9
	V1x18x10
	V1x18x11
	V1x18x12
	V1x18x13
	V1x18x14
	V1x18x15
	V1x18x16
	V1x18x17
	V1x18x18
	V1x18x19
	V1x19x0
	V1x19x1
	V1x19x2
	V1x19x3
	V1x19x4
	V1x19x5
	V1x19x6
	V1x19x7
	V1x19x8
	V1x19x9
	V1x19x10
	V1x20x0
	V1x20x1
	V1x20x2
	V1x20x3
	V1x20x4
	V1x20x5
	V1x20x6
	V1x21x0
)

var kubernetesVersions = []string{
	"unknown",
	"v1.15.0", "v1.15.1", "v1.15.2", "v1.15.3", "v1.15.4", "v1.15.5", "v1.15.6", "v1.15.7", "v1.15.8", "v1.15.9", "v1.15.10", "v1.15.11", "v1.15.12",
	"v1.16.0", "v1.16.1", "v1.16.2", "v1.16.3", "v1.16.4", "v1.16.5", "v1.16.6", "v1.16.7", "v1.16.8", "v1.16.9", "v1.16.10", "v1.16.11", "v1.16.12", "v1.16.13", "v1.16.14", "v1.16.15",
	"v1.17.0", "v1.17.1", "v1.17.2", "v1.17.3", "v1.17.4", "v1.17.5", "v1.17.6", "v1.17.7", "v1.17.8", "v1.17.9", "v1.17.10", "v1.17.11", "v1.17.12", "v1.17.13", "v1.17.14", "v1.17.15", "v1.17.16", "v1.17.17",
	"v1.18.0", "v1.18.1", "v1.18.2", "v1.18.3", "v1.18.4", "v1.18.5", "v1.18.6", "v1.18.7", "v1.18.8", "v1.18.9", "v1.18.10", "v1.18.11", "v1.18.12", "v1.18.13", "v1.18.14", "v1.18.15", "v1.18.16", "v1.18.17", "v1.18.18", "v1.18.19",
	"v1.19.0", "v1.19.1", "v1.19.2", "v1.19.3", "v1.19.4", "v1.19.5", "v1.19.6", "v1.19.7", "v1.19.8", "v1.19.9", "v1.19.10",
	"v1.20.0", "v1.20.1", "v1.20.2", "v1.20.3", "v1.20.4", "v1.20.5", "v1.20.6",
	"v1.21.0",
}

func (k KubernetesVersion) String() string {
	return kubernetesVersions[k]
}

func (k KubernetesVersion) DefineSoftware() common.Software {
	dockerVersion := "docker-ce v19.03.5"
	//	if minor, err := k.ExtractMinor(); err != nil && minor <= 15 {
	//		dockerVersion = "docker-ce v18.09.6"
	//	}

	sysctlPkg := common.Package{}
	sysctl.Enable(&sysctlPkg, common.IpForward)
	sysctl.Enable(&sysctlPkg, common.BridgeNfCallIptables)
	sysctl.Enable(&sysctlPkg, common.BridgeNfCallIp6tables)
	return common.Software{
		Swap: common.Package{Version: "disabled"},
		Containerruntime: common.Package{Version: dockerVersion, Config: map[string]string{
			"daemon.json": `{
	"exec-opts": ["native.cgroupdriver=systemd"],
	"log-driver": "json-file",
	"log-opts": {
		"max-size": "100m"
	},
	"storage-driver": "overlay2"
}`,
		}},
		Kubelet: common.Package{Version: k.String()},
		Kubeadm: common.Package{Version: k.String()},
		Kubectl: common.Package{Version: k.String()},
		Sysctl:  sysctlPkg,
	}
}

func KubernetesSoftware(current common.Software) common.Software {
	return common.Software{
		Swap:             current.Swap,
		Containerruntime: current.Containerruntime,
		Kubelet:          current.Kubelet,
		Kubeadm:          current.Kubeadm,
		Kubectl:          current.Kubectl,
		Sysctl:           current.Sysctl,
	}
}

func ParseString(version string) KubernetesVersion {
	for idx, k8sVersion := range kubernetesVersions {
		if k8sVersion == version {
			return KubernetesVersion(idx)
		}
	}
	return KubernetesVersion(0)
}

func (k KubernetesVersion) equals(other KubernetesVersion) bool {
	return string(k) == string(other)
}

func (k KubernetesVersion) NextHighestMinor() KubernetesVersion {
	switch k {
	case V1x15x0, V1x15x1, V1x15x2, V1x15x3, V1x15x4, V1x15x5, V1x15x6, V1x15x7, V1x15x8, V1x15x9, V1x15x10, V1x15x11, V1x15x12:
		return V1x16x15
	case V1x16x0, V1x16x1, V1x16x2, V1x16x3, V1x16x4, V1x16x5, V1x16x6, V1x16x7, V1x16x8, V1x16x9, V1x16x10, V1x16x11, V1x16x12, V1x16x13, V1x16x14, V1x16x15:
		return V1x17x17
	case V1x17x0, V1x17x1, V1x17x2, V1x17x3, V1x17x4, V1x17x5, V1x17x6, V1x17x7, V1x17x8, V1x17x9, V1x17x10, V1x17x11, V1x17x12, V1x17x13, V1x17x14, V1x17x15, V1x17x16, V1x17x17:
		return V1x18x19
	case V1x18x0, V1x18x1, V1x18x2, V1x18x3, V1x18x4, V1x18x5, V1x18x6, V1x18x7, V1x18x8, V1x18x9, V1x18x10, V1x18x11, V1x18x12, V1x18x13, V1x18x14, V1x18x15, V1x18x16, V1x18x17, V1x18x18, V1x18x19:
		return V1x19x10
	case V1x19x0, V1x19x1, V1x19x2, V1x19x3, V1x19x4, V1x19x5, V1x19x6, V1x19x7, V1x19x8, V1x19x9, V1x19x10:
		return V1x20x6
	case V1x20x0, V1x20x1, V1x20x2, V1x20x3, V1x20x4, V1x20x5, V1x20x6:
		return V1x21x0
	default:
		return Unknown
	}
}

func (k KubernetesVersion) ExtractMinor(monitor mntr.Monitor) (int, error) {
	return k.extractNumber(monitor, 1)
}

func (k KubernetesVersion) ExtractPatch(monitor mntr.Monitor) (int, error) {
	return k.extractNumber(monitor, 2)
}

func (k KubernetesVersion) extractNumber(monitor mntr.Monitor, position int) (int, error) {
	if k == Unknown {
		return 0, errors.New("Unknown kubernetes version")
	}

	parts := strings.Split(k.String(), ".")
	version, err := strconv.ParseInt(parts[position], 10, 8)
	if err != nil {
		return 0, err
	}

	monitor.WithFields(map[string]interface{}{
		"number":   version,
		"position": position,
		"string":   k,
	}).Debug("Extracted from semantic version")

	return int(version), nil
}

func softwareContains(this common.Software, that common.Software) bool {
	return contains(this.Swap, that.Swap) &&
		contains(this.Kubelet, that.Kubelet) &&
		contains(this.Kubeadm, that.Kubeadm) &&
		contains(this.Kubectl, that.Kubectl) &&
		contains(this.Containerruntime, that.Containerruntime) &&
		contains(this.KeepaliveD, that.KeepaliveD) &&
		contains(this.Nginx, that.Nginx) &&
		contains(this.Hostname, that.Hostname) &&
		sysctl.Contains(this.Sysctl, that.Sysctl) &&
		contains(this.Health, that.Health)
}

func contains(this, that common.Package) bool {
	return that.Version == "" && that.Config == nil || common.PackageEquals(this, that)
}

func softwareDefines(this common.Software, that common.Software) bool {
	return defines(this.Swap, that.Swap) &&
		defines(this.Kubelet, that.Kubelet) &&
		defines(this.Kubeadm, that.Kubeadm) &&
		defines(this.Kubectl, that.Kubectl) &&
		defines(this.Containerruntime, that.Containerruntime) &&
		defines(this.KeepaliveD, that.KeepaliveD) &&
		defines(this.Nginx, that.Nginx) &&
		defines(this.Hostname, that.Hostname) &&
		defines(this.Sysctl, that.Sysctl) &&
		defines(this.Health, that.Health)
}

func defines(this, that common.Package) bool {
	zeroPkg := common.Package{}
	defines := common.PackageEquals(that, zeroPkg) || !common.PackageEquals(this, zeroPkg)
	return defines
}
