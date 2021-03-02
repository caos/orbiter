package kubernetes

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/api/meta"

	extensions "k8s.io/api/extensions/v1beta1"

	"github.com/caos/orbos/pkg/labels"

	"github.com/caos/orbos/internal/operator/orbiter/kinds/clusters/kubernetes/drainreason"

	"github.com/caos/orbos/internal/helpers"
	"github.com/caos/orbos/mntr"
	"github.com/pkg/errors"
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

type NotAvailableError struct{}

func (n *NotAvailableError) Error() string {
	return "Kubernetes is not available"
}

type ClientInt interface {
	ApplyService(rsc *core.Service) error
	DeleteService(namespace, name string) error

	GetJob(namespace, name string) (*batch.Job, error)
	ApplyJob(rsc *batch.Job) error
	DeleteJob(namespace string, name string) error
	WaitUntilJobCompleted(namespace string, name string, timeoutSeconds time.Duration) error

	ApplyServiceAccount(rsc *core.ServiceAccount) error
	DeleteServiceAccount(namespace, name string) error

	ApplyStatefulSet(rsc *apps.StatefulSet, force bool) error
	DeleteStatefulset(namespace, name string) error
	WaitUntilStatefulsetIsReady(namespace string, name string, containerCheck, readyCheck bool, timeoutSeconds time.Duration) error

	ExecInPodWithOutput(namespace, name, container, command string) (string, error)
	ExecInPod(namespace, name, container, command string) error

	ApplyDeployment(rsc *apps.Deployment, force bool) error
	DeleteDeployment(namespace, name string) error
	PatchDeployment(namespace, name string, data string) error
	WaitUntilDeploymentReady(namespace string, name string, containerCheck, readyCheck bool, timeoutSeconds time.Duration) error
	ScaleDeployment(namespace, name string, replicaCount int) error
	ExecInPodOfDeployment(namespace, name, container, command string) error

	CheckCRD(name string) (*apixv1beta1.CustomResourceDefinition, bool, error)
	GetNamespacedCRDResource(group, version, kind, namespace, name string) (*unstructured.Unstructured, error)
	ApplyNamespacedCRDResource(group, version, kind, namespace, name string, crd *unstructured.Unstructured) error
	DeleteNamespacedCRDResource(group, version, kind, namespace, name string) error

	ApplyCronJob(rsc *v1beta1.CronJob) error
	DeleteCronJob(namespace string, name string) error

	ListSecrets(namespace string, labels map[string]string) (*core.SecretList, error)
	ApplySecret(rsc *core.Secret) error
	DeleteSecret(namespace, name string) error
	WaitForSecret(namespace string, name string, timeoutSeconds time.Duration) error

	GetConfigMap(namespace, name string) (*core.ConfigMap, error)
	ApplyConfigmap(rsc *core.ConfigMap) error
	DeleteConfigmap(namespace, name string) error
	WaitForConfigMap(namespace string, name string, timeoutSeconds time.Duration) error

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
}

var _ ClientInt = (*Client)(nil)

type Client struct {
	monitor           mntr.Monitor
	set               *kubernetes.Clientset
	dynamic           dynamic.Interface
	apixv1beta1client *apixv1beta1client.ApiextensionsV1beta1Client
	mapper            *restmapper.DeferredDiscoveryRESTMapper
	restConfig        *rest.Config
}

func NewK8sClientWithPath(monitor mntr.Monitor, kubeconfigPath string) (*Client, error) {
	kubeconfigStr := ""
	if kubeconfigPath != "" {
		value, err := ioutil.ReadFile(kubeconfigPath)
		if err != nil {
			monitor.Error(err)
			return nil, err
		}
		kubeconfigStr = string(value)
	}

	return NewK8sClient(monitor, &kubeconfigStr), nil
}

func NewK8sClient(monitor mntr.Monitor, kubeconfig *string) *Client {
	kc := &Client{monitor: monitor}
	err := kc.Refresh(kubeconfig)
	if err != nil {
		// do nothing
	}
	return kc
}

func (c *Client) Available() bool {
	return c.set != nil
}

func (c *Client) nodeApi() (clgocore.NodeInterface, error) {
	if c.set == nil {
		return nil, &NotAvailableError{}
	}
	return c.set.CoreV1().Nodes(), nil
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
	return notFoundIsSuccess(c.set.CoreV1().Namespaces().Delete(context.Background(), name, mach.DeleteOptions{}))
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
	return notFoundIsSuccess(c.set.AppsV1().Deployments(namespace).Delete(context.Background(), name, mach.DeleteOptions{}))
}
func (c *Client) PatchDeployment(namespace, name string, data string) error {
	_, err := c.set.AppsV1().Deployments(namespace).Patch(context.Background(), name, types.StrategicMergePatchType, []byte(data), mach.PatchOptions{})
	return err
}

func (c *Client) WaitUntilDeploymentReady(namespace string, name string, containerCheck, readyCheck bool, timeoutSeconds time.Duration) error {
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
	case <-time.After(timeoutSeconds * time.Second):
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
	return notFoundIsSuccess(c.set.CoreV1().Services(namespace).Delete(context.Background(), name, mach.DeleteOptions{}))
}

func (c *Client) GetJob(namespace, name string) (*batch.Job, error) {
	return c.set.BatchV1().Jobs(namespace).Get(context.Background(), name, mach.GetOptions{})
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
	return notFoundIsSuccess(c.DeletePodsByLabels(namespace, job.Spec.Selector.MatchLabels))
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
	return notFoundIsSuccess(c.set.BatchV1beta1().CronJobs(namespace).Delete(context.Background(), name, mach.DeleteOptions{}))
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
	return notFoundIsSuccess(c.set.PolicyV1beta1().PodDisruptionBudgets(namespace).Delete(context.Background(), name, mach.DeleteOptions{}))
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
	return notFoundIsSuccess(c.set.AppsV1().StatefulSets(namespace).Delete(context.Background(), name, mach.DeleteOptions{}))
}

func (c *Client) WaitUntilStatefulsetIsReady(namespace string, name string, containerCheck, readyCheck bool, timeoutSeconds time.Duration) error {
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
	case <-time.After(timeoutSeconds * time.Second):
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

func (c *Client) WaitForSecret(namespace string, name string, timeoutSeconds time.Duration) error {
	returnChannel := make(chan error, 1)
	go func() {
		ctx := context.Background()
		for i := 0; i < int(timeoutSeconds); i++ {
			secret, err := c.set.CoreV1().Secrets(namespace).Get(ctx, name, mach.GetOptions{})
			if err != nil && !macherrs.IsNotFound(err) {
				returnChannel <- err
				return
			} else if secret != nil {
				returnChannel <- nil
				return
			}
			time.Sleep(1)
		}
	}()

	select {
	case res := <-returnChannel:
		return res
	case <-time.After(timeoutSeconds * time.Second):
		return errors.New("timeout while waiting for secret to be created")
	}
}

func (c *Client) DeleteSecret(namespace, name string) error {
	return notFoundIsSuccess(c.set.CoreV1().Secrets(namespace).Delete(context.Background(), name, mach.DeleteOptions{}))
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
	return notFoundIsSuccess(c.set.CoreV1().ConfigMaps(namespace).Delete(context.Background(), name, mach.DeleteOptions{}))
}

func (c *Client) WaitForConfigMap(namespace string, name string, timeoutSeconds time.Duration) error {
	returnChannel := make(chan error, 1)
	go func() {
		ctx := context.Background()
		for i := 0; i < int(timeoutSeconds); i++ {
			secret, err := c.set.CoreV1().ConfigMaps(namespace).Get(ctx, name, mach.GetOptions{})
			if err != nil && !macherrs.IsNotFound(err) {
				returnChannel <- err
				return
			} else if secret != nil {
				returnChannel <- nil
				return
			}
			time.Sleep(1)
		}
	}()

	select {
	case res := <-returnChannel:
		return res
	case <-time.After(timeoutSeconds * time.Second):
		return errors.New("timeout while waiting for configmap to be created")
	}
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

func (c *Client) DeleteServiceAccount(namespace, name string) error {
	return notFoundIsSuccess(c.set.CoreV1().ServiceAccounts(namespace).Delete(context.Background(), name, mach.DeleteOptions{}))
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
	return notFoundIsSuccess(c.set.ExtensionsV1beta1().Ingresses(namespace).Delete(context.Background(), name, mach.DeleteOptions{}))
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
	return notFoundIsSuccess(c.set.RbacV1().Roles(namespace).Delete(context.Background(), name, mach.DeleteOptions{}))
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
	return notFoundIsSuccess(c.set.RbacV1().ClusterRoles().Delete(context.Background(), name, mach.DeleteOptions{}))
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
	return notFoundIsSuccess(c.set.RbacV1().RoleBindings(namespace).Delete(context.Background(), name, mach.DeleteOptions{}))
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
	return notFoundIsSuccess(c.set.RbacV1().ClusterRoleBindings().Delete(context.Background(), name, mach.DeleteOptions{}))
}

type recreateErr struct{}

func (r *recreateErr) Error() string { return "recreate" }

func (c *Client) applyResource(object, name string, create func() error, update func() error) (err error) {

	defer func() {
		err = errors.Wrapf(err, "applying %s %s failed", object, name)
	}()

	if c.set == nil {
		return &NotAvailableError{}
	}

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
				return errors.Errorf("only recreating %s when force is true", controllerType)
			}

			if err := deleteResource(); err != nil {
				return err
			}
			return &recreateErr{}
		},
	)
}

func (c *Client) Refresh(kubeconfig *string) (err error) {
	if kubeconfig == nil {
		return
	}

	defer func() {
		err = errors.Wrap(err, "refreshing Kubernetes client failed")
	}()

	restCfg := new(rest.Config)
	if *kubeconfig == "" {
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

func (c *Client) RefreshConfig(config *rest.Config) (err error) {
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
	return nil
}

func (c *Client) GetNode(id string) (node *core.Node, err error) {

	api, err := c.nodeApi()
	if err != nil {
		return nil, err
	}
	return api.Get(context.Background(), id, mach.GetOptions{})
}

func (c *Client) ListNodes(filterID ...string) (nodes []core.Node, err error) {
	defer func() {
		err = errors.Wrapf(err, "listing nodes %s failed", strings.Join(filterID, ", "))
	}()

	api, err := c.nodeApi()
	if err != nil {
		return nil, err
	}

	labelSelector := ""
	for _, id := range filterID {
		labelSelector = fmt.Sprintf("%s,name=%s", labelSelector, id)
	}
	if len(labelSelector) > 0 {
		labelSelector = labelSelector[1:]
	}

	nodeList, err := api.List(context.Background(), mach.ListOptions{
		LabelSelector: labelSelector,
	})

	if err != nil {
		return nil, err
	}
	return nodeList.Items, nil
}

func (c *Client) UpdateNode(node *core.Node) (err error) {

	defer func() {
		err = errors.Wrapf(err, "updating node %s failed", node.GetName())
	}()

	api, err := c.nodeApi()
	if err != nil {
		return err
	}
	node.ResourceVersion = ""
	_, err = api.Update(context.Background(), node, mach.UpdateOptions{})
	return err
}

func (c *Client) cordon(node *core.Node, reason drainreason.DrainReason) (err error) {
	defer func() {
		err = errors.Wrapf(err, "cordoning node %s failed", node.GetName())
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

func (c *Client) Tainted(node *core.Node, reason drainreason.DrainReason) bool {
	return c.tainted(node.Spec.Taints, reason) != -1
}

func (c *Client) tainted(taints []core.Taint, reason drainreason.DrainReason) int {

	for idx, taint := range taints {
		if taint.Key == TaintKeyPrefix+reason.String() {
			return idx
		}
	}
	return -1
}

func (c *Client) RemoveFromTaints(taints []core.Taint, reason drainreason.DrainReason) (result []core.Taint) {
	idx := c.tainted(taints, reason)
	if idx < 0 {
		return taints
	}
	return append(taints[0:idx], taints[idx+1:]...)
}

func (c *Client) Drain(machine Machine, node *core.Node, reason drainreason.DrainReason) (err error) {
	defer func() {
		err = errors.Wrapf(err, "draining node %s failed", node.GetName())
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

func (c *Client) EnsureDeleted(name string, machine Machine, node NodeWithKubeadm) (err error) {

	defer func() {
		err = errors.Wrapf(err, "deleting node %s failed", name)
	}()

	monitor := c.monitor.WithFields(map[string]interface{}{
		"machine": name,
	})
	monitor.Info("Ensuring node is deleted")

	api, apiErr := c.nodeApi()
	apiErr = errors.Wrap(apiErr, "getting node api failed")

	drain := func() error {
		// Cordon node
		if apiErr == nil {
			nodeStruct, err := api.Get(context.Background(), name, mach.GetOptions{})
			if err != nil {
				if macherrs.IsNotFound(err) {
					return nil
				}
				return errors.Wrapf(err, "getting node %s from kube api failed", name)
			}

			return c.Drain(machine, nodeStruct, drainreason.Deleting)
		}
		return nil
	}

	if err := drain(); err != nil {
		return err
	}

	monitor.Info("Resetting kubeadm")
	if _, resetErr := node.Execute(nil, "sudo kubeadm reset --force"); resetErr != nil {
		if !strings.Contains(resetErr.Error(), "command not found") {
			return resetErr
		}
	}

	if apiErr != nil {
		return nil
	}
	monitor.Info("Deleting node")
	if err := notFoundIsSuccess(api.Delete(context.Background(), name, mach.DeleteOptions{})); err != nil {
		return err
	}
	if !machine.GetUpdating() || machine.GetJoined() {
		machine.SetUpdating(true)
		machine.SetJoined(false)
		monitor.Changed("Node deleted")
	}
	return nil
}

func (c *Client) evictPods(node *core.Node) (err error) {

	defer func() {
		err = errors.Wrapf(err, "evicting pods from node %s failed", node.GetName())
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
		return errors.Wrapf(err, "listing pods with selector %s failed", selector)
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
				synchronizer.Done(errors.Wrapf(goErr, "evicting pod %s failed", pod.Name))
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
					synchronizer.Done(errors.Wrapf(notFoundIsSuccess(c.set.CoreV1().Pods(pod.Namespace).Delete(context.Background(), pod.Name, mach.DeleteOptions{})), "deleting pod %s after timout exceeded failed", pod.Name))
					return
				}
			}
		}(p)
	}
	wg.Wait()

	if synchronizer.IsError() {
		return errors.Wrapf(synchronizer, "concurrently evicting pods from node %s failed", node.Name)
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
	return notFoundIsSuccess(c.set.CoreV1().Pods(namespace).DeleteCollection(context.Background(), mach.DeleteOptions{}, mach.ListOptions{
		LabelSelector: getLabelSelector(labels),
	}))
}

func (c *Client) DeletePod(namespace, name string) error {
	return notFoundIsSuccess(c.set.CoreV1().Pods(namespace).Delete(context.Background(), name, mach.DeleteOptions{}))
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

func (c *Client) CheckCRD(name string) (*apixv1beta1.CustomResourceDefinition, bool, error) {
	crds := c.apixv1beta1client.CustomResourceDefinitions()

	crd, err := crds.Get(context.Background(), name, mach.GetOptions{})
	if macherrs.IsNotFound(err) {
		return nil, false, nil
	}
	return crd, true, err
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
		return errors.Wrapf(err, "getting existing crd %s of kind %s failed", name, kind)
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
		if _, ok := err.(*meta.NoKindMatchError); ok {
			return nil
		}
		return err
	}

	return notFoundIsSuccess(c.dynamic.Resource(mapping.Resource).Namespace(namespace).Delete(context.Background(), name, mach.DeleteOptions{}))
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

func notFoundIsSuccess(err error) error {
	if macherrs.IsNotFound(err) {
		return nil
	}
	return err
}
