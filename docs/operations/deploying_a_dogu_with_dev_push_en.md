# Deploying a dogu to a cluster with `cldk dogu dev-push`

`cldk dogu dev-push` brings the working state of a dogu v3 into a cluster without releasing the dogu.
It builds the images of the `cldk.yaml`, pushes them and the chart under a new dev version to a registry, and creates or updates the dogu resource.
The dogu operator then installs the dogu from this chart instead of the dogu registry.

This guide shows two examples with the Nexus dogu:

- [Example 1](#example-1-cluster-with-registrycloudogucom-and-the-namespace-testing): a regular cluster that gets its dogus from `registry.cloudogu.com`
- [Example 2](#example-2-local-k3d-cluster-with-the-k3d-registry): a local k3d cluster with a registry of k3d

All options are described in the [README](../../README.md#dogu-dev-push).

## How it works

1. `cldk` loads the `cldk.yaml` and the chart, checks the cluster and lints the rewritten chart.
2. Each image of the `cldk.yaml` is built and pushed to `<reg>/<namespace>/dogu/v3/images/<name>:<dev-version>`.
3. The chart gets the new image references and is pushed to `oci://<reg>/<namespace>/dogu/v3/charts/<name>:<dev-version>`.
4. The dogu resource gets `spec.version: <dev-version>` and the annotation `k8s.cloudogu.com/dev-chart-url` with the URL of the chart.
5. In dev mode, the dogu operator creates an `OCIRepository` for this URL, and Flux installs the chart.

The dev version is `<version of Chart.yaml>-dev.<UTC yyyymmddhhmmss>`, e.g. `1.2.3-dev.20261105131320`.
Each push creates a new version, so that the cluster picks up the new state right away.

## Prerequisites

### Dogu repository

- The chart of the dogu is part of the repository, by default in `k8s/helm`.
  The `version` in `Chart.yaml` must be a valid semantic version, e.g. `1.2.3`.
- To build images, the chart needs a `mirror-patch-tpl.yaml`, which `cldk` uses to rewrite the image references.
- The root directory contains a `cldk.yaml`, for Nexus e.g.:

  ```yaml
  namespace: official   # dogu namespace in the registry, overridden by the .env in example 1
  images:
    nexus: {}           # builds ./Dockerfile, replaces values.images.nexus of mirror-patch-tpl.yaml
  ```

- The settings of your environment belong in the `.env` of the repository.
  It is meant for local settings and is therefore usually not committed; the examples below each show a matching `.env`.

### Dogu operator in dev mode

The dogu operator only uses dev charts if its dev mode is enabled.
In a CES cluster, enable it in the component of the operator:

```shell
kubectl -n ecosystem edit component k8s-dogu-operator
```

```yaml
spec:
  valuesYamlOverwrite: |
    controllerManager:
      env:
        doguV3Enabled: true
        doguV3DevChartsEnabled: true
```

Keep the existing entries of `valuesYamlOverwrite` and add `doguV3DevChartsEnabled: true`.

> [!WARNING]
> **Caution:** The dev mode is meant for development and test clusters only.
> With dev mode enabled, anyone who may change dogu resources can bring arbitrary charts into the cluster.

### Choosing the target cluster

Without further settings, `dev-push` uses the current context of the kubeconfig.
If the kubeconfig also contains production clusters, the dogu resource can easily end up in the wrong cluster.
So put the cluster into the `.env`, either with a kubeconfig of its own or with a context:

```shell
KUBECONFIG=/home/me/.kube/test-cluster
# or
CLDK_DEV_KUBE_CONTEXT=test-cluster
```

`dev-push` logs the context, server and namespace before building; `--dry-run` shows them together with the dogu resource without changing anything.

## Example 1: Cluster with `registry.cloudogu.com` and the namespace `testing`

The cluster gets its dogus from `registry.cloudogu.com`.
The dev state is pushed to the namespace `testing`, so that it is kept apart from the releases in `official`.

### Logging in

The builder pushes the images, `cldk` pushes the chart; both need credentials with write access to `testing`:

```shell
docker login registry.cloudogu.com
```

`cldk` reads the stored credentials of `docker login` or `helm registry login`.

### `.env`

```shell
KUBECONFIG=/home/me/.kube/test-cluster
CLDK_DEV_KUBE_NAMESPACE=ecosystem
CLDK_DEV_REG=registry.cloudogu.com
CLDK_DEV_NAMESPACE=testing
```

`CLDK_DEV_CLUSTER_REG` stays empty: the cluster reaches the registry under the same address.
`CLDK_DEV_PLAIN_HTTP` and `CLDK_DEV_INSECURE` stay empty, the registry has a valid certificate.

### Pushing

```shell
cldk dogu dev-push --dry-run
cldk dogu dev-push
```

Result:

| What                                        | Reference                                                                              |
|---------------------------------------------|----------------------------------------------------------------------------------------|
| Image                                       | `registry.cloudogu.com/testing/dogu/v3/images/nexus:1.2.3-dev.20261105131320`          |
| Chart                                       | `oci://registry.cloudogu.com/testing/dogu/v3/charts/nexus:1.2.3-dev.20261105131320`    |
| Annotation `k8s.cloudogu.com/dev-chart-url` | `oci://registry.cloudogu.com/testing/dogu/v3/charts/nexus`                             |
| `spec.doguNamespace`                        | `testing`                                                                              |

The dogu resource as applied by `cldk`:

```yaml
apiVersion: k8s.cloudogu.com/v3beta1
kind: Dogu
metadata:
  name: nexus
  annotations:
    k8s.cloudogu.com/dev-chart-url: oci://registry.cloudogu.com/testing/dogu/v3/charts/nexus
spec:
  name: nexus
  doguNamespace: testing
  doguApiVersion: v3
  version: 1.2.3-dev.20261105131320
```

### What the cluster needs

- The cluster pulls the chart and the images with the credentials of the secret `ces-container-registries`.
  These credentials must be allowed to read the namespace `testing`.
- Charts that refer to `ces-container-registries` in `global.imagePullSecrets` for their images (like Nexus) need no further settings.

## Example 2: Local k3d cluster with the k3d registry

A local k3d cluster with a registry created by k3d.
The registry has different addresses from your machine and from the cluster and speaks plain HTTP only.

### Addresses of the registry

|                          | Address                                       | Variable               |
|--------------------------|-----------------------------------------------|------------------------|
| from your machine (push) | `localhost:<host port>`                       | `CLDK_DEV_REG`         |
| from the cluster (pull)  | `k3d-<registry name>:<port in the container>` | `CLDK_DEV_CLUSTER_REG` |

`k3d registry list` and `docker ps` show the names and ports of the registries.

The k3d setup of `k8s-ecosystem` (`ces-k3d`) has two registries sharing their storage:
`k3d-registry-dev.localhost` accepts pushes on `localhost:5001`, and the cluster reads via `k3d-registry-proxy.localhost:5000`.

### `.env`

For the k3d setup of `k8s-ecosystem` with the instance `dev1`:

```shell
KUBECONFIG=/home/me/.kube/dev1.k3ces.localdomain
CLDK_DEV_KUBE_NAMESPACE=ecosystem
CLDK_DEV_REG=localhost:5001
CLDK_DEV_CLUSTER_REG=k3d-registry-proxy.localhost:5000
CLDK_DEV_PLAIN_HTTP=true
```

Docker treats `localhost` as an insecure registry anyway; the k3d registries need no login.

### Pushing

```shell
cldk dogu dev-push --dry-run
cldk dogu dev-push
```

Result:

| What                                             | Reference                                                                                     |
|--------------------------------------------------|-----------------------------------------------------------------------------------------------|
| Image (push)                                     | `localhost:5001/official/dogu/v3/images/nexus:1.2.3-dev.20261105131320`                       |
| Image in the chart                               | `k3d-registry-proxy.localhost:5000/official/dogu/v3/images/nexus:1.2.3-dev.20261105131320`    |
| Chart (push)                                     | `oci://localhost:5001/official/dogu/v3/charts/nexus:1.2.3-dev.20261105131320`                 |
| Annotation `k8s.cloudogu.com/dev-chart-url`      | `oci://k3d-registry-proxy.localhost:5000/official/dogu/v3/charts/nexus`                       |
| Annotation `k8s.cloudogu.com/dev-chart-insecure` | `"true"`                                                                                      |

Because of `CLDK_DEV_PLAIN_HTTP=true`, `cldk` sets the annotation `k8s.cloudogu.com/dev-chart-insecure`, and the operator pulls the chart via HTTP.

## Checking the result

`dev-push` does not wait for the dogu to run and prints a matching command to watch it at the end:

```shell
kubectl -n ecosystem get dogu nexus -w
```

Further checks:

```shell
# events: DevChart, ChartAvailable, HelmRelease created/updated
kubectl -n ecosystem describe dogu nexus
# URL, tag and insecure of the chart
kubectl -n ecosystem get ocirepository nexus -o yaml
# image of the new pod
kubectl -n ecosystem get pods -l app.kubernetes.io/instance=nexus \
  -o jsonpath='{range .items[*]}{.metadata.name}: {.spec.containers[0].image}{"\n"}{end}'
```

`values`, `mappedValues` and `stopped` of the dogu resource are kept on every push, `cldk` only sets its own fields.

## Building single images only

Use `--replace-image` and `--skip-image` to leave out images for one push, e.g. if only the chart has changed:

```shell
# build nothing, the image reference of values.yaml stays
cldk dogu dev-push --skip-image nexus
# build nothing, write an existing image into the chart
cldk dogu dev-push --replace-image nexus=registry.cloudogu.com/official/nexus:3.96.4
```

## Returning to the regular dogu

To use a release of the dogu registry again, remove the dev annotations and set a regular version:

```shell
kubectl -n ecosystem annotate dogu nexus k8s.cloudogu.com/dev-chart-url- k8s.cloudogu.com/dev-chart-insecure-
kubectl -n ecosystem patch dogu nexus --type merge -p '{"spec":{"version":"1.2.2","doguNamespace":"official"}}'
```

The operator only reacts to changes of the spec.
Only the new `spec.version` makes it pull the chart from the dogu registry again; the annotations alone are not enough.
`doguNamespace` only needs to be reset if the dev state was in another namespace, as in example 1.

## Troubleshooting

| Symptom                                                                  | Cause and solution                                                                                                                                                                         |
|--------------------------------------------------------------------------|--------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `dev-push` shows the wrong context                                       | The cluster comes from the current context of the kubeconfig. Put `KUBECONFIG` or `CLDK_DEV_KUBE_CONTEXT` into the `.env`, see [Choosing the target cluster](#choosing-the-target-cluster). |
| Warning event `DevChartsDisabled`, the dogu ends in `DownloadFailed`     | The dev mode of the operator is disabled, so the operator looks for the dev version in the dogu registry. Enable the dev mode, see [Dogu operator in dev mode](#dogu-operator-in-dev-mode). |
| `dev-push` aborts because a classic dogu with the same name is installed | `dev-push` does not trigger a migration from v2 to v3. Remove or migrate the classic dogu first.                                                                                           |
| The pod cannot pull the image                                            | The cluster address of the registry is wrong (`CLDK_DEV_CLUSTER_REG`), or the credentials in `ces-container-registries` do not cover the namespace.                                        |
