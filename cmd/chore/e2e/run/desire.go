package main

import (
	"bytes"
	"context"
	"fmt"
	"time"

	"github.com/caos/orbos/internal/helpers"

	"github.com/caos/orbos/internal/operator/common"

	"gopkg.in/yaml.v3"
)

var _ testFunc = desireORBITERState

func desireORBITERState(specs *testSpecs, settings programSettings, conditions *conditions) interactFunc {

	clusterSpec := fmt.Sprintf(`      controlplane:
        updatesdisabled: false
        provider: %s
        nodes: %d
        pool: management
        taints:
        - key: node-role.kubernetes.io/master
          effect: NoSchedule
      kubeconfig: null
      networking:
        dnsdomain: cluster.orbitertest
        network: calico
        podcidr: 100.126.4.0/22
        servicecidr: 100.127.224.0/20
      verbose: false
      versions:
        kubernetes: v1.18.8
        orbiter: %s
      workers:
      - updatesdisabled: false
        provider: %s
        nodes: %d
        pool: application`,
		settings.orbID,
		specs.DesireORBITERState.InitialMasters,
		settings.artifactsVersion(),
		settings.orbID,
		specs.DesireORBITERState.InitialWorkers,
	)

	if err := yaml.Unmarshal([]byte(clusterSpec), conditions.kubernetes); err != nil {
		panic(err)
	}

	return func(ctx context.Context, _ uint8, newOrbctl newOrbctlCommandFunc) error {

		initCtx, initCancel := context.WithTimeout(ctx, 1*time.Minute)
		defer initCancel()

		var providerYml string
		if err := runCommand(settings, nil, nil, func(line string) {
			providerYml += fmt.Sprintf("    %s\n", line)
		}, newOrbctl(initCtx), "--gitops", "file", "print", "provider.yml"); err != nil {
			return err
		}

		conditions.testCase = nil

		return desireState(initCtx, settings, newOrbctl, "orbiter.yml", fmt.Sprintf(`kind: orbiter.caos.ch/Orb
version: v0
spec:
  verbose: false
clusters:
  %s:
    kind: orbiter.caos.ch/KubernetesCluster
    version: v0
    spec:
%s
providers:
  %s:
%s
    loadbalancing:
      kind: orbiter.caos.ch/DynamicLoadBalancer
      version: v2
      spec:
        management:
        - ip: 192.168.122.2
          transport:
          - name: kubeapi
            frontendport: 6443
            backendport: 6666
            backendpools:
            - management
            whitelist:
            - 0.0.0.0/0
            healthchecks:
              protocol: https
              path: /healthz
              code: 200
        application:
        - ip: 192.168.122.3
          transport:
          - name: httpsingress
            frontendport: 443
            backendport: 30443
            backendpools:
            - application
            whitelist:
            - 0.0.0.0/0
            healthchecks:
              protocol: https
              path: /ambassador/v0/check_ready
              code: 200
            proxyprotocol: true
          - name: httpingress
            frontendport: 80
            backendport: 30080
            backendpools:
            - application
            whitelist:
            - 0.0.0.0/0
            healthchecks:
              protocol: http
              path: /ambassador/v0/check_ready
              code: 200
            proxyprotocol: true
`, settings.orbID, clusterSpec, settings.orbID, providerYml))
	}
}

func desireBOOMState(deploy bool) testFunc {
	return func(specs *testSpecs, settings programSettings, conditions *conditions) interactFunc {

		// needs to be assigned also when test is skipped
		conditions.boom = &condition{
			watcher: watch(10*time.Minute, boom),
			checks: func(ctx context.Context, newKubectl newKubectlCommandFunc, orbiter currentOrbiter, current common.NodeAgentsCurrentKind) error {

				checkCtx, checkCancel := context.WithTimeout(ctx, 1*time.Minute)
				defer checkCancel()

				var expectTotal uint8
				checkPodsAreReadyFunc := func(selector string, expectPods uint8) func() error {
					expectTotal += expectPods
					return func() error {
						return checkPodsAreReady(checkCtx, settings, newKubectl, "caos-system", selector, expectPods)
					}
				}

				if !deploy {
					// only orbiter and boom should be running in caos-system
					return checkPodsAreReadyFunc("", 2)()
				}

				workers := conditions.desiredWorkers()
				allNodes := conditions.desiredMasters() + workers

				return helpers.Fanout([]func() error{
					checkPodsAreReadyFunc("app.kubernetes.io/instance=ambassador", 2),
					checkPodsAreReadyFunc("app.kubernetes.io/instance=argocd", 4),
					checkPodsAreReadyFunc("app.kubernetes.io/instance=grafana", 1),
					checkPodsAreReadyFunc("app.kubernetes.io/instance=kube-state-metrics", 1),
					checkPodsAreReadyFunc("app.kubernetes.io/name=fluentbit", workers),
					checkPodsAreReadyFunc("app.kubernetes.io/name=fluentd", 1),
					checkPodsAreReadyFunc("app.kubernetes.io/instance=logging-operator", 1),
					checkPodsAreReadyFunc("app=loki", 1),
					checkPodsAreReadyFunc("app=prometheus-node-exporter", allNodes),
					checkPodsAreReadyFunc("app=prometheus", 1),
					checkPodsAreReadyFunc("app=prometheus-operator-operator", 1),
					checkPodsAreReadyFunc("app=systemd-exporter", allNodes),
					checkPodsAreReadyFunc("app.kubernetes.io/name notin (orbiter, boom)", expectTotal),
					func() error {
						provider, ok := orbiter.Providers[settings.orbID]
						if !ok {
							return fmt.Errorf("provider %s not found in current state", settings.orbID)
						}

						ep := provider.Current.Ingresses.Httpsingress

						msg, err := helpers.Check("https", ep.Location, ep.FrontendPort, "/ambassador/v0/check_ready", 200, false)
						if err != nil {
							return fmt.Errorf("ambassador ready check failed with message: %s: %w", msg, err)
						}

						settings.logger.Infof(msg)

						return nil
					},
				})()
			},
		}

		return func(ctx context.Context, step uint8, orbctl newOrbctlCommandFunc) (err error) {

			storage := `
    storage:
      accessModes:
        - ReadWriteOnce
      size: 1G`

			if specs.DesireBOOMState.Stateless {
				storage = ""
			}

			return desireState(ctx, settings, orbctl, "boom.yml", fmt.Sprintf(`
apiVersion: caos.ch/v1
kind: Boom
metadata:
  name: caos
  namespace: caos-system
spec:
  boomVersion: %s
  preApply:
    folder: preapply
    deploy: true
  postApply:
    folder: postapply
    deploy: true
  metricCollection:
    resources:
      requests: {}
    deploy: %t
  logCollection:
    deploy: %t
    fluentbit:
      resources:
        requests: {}
    fluentd:
      resources:
        requests: {}
  nodeMetricsExporter:
    resources:
      requests: {}
    deploy: %t
  systemdMetricsExporter:
    resources:
      requests: {}
    deploy: %t
  monitoring:
    resources:
      requests: {}
    deploy: %t
  apiGateway:
    deploy: %t
    replicaCount: 1
    service:
      type: NodePort
  kubeMetricsExporter:
    deploy: %t
    resources:
      requests: {}
  reconciling:
    deploy: %t
    resources:
      requests: {}
  metricsPersisting:
    deploy: %t
    resources:
      requests: {}%s
  logsPersisting:
    deploy: %t
    resources:
      requests: {}
  metricsServer:
    deploy: false
`,
				settings.artifactsVersion(),
				deploy,
				deploy,
				deploy,
				deploy,
				deploy,
				deploy,
				deploy,
				deploy,
				deploy,
				storage,
				deploy,
			))
		}
	}
}

func desireState(ctx context.Context, settings programSettings, newOrbctl newOrbctlCommandFunc, path, yml string) error {

	desireCtx, desireCancel := context.WithTimeout(ctx, 1*time.Minute)
	defer desireCancel()

	cmd := newOrbctl(desireCtx)
	cmd.Stdin = bytes.NewReader([]byte(yml))

	return runCommand(settings, orbctl.strPtr(), nil, nil, cmd, "--gitops", "file", "patch", path, "--exact", "--stdin")
}
