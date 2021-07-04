# BOOM: the base tooling operator

## What is it

`BOOM` is designed to ensure that someone can create a reproducable "platform" with tools which are tested for their interoperability.

Currently we include the following tools:

- Ambassador Edge Stack
- Prometheus Operator
- Grafana
- logging-operator
- kube-state-metrics
- prometheus-node-exporter
- prometheus-systemd-exporter
- loki
- ArgoCD

Upcoming tools:

- Flux

## How to use it

There has to be a git-repository with an boom.yml in the base. Then a `BOOM` instance can be started with

```bash
orbctl -f $HOME/.orb/config takeoff
```

If you want to run boom together with orbiter you can follow the documentation in the [README.md](../../README.md). If you want to run boom on a existing cluster

## Boom on an existing Cluster

If you just want to use `BOOM` without `ORBITER` to bootstrap some tools you can follow these steps:

1. Install orbctl locally

```
curl -s https://api.github.com/repos/caos/orbos/releases/latest | grep "browser_download_url.*orbctl-$(uname)-$(uname -m)" | cut -d '"' -f 4 | sudo wget -i - -O /usr/local/bin/orbctl
sudo chmod +x /usr/local/bin/orbctl
sudo chown $(id -u):$(id -g) /usr/local/bin/orbctl
```

2. Create a git Repository with a [boom.yml](../../examples/boom/boom.yml)
3. Optional: [configure your boom.yml](yml/v1beta2/Toolset.md)
4. [Create SSH Key](https://docs.github.com/en/github/authenticating-to-github/generating-a-new-ssh-key-and-adding-it-to-the-ssh-agent#generating-a-new-ssh-key)
5. Add SSH Key to your repository (usually called deployment or access key depending on your git provider)
6. Create an orb config (usually in ~/.orb/config)

```yaml
url: git@[your-git-provider]:[path-to-your-repository].git
repokey: |
  -----BEGIN OPENSSH PRIVATE KEY-----
  insert the generated private key
  -----END OPENSSH PRIVATE KEY-----
masterkey: [create key with `openssl rand -base64 21`]
```

7. Commit / Push everything
8. Make sure your `~/.kube/config` contains a kubeconfig with access to your cluster (https://kubernetes.io/docs/concepts/configuration/organize-cluster-access-kubeconfig/)
9. Start boom with `orbctl -f ~/.orb/config takeoff --kubeconfig ~/.kube/config`
10. Boom should now be provisioning the applications you configured in your `boom.yml`

## How does it work

The operator works by reading a configuration (crd) located in a GIT Repository.
In our default setup our "cluster lifecycle" tool `ORBITER`, shares the repository and secrets with `BOOM`.

```yaml
apiVersion: boom.caos.ch/v1beta1
kind: Toolset
metadata:
  name: caos
  namespace: caos-system
spec:
  preApply:
    deploy: true
    folder: preapply
  postApply:
    deploy: true
    folder: postapply
  prometheus-operator:
    deploy: true
  logging-operator:
    deploy: true
  prometheus-node-exporter:
    deploy: true
  grafana:
    deploy: true
  ambassador:
    deploy: true
    service:
      type: LoadBalancer
  kube-state-metrics:
    deploy: true
  prometheus:
    deploy: true
    storage:
      size: 5Gi
      storageClass: standard
  loki:
    deploy: true
    storage:
      size: 5Gi
      storageClass: standard
```

## Structure of the used boom.yml

Jump to the [latest](yml/latest/Toolset.md) configuration options or to the older API versions [v1beta2](yml/v1beta2/Toolset.md) or [v1beta1](yml/v1beta1/Toolset.md)
