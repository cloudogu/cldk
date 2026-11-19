# `cldk dogu dev-push` reference

`cldk dogu dev-push` pushes the working state of a dogu repository under a new dev version to a dev registry and installs it in a cluster.
For a step-by-step guide with the prerequisites in the cluster, examples with `registry.cloudogu.com` and a local k3d cluster, and how to return to the regular dogu, see [Deploying a dogu with `cldk dogu dev-push`](deploying_a_dogu_with_dev_push_en.md).
The project directory needs a `cldk.yaml`, see [Project configuration](project_configuration_en.md#cldkyaml).

## Dev version and chart

- Each run creates a new dev version `<version>-dev.<UTC yyyymmddhhmmss>` from the `version` of `Chart.yaml`, e.g. `1.2.3-dev.20261103143012`.
  - The version in `Chart.yaml` must be a valid semantic version without build metadata (`+…`).
  - The chart is pushed to `oci://<reg>/<namespace>/dogu/v3/charts/<name>:<dev-version>`, the same layout as `release`.
- The chart is loaded from its directory like `helm package` does, including `.helmignore`.
  - If `Chart.yaml` declares dependencies, they must be in `charts/`; run `helm dependency build <chart dir>` first.
  - The chart is checked with `helm lint` (including `values.schema.json`) before anything is built or pushed; errors abort, warnings are logged.
  - With `--values`, the chart is linted with these values, like `helm lint --values`.

## Options

The dev registry, its access and the cluster are given as flags or environment variables, flags first:

| Flag                           | Environment variable      | Description                                                                            |
|--------------------------------|---------------------------|----------------------------------------------------------------------------------------|
| `--reg <host[:port]>`          | `CLDK_DEV_REG`            | address of the dev registry to push to, required                                       |
| `--cluster-reg <host[:port]>`  | `CLDK_DEV_CLUSTER_REG`    | address of the dev registry as seen from the cluster, default `--reg`                  |
| `--dogu-namespace <ns>`        | `CLDK_DEV_DOGU_NAMESPACE` | dogu namespace, default the `doguNamespace` of `cldk.yaml`                             |
| `--plain-http`                 | `CLDK_DEV_PLAIN_HTTP`     | dev registry without TLS                                                               |
| `--insecure`                   | `CLDK_DEV_INSECURE`       | dev registry with an untrusted (e.g. self-signed) certificate                          |
| `--kube-context <name>`        | `CLDK_DEV_KUBE_CONTEXT`   | kubeconfig context of the cluster, default the current context                         |
| `--kube-namespace <ns>`        | `CLDK_DEV_KUBE_NAMESPACE` | Kubernetes namespace of the dogu, default the namespace of the context, else `default` |
| `--builder <bin>`              | `CLDK_BUILDER`            | Docker compatible build CLI, default `docker`; shared with `release`                   |
| `--platform <os/arch>`         | `CLDK_PLATFORM`           | platform the images are built for, passed to the builder; shared with `release`        |
| `--no-cache`                   | `CLDK_NO_CACHE`           | build the images without the cache of the builder, e.g. to enforce a clean build       |
| `--values <file>`              | `CLDK_DEV_VALUES`         | YAML file with Helm values for the dogu, relative to the project directory             |
| `--build-arg KEY[=VALUE]`      |                           | build argument for all images, repeatable                                              |
| `--replace-image <name>=<ref>` |                           | do not build the image `<name>`, write `<ref>` into the chart instead, repeatable      |
| `--skip-image <name>`          |                           | neither build the image `<name>` nor write it into the chart, repeatable               |
| `--dry-run`                    |                           | run all checks and print the plan, but build, push and apply nothing                   |

- Boolean variables take the values of Go's `strconv.ParseBool`, e.g. `true`, `false`, `1`, `0`; an empty variable counts as not set.
- The dev registry is accessed with the stored credentials of `docker login` or `helm registry login`.
- With `-v`, `dev-push` logs the resolved value of each option together with its source, i.e. flag, environment variable or default.
- Put the settings of your environment into the [`.env`](project_configuration_en.md#env) of the dogu repository, e.g. for a k3d cluster with a local registry:

  ```shell
  CLDK_DEV_REG=localhost:5001
  CLDK_DEV_CLUSTER_REG=k3d-registry-proxy.localhost:5000
  CLDK_DEV_PLAIN_HTTP=true
  ```

## Building images

- `dev-push` builds the images of `cldk.yaml` one after another, ordered by name, and then pushes them in the same order:
  - `<builder> build -f <dockerfile> -t <reg>/<namespace>/dogu/v3/images/<name>:<dev-version> [--target …] [--build-arg …] [--platform …] [--no-cache] <context>`
  - `<builder> push <reg>/<namespace>/dogu/v3/images/<name>:<dev-version>`
- The builder is any CLI with Docker compatible `build` and `push` commands, e.g. `docker` or `podman`.
  - It must be on the `PATH` if at least one image is built.
  - It inherits the environment of `cldk`, its output goes to stderr; the built images stay in its local store.
  - The builder must trust the dev registry and be logged in to it if needed, e.g. as insecure registry for a registry without TLS; `--plain-http` and `--insecure` only apply to the chart push of `cldk`.
- `--platform` takes exactly one platform; without it, the builder chooses.
- `--build-arg KEY=VALUE` applies to all images and overrides `buildArgs` of the same key; `--build-arg KEY` takes the value from the environment like `docker build`.
- A copy of the chart gets the references of the built images on the cluster address of the dev registry, e.g. `k3d-registry-proxy.localhost:5000/official/dogu/v3/images/nexus:<dev-version>`.
  - `dev-push` renders `mirror-patch-tpl.yaml` with the new references and merges the patches into the chart.
  - The chart is rewritten and linted before the first build.
- The first failed build or push aborts with the image name and the exit code of the builder; a failed build pushes nothing, images pushed before a failed push stay in the dev registry.
- Only the images of `cldk.yaml` are built; the other images of the chart stay unchanged without any flag.
- Replace or skip single images of `cldk.yaml` for one run, e.g. to work on one image only:

  ```shell
  # with the images nexus, sidecar and postgres in cldk.yaml:
  #   build only nexus, take an existing sidecar image and keep the postgres image of the chart
  cldk dogu dev-push --replace-image sidecar=registry.example.com/foo/sidecar:1.0 --skip-image postgres
  ```

  - `--replace-image <name>=<ref>` neither builds nor pushes the image; `<ref>` is written into the chart unchanged, to the same entry of `values.images` as the built image.
  - `--skip-image <name>` neither builds the image nor changes its reference in the chart, as if it were not in `cldk.yaml`.
  - Each name must be an image of `cldk.yaml` and may be given only once in both flags.
  - If all images are replaced or skipped, nothing is built and the builder is not needed; if all are skipped, the chart is not rewritten.

## Installing in the cluster

- After pushing the chart, `dev-push` applies the dogu resource (`k8s.cloudogu.com/v3beta1`, kind `Dogu`) in the cluster per server-side apply with the field manager `cldk`:
  - `spec.version` is the dev version, `spec.doguApiVersion` is `v3`, `spec.name` and `metadata.name` are the `name` of `Chart.yaml`, `spec.doguNamespace` is the dogu namespace.
  - The annotation `k8s.cloudogu.com/dev-chart-url` holds the URL of the chart without tag from the cluster address of the dev registry, e.g. `oci://k3d-registry-proxy.localhost:5000/official/dogu/v3/charts/nexus`.
  - The annotation `k8s.cloudogu.com/dev-chart-insecure: "true"` is set with `--plain-http` or `--insecure` and removed without them.
  - With `--values`, `spec.values` gets the values of the file; an empty file applies no values.
  - All other fields of an existing dogu, e.g. `mappedValues`, `stopped` or values set by others, are kept.
- Values are owned per key:
  - A value of the file overrides the same value set before, e.g. with `kubectl`.
  - A value that `cldk` applied before and that is no longer in the file, or a push without `--values`, removes it from the dogu.
  - Lists are replaced as a whole.
  - Put `CLDK_DEV_VALUES` into the `.env`, so that every push applies the same values.
- `KUBECONFIG` may be set in the `.env`.
  - Select another context or namespace with `--kube-context` and `--kube-namespace`.
- Before linting and pushing, `dev-push` logs the context, server and namespace of the cluster.
- `dev-push` does not wait for the dogu to be ready.
- If applying fails after the push, the pushed images and chart stay in the dev registry.

## Dry run

- Use `--dry-run` to check a dev push without building, pushing or applying anything:

  ```shell
  cldk dogu dev-push --dry-run
  ```

  - It runs all checks of a dev push: `cldk.yaml`, the chart and its version, the builder (if an image is built), the connection to the cluster, and the rewrite and lint of the chart.
  - It prints the dev version, each image with its push reference or replaced reference or as skipped, the chart reference, the target cluster and the dogu resource as YAML, as it would be applied.
  - A failed check fails the dry run, too.
  - `--dry-run` has no environment variable, so that a `.env` never turns a dev push into a dry run.
