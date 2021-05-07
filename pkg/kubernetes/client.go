package kubernetes

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"time"

	extensions "k8s.io/api/extensions/v1beta1"
	utilyaml "k8s.io/apimachinery/pkg/util/yaml"
	k8syaml "sigs.k8s.io/yaml"

	"github.com/caos/orbos/pkg/labels"

	"github.com/caos/orbos/internal/helpers"
	"github.com/caos/orbos/mntr"
	apps "k8s.io/api/apps/v1"
	batch "k8s.io/api/batch/v1"
	"k8s.io/api/batch/v1beta1"
	core "k8s.io/api/core/v1"
	policy "k8s.io/api/policy/v1beta1"
	rbac "k8s.io/api/rbac/v1"
	apixv1beta1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	apixv1beta1client "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset/typed/apiextensions/v1beta1"
	macherrs "k8s.io/apimachinery/pkg/api/errors"
	mach "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/discovery/cached/memory"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	clgocore "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/restmapper"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/remotecommand"
)

const TaintKeyPrefix = "node.orbos.ch/"

type NodeWithKubeadm interface {
	Execute(stdin io.Reader, cmd string) ([]byte, error)
}

type IDFunc func() string

func (i IDFunc) ID() string {
	return i()
}

type ClientInt interface {
	ApplyService(rsc *core.Service) error
	DeleteService(namespace, name string) error

	GetJob(namespace, name string) (*batch.Job, error)
	ApplyJob(rsc *batch.Job) error
	ApplyJobDryRun(rsc *batch.Job) error
	DeleteJob(namespace string, name string) error
	WaitUntilJobCompleted(namespace string, name string, timeout time.Duration) error

	ApplyServiceAccount(rsc *core.ServiceAccount) error
	DeleteServiceAccount(namespace, name string) error

	ApplyStatefulSet(rsc *apps.StatefulSet, force bool) error
	DeleteStatefulset(namespace, name string) error
	WaitUntilStatefulsetIsReady(namespace string, name string, containerCheck, readyCheck bool, timeout time.Duration) error

	ExecInPodWithOutput(namespace, name, container, command string) (string, error)
	ExecInPod(namespace, name, container, command string) error

	GetDeployment(namespace, name string) (*apps.Deployment, error)
	ApplyDeployment(rsc *apps.Deployment, force bool) error
	DeleteDeployment(namespace, name string) error
	PatchDeployment(namespace, name string, data string) error
	WaitUntilDeploymentReady(namespace string, name string, containerCheck, readyCheck bool, timeout time.Duration) error
	ScaleDeployment(namespace, name string, replicaCount int) error
	ExecInPodOfDeployment(namespace, name, container, command string) error

	CheckCRD(name string) (*apixv1beta1.CustomResourceDefinition, error)
	GetNamespacedCRDResource(group, version, kind, namespace, name string) (*unstructured.Unstructured, error)
	ApplyNamespacedCRDResource(group, version, kind, namespace, name string, crd *unstructured.Unstructured) error
	DeleteNamespacedCRDResource(group, version, kind, namespace, name string) error
	ApplyCRDResource(crd *unstructured.Unstructured) error
	DeleteCRDResource(group, version, kind, name string) error

	ApplyCronJob(rsc *v1beta1.CronJob) error
	DeleteCronJob(namespace string, name string) error

	ListSecrets(namespace string, labels map[string]string) (*core.SecretList, error)
	GetSecret(namespace string, name string) (*core.Secret, error)
	ApplySecret(rsc *core.Secret) error
	DeleteSecret(namespace, name string) error
	WaitForSecret(namespace string, name string, timeout time.Duration) error

	GetConfigMap(namespace, name string) (*core.ConfigMap, error)
	ApplyConfigmap(rsc *core.ConfigMap) error
	DeleteConfigmap(namespace, name string) error
	WaitForConfigMap(namespace string, name string, timeout time.Duration) error

	ApplyRole(rsc *rbac.Role) error
	DeleteRole(namespace, name string) error

	ApplyClusterRole(rsc *rbac.ClusterRole) error
	DeleteClusterRole(name string) error

	ApplyIngress(rsc *extensions.Ingress) error
	DeleteIngress(namespace, name string) error

	ApplyRoleBinding(rsc *rbac.RoleBinding) error
	DeleteRoleBinding(namespace, name string) error

	ApplyClusterRoleBinding(rsc *rbac.ClusterRoleBinding) error
	DeleteClusterRoleBinding(name string) error

	ApplyPodDisruptionBudget(rsc *policy.PodDisruptionBudget) error
	DeletePodDisruptionBudget(namespace string, name string) error

	ApplyNamespace(rsc *core.Namespace) error
	DeleteNamespace(name string) error

	ListPersistentVolumes() (*core.PersistentVolumeList, error)

	ApplyPlainYAML(mntr.Monitor, []byte) error
}

var _ ClientInt = (*Client)(nil)

type Client struct {
	monitor           mntr.Monitor
	set               *kubernetes.Clientset
	dynamic           dynamic.Interface
	apixv1beta1client *apixv1beta1client.ApiextensionsV1beta1Client
	mapper            *restmapper.DeferredDiscoveryRESTMapper
	restConfig        *rest.Config
	available         bool
}

func NewK8sClientWithPath(monitor mntr.Monitor, kubeconfigPath string) (*Client, error) {
	kubeconfigStr := ""
	if kubeconfigPath != "" {
		value, err := ioutil.ReadFile(helpers.PruneHome(kubeconfigPath))
		if err != nil {
			monitor.Error(err)
			return nil, err
		}
		kubeconfigStr = string(value)
	}

	return NewK8sClient(monitor, &kubeconfigStr)
}

func newClient(monitor mntr.Monitor) *Client {
	return &Client{monitor: monitor}
}

func NewK8sClient(monitor mntr.Monitor, kubeconfig *string) (*Client, error) {
	kc := newClient(monitor)
	if err := kc.init(kubeconfig); err != nil {
		return nil, err
	}
	return kc, nil
}

func NewK8sClientWithConfig(monitor mntr.Monitor, conf *rest.Config) (*Client, error) {
	kc := newClient(monitor)
	if err := kc.initConfig(conf); err != nil {
		return nil, err
	}
	return kc, nil
}

func (c *Client) checkConnectivity() error {

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := c.set.CoreV1().Nodes().Get(ctx, "<<<-impo$$ible->>>", mach.GetOptions{})
	if err == nil || macherrs.IsNotFound(err) {
		return nil
	}
	return err
}

func (c *Client) nodeApi() clgocore.NodeInterface {
	return c.set.CoreV1().Nodes()
}

type File struct {
	Name    string
	Content []byte
}

func (c *Client) ApplyNamespace(rsc *core.Namespace) error {
	resources := c.set.CoreV1().Namespaces()
	return c.applyResource("namespace", rsc.GetName(), func() error {

		_, err := resources.Create(context.Background(), rsc, mach.CreateOptions{})
		return err
	}, func() error {
		_, err := resources.Update(context.Background(), rsc, mach.UpdateOptions{})
		return err
	})
}
func (c *Client) DeleteNamespace(name string) error {
	return c.set.CoreV1().Namespaces().Delete(context.Background(), name, mach.DeleteOptions{})
}

func (c *Client) ListNamespaces() (*core.NamespaceList, error) {
	return c.set.CoreV1().Namespaces().List(context.Background(), mach.ListOptions{})
}

func (c *Client) ListSecrets(namespace string, labels map[string]string) (*core.SecretList, error) {
	labelSelector := ""
	for k, v := range labels {
		if labelSelector == "" {
			labelSelector = fmt.Sprintf("%s=%s", k, v)
		} else {
			labelSelector = fmt.Sprintf("%s, %s=%s", labelSelector, k, v)
		}
	}

	return c.set.CoreV1().Secrets(namespace).List(context.Background(), mach.ListOptions{LabelSelector: labelSelector})
}

func (c *Client) ListPersistentVolumes() (*core.PersistentVolumeList, error) {
	return c.set.CoreV1().PersistentVolumes().List(context.Background(), mach.ListOptions{})
}

func (c *Client) ScaleDeployment(namespace, name string, replicaCount int) error {
	patch := []byte(`{"spec":{"replicas":` + strconv.Itoa(replicaCount) + `}}`)
	_, err := c.set.AppsV1().Deployments(namespace).Patch(context.Background(), name, types.StrategicMergePatchType, patch, mach.PatchOptions{})
	return err
}

func (c *Client) GetDeployment(namespace, name string) (*apps.Deployment, error) {
	return c.set.AppsV1().Deployments(namespace).Get(context.Background(), name, mach.GetOptions{})
}

func (c *Client) ApplyDeployment(rsc *apps.Deployment, force bool) error {
	resources := c.set.AppsV1().Deployments(rsc.GetNamespace())
	rscLabels, err := labels.NameFrom(rsc.Labels)
	if err != nil {
		return err
	}

	rscSelector, err := labels.SelectorFrom(rsc.Spec.Selector.MatchLabels)
	if err != nil {
		return err
	}

	return c.applyController(
		"deployment",
		force,
		rscLabels,
		rscSelector,
		func() (*labels.Selector, error) {
			sts, err := resources.Get(context.Background(), rsc.GetName(), mach.GetOptions{})
			if err != nil {
				return nil, err
			}

			return labels.SelectorFrom(sts.Spec.Selector.MatchLabels)
		},
		func() error {
			_, err := resources.Create(context.Background(), rsc, mach.CreateOptions{})
			return err
		},
		func() error {
			_, err = resources.Update(context.Background(), rsc, mach.UpdateOptions{})
			return err
		},
		func() error {
			return c.DeleteDeployment(rsc.GetNamespace(), rsc.GetName())
		},
	)
}

func (c *Client) DeleteDeployment(namespace, name string) error {
	return c.set.AppsV1().Deployments(namespace).Delete(context.Background(), name, mach.DeleteOptions{})
}
func (c *Client) PatchDeployment(namespace, name string, data string) error {
	_, err := c.set.AppsV1().Deployments(namespace).Patch(context.Background(), name, types.StrategicMergePatchType, []byte(data), mach.PatchOptions{})
	return err
}

func (c *Client) WaitUntilDeploymentReady(namespace string, name string, containerCheck, readyCheck bool, timeout time.Duration) error {
	returnChannel := make(chan error, 1)
	go func() {
		ctx := context.Background()
		deploy, err := c.set.AppsV1().Deployments(namespace).Get(ctx, name, mach.GetOptions{})
		if err != nil {
			returnChannel <- err
			return
		}

		labelSelector := getLabelSelector(deploy.Spec.Selector.MatchLabels)

		watch, err := c.set.CoreV1().Pods(namespace).Watch(ctx, mach.ListOptions{
			LabelSelector: labelSelector,
		})
		defer watch.Stop()
		if err != nil {
			returnChannel <- err
			return
		}
		replicas := deploy.Spec.Replicas

		returnChannel <- waitForPodsPhase(watch, int(*replicas), core.PodRunning, containerCheck, readyCheck)
	}()

	select {
	case res := <-returnChannel:
		return res
	case <-time.After(timeout):
		return errors.New("timeout while waiting for deployment to be ready")
	}
}

func (c *Client) ApplyService(rsc *core.Service) error {
	resources := c.set.CoreV1().Services(rsc.GetNamespace())
	return c.applyResource("service", rsc.GetName(), func() error {
		_, err := resources.Create(context.Background(), rsc, mach.CreateOptions{})
		return err
	}, func() error {
		svc, err := resources.Get(context.Background(), rsc.Name, mach.GetOptions{})
		if err != nil {
			return err
		}
		rsc.Spec.ClusterIP = svc.Spec.ClusterIP
		rsc.ObjectMeta.ResourceVersion = svc.ObjectMeta.ResourceVersion
		_, err = resources.Update(context.Background(), rsc, mach.UpdateOptions{})
		return err
	})
}

func (c *Client) DeleteService(namespace, name string) error {
	return c.set.CoreV1().Services(namespace).Delete(context.Background(), name, mach.DeleteOptions{})
}

func (c *Client) GetJob(namespace, name string) (*batch.Job, error) {
	return c.set.BatchV1().Jobs(namespace).Get(context.Background(), name, mach.GetOptions{})
}

func (c *Client) ApplyJobDryRun(rsc *batch.Job) error {
	resources := c.set.BatchV1().Jobs(rsc.Namespace)
	return c.applyResource("job", rsc.GetName(), func() error {
		res, err := resources.Create(context.Background(), rsc, mach.CreateOptions{DryRun: []string{mach.DryRunAll}})
		if err != nil {
			return err
		}
		*rsc = *res
		return nil
	}, func() error {
		j, err := resources.Get(context.Background(), rsc.GetName(), mach.GetOptions{})
		if err != nil {
			return err
		}
		if j.GetName() == rsc.GetName() && j.GetNamespace() == rsc.GetNamespace() {
			res, err := resources.Update(context.Background(), rsc, mach.UpdateOptions{DryRun: []string{mach.DryRunAll}})
			if err != nil {
				return err
			}
			*rsc = *res
			return nil
		}
		return nil
	})
}

func (c *Client) ApplyJob(rsc *batch.Job) error {
	resources := c.set.BatchV1().Jobs(rsc.Namespace)
	return c.applyResource("job", rsc.GetName(), func() error {
		_, err := resources.Create(context.Background(), rsc, mach.CreateOptions{})
		return err
	}, func() error {
		j, err := resources.Get(context.Background(), rsc.GetName(), mach.GetOptions{})
		if err != nil {
			return err
		}
		if j.GetName() == rsc.GetName() && j.GetNamespace() == rsc.GetNamespace() {
			_, err := resources.Update(context.Background(), rsc, mach.UpdateOptions{})
			return err
		}
		return nil
	})
}

func (c *Client) WaitUntilJobCompleted(namespace string, name string, timeout time.Duration) error {
	returnChannel := make(chan error, 1)
	go func() {
		ctx := context.Background()
		job, err := c.set.BatchV1().Jobs(namespace).Get(ctx, name, mach.GetOptions{})
		if err != nil {
			returnChannel <- err
			return
		}

		if job.Status.Succeeded > 0 {
			returnChannel <- nil
			return
		}

		labelSelector := getLabelSelector(job.Spec.Selector.MatchLabels)

		watch, err := c.set.CoreV1().Pods(namespace).Watch(ctx, mach.ListOptions{
			LabelSelector: labelSelector,
		})
		defer watch.Stop()
		if err != nil {
			returnChannel <- err
			return
		}

		returnChannel <- waitForPodsPhase(watch, 1, core.PodSucceeded, false, false)
	}()

	select {
	case res := <-returnChannel:
		return res
	case <-time.After(timeout):
		return fmt.Errorf("timeout after %s while waiting for job to complete", timeout)
	}
}

func (c *Client) DeleteJob(namespace string, name string) error {
	job, err := c.GetJob(namespace, name)
	if err != nil {
		return err
	}

	if err := c.set.BatchV1().Jobs(namespace).Delete(context.Background(), name, mach.DeleteOptions{}); err != nil {
		return err
	}

	//Pod cleanup if necessary
	return c.DeletePodsByLabels(namespace, job.Spec.Selector.MatchLabels)
}

func (c *Client) ApplyCronJob(rsc *v1beta1.CronJob) error {
	resources := c.set.BatchV1beta1().CronJobs(rsc.Namespace)
	return c.applyResource("cronjob", rsc.GetName(), func() error {
		_, err := resources.Create(context.Background(), rsc, mach.CreateOptions{})
		return err
	}, func() error {
		j, err := resources.Get(context.Background(), rsc.GetName(), mach.GetOptions{})
		if err != nil {
			return err
		}
		if j.GetName() == rsc.GetName() && j.GetNamespace() == rsc.GetNamespace() {
			_, err := resources.Update(context.Background(), rsc, mach.UpdateOptions{})
			return err
		}
		return nil
	})
}

func (c *Client) DeleteCronJob(namespace string, name string) error {
	return c.set.BatchV1beta1().CronJobs(namespace).Delete(context.Background(), name, mach.DeleteOptions{})
}

func (c *Client) ListPods(namespace string, labels map[string]string) (*core.PodList, error) {
	return c.set.CoreV1().Pods(namespace).List(context.Background(), mach.ListOptions{LabelSelector: getLabelSelector(labels)})
}

func (c *Client) ApplyPodDisruptionBudget(rsc *policy.PodDisruptionBudget) error {
	resources := c.set.PolicyV1beta1().PodDisruptionBudgets(rsc.Namespace)
	return c.applyResource("poddisruptionbudget", rsc.GetName(), func() error {
		_, err := resources.Create(context.Background(), rsc, mach.CreateOptions{})
		return err
	}, func() error {
		pdb, err := resources.Get(context.Background(), rsc.GetName(), mach.GetOptions{})
		if err != nil {
			return err
		}
		if pdb.GetName() == rsc.GetName() && pdb.GetNamespace() == rsc.GetNamespace() {
			rsc.ResourceVersion = pdb.ResourceVersion
			_, err := resources.Update(context.Background(), rsc, mach.UpdateOptions{})
			return err
		}
		return nil
	})
}
func (c *Client) DeletePodDisruptionBudget(namespace string, name string) error {
	return c.set.PolicyV1beta1().PodDisruptionBudgets(namespace).Delete(context.Background(), name, mach.DeleteOptions{})
}

func (c *Client) ApplyStatefulSet(rsc *apps.StatefulSet, force bool) error {
	resources := c.set.AppsV1().StatefulSets(rsc.Namespace)
	rscLabels, err := labels.NameFrom(rsc.Labels)
	if err != nil {
		return err
	}

	rscSelector, err := labels.SelectorFrom(rsc.Spec.Selector.MatchLabels)
	if err != nil {
		return err
	}

	return c.applyController(
		"statefulset",
		force,
		rscLabels,
		rscSelector,
		func() (*labels.Selector, error) {
			sts, err := resources.Get(context.Background(), rsc.GetName(), mach.GetOptions{})
			if err != nil {
				return nil, err
			}

			return labels.SelectorFrom(sts.Spec.Selector.MatchLabels)
		},
		func() error {
			_, err := resources.Create(context.Background(), rsc, mach.CreateOptions{})
			return err
		},
		func() error {
			_, err = resources.Update(context.Background(), rsc, mach.UpdateOptions{})
			return err
		},
		func() error {
			return c.DeleteStatefulset(rsc.GetNamespace(), rsc.GetName())
		},
	)
}

func (c *Client) DeleteStatefulset(namespace, name string) error {
	return c.set.AppsV1().StatefulSets(namespace).Delete(context.Background(), name, mach.DeleteOptions{})
}

func (c *Client) WaitUntilStatefulsetIsReady(namespace string, name string, containerCheck, readyCheck bool, timeout time.Duration) error {
	returnChannel := make(chan error, 1)
	go func() {
		ctx := context.Background()
		sfs, err := c.set.AppsV1().StatefulSets(namespace).Get(ctx, name, mach.GetOptions{})
		if err != nil {
			returnChannel <- err
			return
		}

		labelSelector := getLabelSelector(sfs.Spec.Selector.MatchLabels)

		watch, err := c.set.CoreV1().Pods(namespace).Watch(ctx, mach.ListOptions{
			LabelSelector: labelSelector,
		})
		defer watch.Stop()
		if err != nil {
			returnChannel <- err
			return
		}
		replicas := sfs.Spec.Replicas

		returnChannel <- waitForPodsPhase(watch, int(*replicas), core.PodRunning, containerCheck, readyCheck)
	}()

	select {
	case res := <-returnChannel:
		return res
	case <-time.After(timeout):
		return errors.New("timeout while waiting for job to complete")
	}
}

func (c *Client) ApplySecret(rsc *core.Secret) error {
	resources := c.set.CoreV1().Secrets(rsc.GetNamespace())
	return c.applyResource("secret", rsc.GetName(), func() error {
		_, err := resources.Create(context.Background(), rsc, mach.CreateOptions{})
		return err
	}, func() error {
		_, err := resources.Update(context.Background(), rsc, mach.UpdateOptions{})
		return err
	})
}
func (c *Client) GetSecret(namespace string, name string) (*core.Secret, error) {
	return c.set.CoreV1().Secrets(namespace).Get(context.Background(), name, mach.GetOptions{})
}

func (c *Client) WaitForSecret(namespace string, name string, timeout time.Duration) error {
	ctx := context.Background()
	return await(
		timeout,
		func() (interface{}, error) {
			return c.set.CoreV1().Secrets(namespace).Get(ctx, name, mach.GetOptions{})
		},
	)
}

func await(timeout time.Duration, getResource func() (interface{}, error)) error {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	for {
		select {
		case <-ticker.C:
			resource, err := getResource()
			if err != nil && !macherrs.IsNotFound(err) {
				return err
			} else if resource != nil {
				return nil
			}

		case <-timer.C:
			return errors.New("timeout while waiting for secret to be created")
		}
	}
}

func (c *Client) DeleteSecret(namespace, name string) error {
	return c.set.CoreV1().Secrets(namespace).Delete(context.Background(), name, mach.DeleteOptions{})
}

func (c *Client) GetConfigMap(namespace, name string) (*core.ConfigMap, error) {
	return c.set.CoreV1().ConfigMaps(namespace).Get(context.Background(), name, mach.GetOptions{})
}

func (c *Client) ApplyConfigmap(rsc *core.ConfigMap) error {
	resources := c.set.CoreV1().ConfigMaps(rsc.GetNamespace())
	return c.applyResource("secret", rsc.GetName(), func() error {
		_, err := resources.Create(context.Background(), rsc, mach.CreateOptions{})
		return err
	}, func() error {
		_, err := resources.Update(context.Background(), rsc, mach.UpdateOptions{})
		return err
	})
}

func (c *Client) DeleteConfigmap(namespace, name string) error {
	return c.set.CoreV1().ConfigMaps(namespace).Delete(context.Background(), name, mach.DeleteOptions{})
}

func (c *Client) ApplyServiceAccount(rsc *core.ServiceAccount) error {
	resources := c.set.CoreV1().ServiceAccounts(rsc.Namespace)
	return c.applyResource("serviceaccount", rsc.GetName(), func() error {
		_, err := resources.Create(context.Background(), rsc, mach.CreateOptions{})
		return err
	}, func() error {
		sa, err := resources.Get(context.Background(), rsc.GetName(), mach.GetOptions{})
		if err != nil {
			return err
		}

		different := false
		//workaround as 1 token will always be created by kubeapi
		if (!(sa.Secrets != nil && len(sa.Secrets) == 1 && (rsc.Secrets == nil || len(rsc.Secrets) == 0)) && (!reflect.DeepEqual(sa.Secrets, rsc.Secrets))) ||
			(sa.ImagePullSecrets != nil && rsc.ImagePullSecrets != nil && !reflect.DeepEqual(sa.ImagePullSecrets, rsc.ImagePullSecrets)) ||
			(sa.AutomountServiceAccountToken != nil && rsc.AutomountServiceAccountToken != nil && *sa.AutomountServiceAccountToken != *rsc.AutomountServiceAccountToken) {
			different = true
		}

		if different &&
			sa.GetName() == rsc.GetName() &&
			sa.GetNamespace() == rsc.GetNamespace() {

			_, err := resources.Update(context.Background(), rsc, mach.UpdateOptions{})
			return err
		}
		return nil
	})
}

func (c *Client) WaitForConfigMap(namespace string, name string, timeout time.Duration) error {
	ctx := context.Background()
	return await(
		timeout,
		func() (interface{}, error) {
			return c.set.CoreV1().ConfigMaps(namespace).Get(ctx, name, mach.GetOptions{})
		},
	)
}

func (c *Client) DeleteServiceAccount(namespace, name string) error {
	return c.set.CoreV1().ServiceAccounts(namespace).Delete(context.Background(), name, mach.DeleteOptions{})
}

func (c *Client) ApplyIngress(rsc *extensions.Ingress) error {
	resources := c.set.ExtensionsV1beta1().Ingresses(rsc.GetNamespace())
	return c.applyResource("ingress", rsc.GetName(), func() error {
		_, err := resources.Create(context.Background(), rsc, mach.CreateOptions{})
		return err
	}, func() error {
		svc, err := resources.Get(context.Background(), rsc.Name, mach.GetOptions{})
		if err != nil {
			return err
		}
		rsc.ObjectMeta.ResourceVersion = svc.ObjectMeta.ResourceVersion
		_, err = resources.Update(context.Background(), rsc, mach.UpdateOptions{})
		return err
	})
}

func (c *Client) DeleteIngress(namespace, name string) error {
	return c.set.ExtensionsV1beta1().Ingresses(namespace).Delete(context.Background(), name, mach.DeleteOptions{})
}

func (c *Client) ApplyRole(rsc *rbac.Role) error {
	resources := c.set.RbacV1().Roles(rsc.Namespace)
	return c.applyResource("role", rsc.GetName(), func() error {
		_, err := resources.Create(context.Background(), rsc, mach.CreateOptions{})
		return err
	}, func() error {
		_, err := resources.Update(context.Background(), rsc, mach.UpdateOptions{})
		return err
	})
}
func (c *Client) DeleteRole(namespace, name string) error {
	return c.set.RbacV1().Roles(namespace).Delete(context.Background(), name, mach.DeleteOptions{})
}

func (c *Client) ApplyClusterRole(rsc *rbac.ClusterRole) error {
	resources := c.set.RbacV1().ClusterRoles()
	return c.applyResource("clusterrole", rsc.GetName(), func() error {
		_, err := resources.Create(context.Background(), rsc, mach.CreateOptions{})
		return err
	}, func() error {
		_, err := resources.Update(context.Background(), rsc, mach.UpdateOptions{})
		return err
	})
}

func (c *Client) DeleteClusterRole(name string) error {
	return c.set.RbacV1().ClusterRoles().Delete(context.Background(), name, mach.DeleteOptions{})
}

func (c *Client) ApplyRoleBinding(rsc *rbac.RoleBinding) error {
	resources := c.set.RbacV1().RoleBindings(rsc.Namespace)
	return c.applyResource("rolebinding", rsc.GetName(), func() error {
		_, err := resources.Create(context.Background(), rsc, mach.CreateOptions{})
		return err
	}, func() error {
		_, err := resources.Update(context.Background(), rsc, mach.UpdateOptions{})
		return err
	})
}

func (c *Client) DeleteRoleBinding(namespace, name string) error {
	return c.set.RbacV1().RoleBindings(namespace).Delete(context.Background(), name, mach.DeleteOptions{})
}

func (c *Client) ApplyClusterRoleBinding(rsc *rbac.ClusterRoleBinding) error {
	resources := c.set.RbacV1().ClusterRoleBindings()
	return c.applyResource("clusterrolebinding", rsc.GetName(), func() error {
		_, err := resources.Create(context.Background(), rsc, mach.CreateOptions{})
		return err
	}, func() error {
		_, err := resources.Update(context.Background(), rsc, mach.UpdateOptions{})
		return err
	})
}

func (c *Client) DeleteClusterRoleBinding(name string) error {
	return c.set.RbacV1().ClusterRoleBindings().Delete(context.Background(), name, mach.DeleteOptions{})
}

type recreateErr struct{}

func (r *recreateErr) Error() string { return "recreate" }

func (c *Client) applyResource(object, name string, create func() error, update func() error) (err error) {

	defer func() {
		if err != nil {
			err = fmt.Errorf("applying %s %s failed: %w", object, name, err)
		}
	}()

	err = update()
	_, recreate := err.(*recreateErr)
	if err == nil || (!macherrs.IsNotFound(err) && !recreate) {
		return err
	}
	return create()
}

func (c *Client) applyController(
	controllerType string,
	force bool,
	newNameLabels *labels.Name,
	newSelector *labels.Selector,
	getCurrentSelectorFunc func() (*labels.Selector, error),
	createResource func() error,
	updateResource func() error,
	deleteResource func() error) error {

	return c.applyResource(
		controllerType,
		newNameLabels.Name(),
		createResource,
		func() error {

			currentSelector, err := getCurrentSelectorFunc()
			if err != nil {
				return err
			}

			if newSelector.Equal(currentSelector) {
				return updateResource()
			}

			if !force {
				return fmt.Errorf("only recreating %s when force is true", controllerType)
			}

			if err := deleteResource(); err != nil {
				return err
			}
			return &recreateErr{}
		},
	)
}

func (c *Client) init(kubeconfig *string) (err error) {
	defer func() {
		if err != nil {
			err = fmt.Errorf("refreshing Kubernetes client failed: %w", err)
		}
	}()

	restCfg := new(rest.Config)
	if kubeconfig == nil || *kubeconfig == "" {
		restCfg, err = rest.InClusterConfig()
		if err != nil {
			return err
		}
	} else {
		clientCfg, err := clientcmd.NewClientConfigFromBytes([]byte(*kubeconfig))
		if err != nil {
			return err
		}

		restCfg, err = clientCfg.ClientConfig()
		if err != nil {
			return err
		}
	}

	return c.refreshAllClients(restCfg)
}

func (c *Client) initConfig(config *rest.Config) (err error) {
	return c.refreshAllClients(config)
}

func (c *Client) refreshAllClients(config *rest.Config) error {
	c.restConfig = config

	set, err := kubernetes.NewForConfig(config)
	if err != nil {
		return err
	}
	c.set = set

	apixv1beta1clientC, err := apixv1beta1client.NewForConfig(config)
	if err != nil {
		return err
	}
	c.apixv1beta1client = apixv1beta1clientC

	dynamicC, err := dynamic.NewForConfig(config)
	if err != nil {
		return err
	}
	c.dynamic = dynamicC

	dc, err := discovery.NewDiscoveryClientForConfig(config)
	if err != nil {
		return err
	}
	c.mapper = restmapper.NewDeferredDiscoveryRESTMapper(memory.NewMemCacheClient(dc))

	return c.checkConnectivity()
}

func (c *Client) GetNode(id string) (node *core.Node, err error) {
	return c.nodeApi().Get(context.Background(), id, mach.GetOptions{})
}

func (c *Client) ListNodes(filterID ...string) (nodes []core.Node, err error) {
	defer func() {
		if err != nil {
			err = fmt.Errorf("listing nodes %s failed: %w", strings.Join(filterID, ", "), err)
		}
	}()

	labelSelector := ""
	for _, id := range filterID {
		labelSelector = fmt.Sprintf("%s,name=%s", labelSelector, id)
	}
	if len(labelSelector) > 0 {
		labelSelector = labelSelector[1:]
	}

	nodeList, err := c.nodeApi().List(context.Background(), mach.ListOptions{
		LabelSelector: labelSelector,
	})

	if err != nil {
		return nil, err
	}
	return nodeList.Items, nil
}

func (c *Client) UpdateNode(node *core.Node) (err error) {

	defer func() {
		if err != nil {
			err = fmt.Errorf("updating node %s failed: %w", node.GetName(), err)
		}
	}()

	node.ResourceVersion = ""
	_, err = c.nodeApi().Update(context.Background(), node, mach.UpdateOptions{})
	return err
}

type DrainReason int

const (
	Updating DrainReason = iota
	Rebooting
	Deleting
)

func (c *Client) cordon(node *core.Node, reason DrainReason) (err error) {
	defer func() {
		if err != nil {
			err = fmt.Errorf("cordoning node %s failed: %w", node.GetName(), err)
		}
	}()

	monitor := c.monitor.WithFields(map[string]interface{}{
		"machine": node.GetName(),
	})
	monitor.Info("Cordoning node")

	if c.Tainted(node, reason) {
		return nil
	}
	node.Spec.Taints = append(node.Spec.Taints, core.Taint{
		Key:    TaintKeyPrefix + reason.String(),
		Effect: "NoSchedule",
	})

	if err := c.UpdateNode(node); err != nil {
		return err
	}
	monitor.Info("Node cordoned")
	return nil
}

type Machine interface {
	GetUpdating() bool
	SetUpdating(bool)
	GetJoined() bool
	SetJoined(bool)
}

func (c *Client) Tainted(node *core.Node, reason DrainReason) bool {
	return c.tainted(node.Spec.Taints, reason) != -1
}

func (c *Client) tainted(taints []core.Taint, reason DrainReason) int {

	for idx, taint := range taints {
		if taint.Key == TaintKeyPrefix+reason.String() {
			return idx
		}
	}
	return -1
}

func (c *Client) RemoveFromTaints(taints []core.Taint, reason DrainReason) (result []core.Taint) {
	idx := c.tainted(taints, reason)
	if idx < 0 {
		return taints
	}
	return append(taints[0:idx], taints[idx+1:]...)
}

func (c *Client) Drain(machine Machine, node *core.Node, reason DrainReason) (err error) {
	defer func() {
		if err != nil {
			err = fmt.Errorf("draining node %s failed", node.GetName(), err)
		}
	}()

	monitor := c.monitor.WithFields(map[string]interface{}{
		"machine": node.GetName(),
	})
	monitor.Info("Draining node")

	if err = c.cordon(node, reason); err != nil {
		return err
	}

	if err := c.evictPods(node); err != nil {
		return err
	}
	if !machine.GetUpdating() {
		machine.SetUpdating(true)
		monitor.Changed("Node drained")
	}
	return nil
}

func (c *Client) DeleteNode(name string) error {
	err := c.set.CoreV1().Nodes().Delete(context.Background(), name, mach.DeleteOptions{})
	if macherrs.IsNotFound(err) {
		return nil
	}
	return err
}

func (c *Client) evictPods(node *core.Node) (err error) {

	defer func() {
		if err != nil {
			err = fmt.Errorf("evicting pods from node %s failed: %w", node.GetName(), err)
		}
	}()

	monitor := c.monitor.WithFields(map[string]interface{}{
		"machine": node.GetName(),
	})

	monitor.Info("Evicting pods")

	selector := fmt.Sprintf("spec.nodeName=%s,status.phase=Running", node.Name)
	podItems, err := c.set.CoreV1().Pods("").List(context.Background(), mach.ListOptions{
		FieldSelector: selector,
	})
	if err != nil {
		return fmt.Errorf("listing pods with selector %s failed: %w", selector, err)
	}

	// --ignore-daemonsets
	pods := deriveFilter(func(pod core.Pod) bool {
		controllerRef := mach.GetControllerOf(&pod)
		return controllerRef == nil || controllerRef.Kind != apps.SchemeGroupVersion.WithKind("DaemonSet").Kind
	}, append([]core.Pod{}, podItems.Items...))

	var wg sync.WaitGroup
	synchronizer := helpers.NewSynchronizer(&wg)

	for _, p := range pods {
		wg.Add(1)
		go func(pod core.Pod) {

			var gracePeriodSeconds int64 = 60
			monitor := c.monitor.WithFields(map[string]interface{}{
				"pod":       pod.GetName(),
				"namespace": pod.GetNamespace(),
			})
			monitor.Debug("Evicting pod")

			watcher, goErr := c.set.CoreV1().Pods(pod.Namespace).Watch(context.Background(), mach.ListOptions{
				FieldSelector: fmt.Sprintf("metadata.name=%s", pod.Name),
			})
			if goErr != nil {
				synchronizer.Done(nil)
				return
			}
			defer watcher.Stop()

			if goErr := c.set.PolicyV1beta1().Evictions(pod.Namespace).Evict(context.Background(), &policy.Eviction{
				TypeMeta: mach.TypeMeta{
					Kind:       "EvictionKind",
					APIVersion: c.set.PolicyV1beta1().RESTClient().APIVersion().String(),
				},
				ObjectMeta: mach.ObjectMeta{
					Name:      pod.Name,
					Namespace: pod.Namespace,
				},
				DeleteOptions: &mach.DeleteOptions{
					GracePeriodSeconds: &gracePeriodSeconds,
				},
			}); goErr != nil {
				synchronizer.Done(fmt.Errorf("evicting pod %s failed: %w", pod.Name, goErr))
				return
			}
			monitor.Debug("Watching pod")

			timeout := time.After(time.Duration(safeUint64(pod.Spec.TerminationGracePeriodSeconds)) + 30)
			for {
				select {
				case event := <-watcher.ResultChan():
					wPod, ok := event.Object.(*core.Pod)
					if !ok {
						continue
					}
					monitor = monitor.WithFields(map[string]interface{}{
						"event": event.Type,
					})
					monitor.Debug("Pod event happened")
					if event.Type == watch.Deleted {
						monitor.WithFields(map[string]interface{}{
							"new_node": wPod.Spec.NodeName,
						}).Debug("Pod evicted")
						synchronizer.Done(nil)
						return
					}
				case <-timeout:

					delErr := c.set.CoreV1().Pods(pod.Namespace).Delete(context.Background(), pod.Name, mach.DeleteOptions{})
					if delErr != nil {
						delErr = fmt.Errorf("deleting pod %s after timout exceeded failed: %w", pod.Name, delErr)
					}
					synchronizer.Done(delErr)
					return
				}
			}
		}(p)
	}
	wg.Wait()

	if synchronizer.IsError() {
		return fmt.Errorf("concurrently evicting pods from node %s failed: %w", node.Name, synchronizer)
	}

	monitor.Info("Pods evicted")
	return nil
}

func safeUint64(ptr *int64) int64 {
	if ptr == nil {
		return 0
	}
	return *ptr
}
func (c *Client) DeletePodsByLabels(namespace string, labels map[string]string) error {
	return c.set.CoreV1().Pods(namespace).DeleteCollection(context.Background(), mach.DeleteOptions{}, mach.ListOptions{
		LabelSelector: getLabelSelector(labels),
	})
}

func (c *Client) DeletePod(namespace, name string) error {
	return c.set.CoreV1().Pods(namespace).Delete(context.Background(), name, mach.DeleteOptions{})
}

func getLabelSelector(labels map[string]string) string {
	labelSelector := ""
	for k, v := range labels {
		if labelSelector == "" {
			labelSelector = k + "=" + v
		} else {
			labelSelector = labelSelector + ", " + k + "=" + v

		}
	}
	return labelSelector
}

type podStatus struct {
	name              string
	phase             core.PodPhase
	containerStatuses []containerStatus
}
type containerStatus struct {
	containerState core.ContainerStatus
}

func waitForPodsPhase(watch watch.Interface, replicaCount int, phase core.PodPhase, containerCheck bool, readyCheck bool) error {
	statusChan := make(chan podStatus)
	pods := make(map[string]core.PodPhase, 0)

	if watch == nil {
		return errors.New("no pods watched")
	}

	go func() {
		for event := range watch.ResultChan() {
			p, ok := event.Object.(*core.Pod)
			if !ok {
				continue
			}
			containerStatuses := make([]containerStatus, 0)
			for _, podContainerStatus := range p.Status.ContainerStatuses {
				containerStatuses = append(containerStatuses, containerStatus{
					containerState: podContainerStatus,
				})
			}

			statusChan <- podStatus{p.Name, p.Status.Phase, containerStatuses}

		}
	}()

	runningPods := 0
	for runningPods < replicaCount {
		podStat := <-statusChan
		if podStat.phase == phase {
			running := true
			ready := true
			if containerCheck {
				running = checkContainer(podStat.containerStatuses)
			}
			if readyCheck {
				ready = checkReady(podStat.containerStatuses)
			}

			if running && ready {
				pods[podStat.name] = podStat.phase
			} else {
				delete(pods, podStat.name)
			}
		} else {
			delete(pods, podStat.name)
		}
		runningPods = len(pods)
	}
	return nil
}

func checkContainer(containers []containerStatus) bool {
	running := true
	for _, stat := range containers {
		if stat.containerState.State.Running == nil {
			running = false
		}
	}
	return running
}

func checkReady(containers []containerStatus) bool {
	ready := true
	for _, stat := range containers {
		if !stat.containerState.Ready {
			ready = false
		}
	}
	return ready
}

func (c *Client) CheckCRD(name string) (*apixv1beta1.CustomResourceDefinition, error) {
	crds := c.apixv1beta1client.CustomResourceDefinitions()
	return crds.Get(context.Background(), name, mach.GetOptions{})
}

func (c *Client) GetNamespacedCRDResource(group, version, kind, namespace, name string) (*unstructured.Unstructured, error) {
	mapping, err := c.mapper.RESTMapping(schema.GroupKind{
		Group: group,
		Kind:  kind,
	}, version)
	if err != nil {
		return nil, err
	}
	resource := c.dynamic.Resource(mapping.Resource).Namespace(namespace)

	return resource.Get(context.Background(), name, mach.GetOptions{})
}

func (c *Client) ApplyNamespacedCRDResource(group, version, kind, namespace, name string, crd *unstructured.Unstructured) error {
	mapping, err := c.mapper.RESTMapping(schema.GroupKind{
		Group: group,
		Kind:  kind,
	}, version)
	if err != nil {
		return err
	}

	resources := c.dynamic.Resource(mapping.Resource).Namespace(namespace)
	existing, err := resources.Get(context.Background(), name, mach.GetOptions{})
	if err != nil && !macherrs.IsNotFound(err) {
		return fmt.Errorf("getting existing crd %s of kind %s failed: %w", name, kind, err)
	}
	update := func() error {
		return err
	}
	if err == nil {
		crd.SetResourceVersion(existing.GetResourceVersion())
		update = func() error {
			_, err := resources.Update(context.Background(), crd, mach.UpdateOptions{})
			return err
		}
	}

	return c.applyResource("crd", name, func() error {
		_, err := resources.Create(context.Background(), crd, mach.CreateOptions{})
		return err
	}, update)
}

func (c *Client) DeleteNamespacedCRDResource(group, version, kind, namespace, name string) error {
	mapping, err := c.mapper.RESTMapping(schema.GroupKind{
		Group: group,
		Kind:  kind,
	}, version)
	if err != nil {
		return err
	}

	return c.dynamic.Resource(mapping.Resource).Namespace(namespace).Delete(context.Background(), name, mach.DeleteOptions{})
}

func (c *Client) ApplyCRDResource(crd *unstructured.Unstructured) error {
	kind := crd.Object["kind"].(string)
	apiVersion := strings.Split(crd.Object["apiVersion"].(string), "/")
	metadata := crd.Object["metadata"].(map[string]interface{})
	name := metadata["name"].(string)

	apiVersionGroup := ""
	apiVersionVersion := ""
	switch len(apiVersion) {
	case 2:
		apiVersionGroup = apiVersion[0]
		apiVersionVersion = apiVersion[1]
	case 1:
		apiVersionVersion = apiVersion[0]
	default:
		return fmt.Errorf("")
	}

	mapping, err := c.mapper.RESTMapping(schema.GroupKind{
		Group: apiVersionGroup,
		Kind:  kind,
	}, apiVersionVersion)
	if err != nil {
		return err
	}

	var resources dynamic.ResourceInterface
	clusterResources := c.dynamic.Resource(mapping.Resource)
	resources = clusterResources
	namespace, ok := metadata["namespace"]
	if ok && namespace.(string) != "" {
		resources = clusterResources.Namespace(namespace.(string))
	}

	existing, err := resources.Get(context.Background(), name, mach.GetOptions{})
	if err != nil && !macherrs.IsNotFound(err) {
		return fmt.Errorf("getting existing crd %s of kind %s failed: %w", name, kind, err)
	}
	if err == nil {
		crd.SetResourceVersion(existing.GetResourceVersion())
	}
	err = nil

	return c.applyResource("crd", name, func() error {
		_, err := resources.Create(context.Background(), crd, mach.CreateOptions{})
		return err
	}, func() error {
		_, err := resources.Update(context.Background(), crd, mach.UpdateOptions{})
		return err
	})
}

func (c *Client) DeleteCRDResource(group, version, kind, name string) error {
	mapping, err := c.mapper.RESTMapping(schema.GroupKind{
		Group: group,
		Kind:  kind,
	}, version)
	if err != nil {
		return err
	}

	return c.dynamic.Resource(mapping.Resource).Delete(context.Background(), name, mach.DeleteOptions{})
}

func (c *Client) ExecInPodOfDeployment(namespace, name, container, command string) error {
	cmd := []string{
		"sh",
		"-c",
		command,
	}
	deploy, err := c.GetDeployment(namespace, name)
	if err != nil {
		return err
	}

	labelSelector := ""
	for k, v := range deploy.Spec.Selector.MatchLabels {
		if labelSelector == "" {
			labelSelector = fmt.Sprintf("%s=%s", k, v)
		} else {
			labelSelector = fmt.Sprintf("%s, %s=%s", labelSelector, k, v)
		}
	}
	list, err := c.set.CoreV1().Pods(namespace).List(context.Background(), mach.ListOptions{
		LabelSelector: labelSelector,
	})
	if err != nil {
		return err
	}

	firstPod := list.Items[0]
	req := c.set.CoreV1().RESTClient().Post().Resource("pods").Namespace(namespace).Name(firstPod.Name).SubResource("exec")
	return c.execInPod(cmd, container, req)
}
func (c *Client) ExecInPod(namespace, name, container, command string) error {
	cmd := []string{
		"sh",
		"-c",
		command,
	}

	req := c.set.CoreV1().RESTClient().Post().Resource("pods").Namespace(namespace).Name(name).SubResource("exec")
	return c.execInPod(cmd, container, req)
}

func (c *Client) ExecInPodWithOutput(namespace, name, container, command string) (string, error) {
	cmd := []string{
		"sh",
		"-c",
		command,
	}

	req := c.set.CoreV1().RESTClient().Post().Resource("pods").Namespace(namespace).Name(name).SubResource("exec")
	return c.execInPodWithOutput(cmd, container, req)
}

func (c *Client) execInPod(cmd []string, container string, req *rest.Request) error {
	option := &core.PodExecOptions{
		Command:   cmd,
		Stdin:     false,
		Stdout:    true,
		Stderr:    true,
		TTY:       false,
		Container: container,
	}
	req.VersionedParams(
		option,
		scheme.ParameterCodec,
	)

	exec, err := remotecommand.NewSPDYExecutor(c.restConfig, "POST", req.URL())
	if err != nil {
		return err
	}

	var stdout, stderr bytes.Buffer
	err = exec.Stream(remotecommand.StreamOptions{
		Stdin:  nil,
		Stdout: &stdout,
		Stderr: &stderr,
		Tty:    true,
	})
	if err != nil {
		return err
	}

	errStr := stderr.Bytes()
	if len(errStr) > 0 {
		return errors.New(string(errStr))
	}

	return nil
}

func (c *Client) execInPodWithOutput(cmd []string, container string, req *rest.Request) (string, error) {
	option := &core.PodExecOptions{
		Command:   cmd,
		Stdin:     false,
		Stdout:    true,
		Stderr:    true,
		TTY:       false,
		Container: container,
	}
	req.VersionedParams(
		option,
		scheme.ParameterCodec,
	)

	exec, err := remotecommand.NewSPDYExecutor(c.restConfig, "POST", req.URL())
	if err != nil {
		return "", err
	}

	var stdout, stderr bytes.Buffer
	err = exec.Stream(remotecommand.StreamOptions{
		Stdin:  nil,
		Stdout: &stdout,
		Stderr: &stderr,
		Tty:    true,
	})
	if err != nil {
		return "", err
	}

	errStr := stderr.Bytes()
	if len(errStr) > 0 {
		return "", errors.New(string(errStr))
	}

	outData := stdout.Bytes()
	return string(outData), nil
}

func (c *Client) ApplyPlainYAML(monitor mntr.Monitor, data []byte) error {
	utilyaml.NewYAMLReader(bufio.NewReader(bytes.NewReader(data)))

	return forEachObjectInYAML(monitor, data, c.ApplyCRDResource)
}

// ForEachObjectInYAMLActionFunc is a function that is executed against each
// object found in a YAML document.
// When a non-empty namespace is provided then the object is assigned the
// namespace prior to any other actions being performed with or to the object.
// this is heavily ispired by https://github.com/kubernetes/client-go/issues/216#issuecomment-718813670
type forEachObjectInYAMLActionFunc func(*unstructured.Unstructured) error

// ForEachObjectInYAML excutes actionFn for each object in the provided YAML.
// If an error is returned then no further objects are processed.
// The data may be a single YAML document or multidoc YAML.
// When a non-empty namespace is provided then all objects are assigned the
// the namespace prior to any other actions being performed with or to the
// object.
// this is heavily ispired by https://github.com/kubernetes/client-go/issues/216#issuecomment-718813670
func forEachObjectInYAML(
	monitor mntr.Monitor,
	data []byte,
	actionFn forEachObjectInYAMLActionFunc) error {

	chanObj, chanErr := decodeYAML(data)
	for {
		select {
		case obj := <-chanObj:
			if obj == nil {
				return nil
			}

			if err := actionFn(obj); err != nil {
				return err
			}

			monitor.WithFields(map[string]interface{}{
				"kind": obj.GetKind(),
				"name": obj.GetName(),
			}).Info("Resource successfully applied")
		case err := <-chanErr:
			if err == nil {
				return nil
			}
			return fmt.Errorf("received error while decoding yaml: %w", err)
		}
	}
}

// DecodeYAML unmarshals a YAML document or multidoc YAML as unstructured
// objects, placing each decoded object into a channel.
// this is heavily ispired by https://github.com/kubernetes/client-go/issues/216#issuecomment-718813670
func decodeYAML(data []byte) (<-chan *unstructured.Unstructured, <-chan error) {

	var (
		chanErr        = make(chan error)
		chanObj        = make(chan *unstructured.Unstructured)
		multidocReader = utilyaml.NewYAMLReader(bufio.NewReader(bytes.NewReader(data)))
	)

	go func() {
		defer close(chanErr)
		defer close(chanObj)

		// Iterate over the data until Read returns io.EOF. Every successful
		// read returns a complete YAML document.
		for {
			buf, err := multidocReader.Read()
			if err != nil {
				if err == io.EOF {
					return
				}
				chanErr <- fmt.Errorf("failed to read yaml data: %w", err)
				return
			}

			// Define the unstructured object into which the YAML document will be
			// unmarshaled.
			obj := &unstructured.Unstructured{
				Object: map[string]interface{}{},
			}

			// Unmarshal the YAML document into the unstructured object.
			if err := k8syaml.Unmarshal(buf, &obj.Object); err != nil {
				chanErr <- fmt.Errorf("failed to unmarshal yaml data: %w", err)
				return
			}

			// filter non-kind resources
			if kind, ok := obj.Object["kind"]; !ok || kind == "" {
				continue
			}

			// Place the unstructured object into the channel.
			chanObj <- obj
		}
	}()

	return chanObj, chanErr
}
