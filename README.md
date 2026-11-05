# cldk
Cloudogu (LOP) Development Kit helps developers in releasing and developing their Dogu helm-charts.

## Usage

```shell
cldk release --namespace <ns> <chart.tgz>   # release a packed dogu chart to registry.cloudogu.com
cldk release --namespace <ns> -i <image> ... <chart.tgz>   # release the chart together with its images
cldk release --dry-run --namespace <ns> -i <image> ... <chart.tgz>   # print the plan without pushing anything
cldk dev-push       # push the dogu v3 under development in the working directory and install it in the current cluster
cldk version        # print the version and commit of cldk
cldk -v <command>   # enable debug logging of cldk, debug output of libraries like Helm is left out
cldk -d <dir> <command>   # use <dir> as project directory instead of the working directory
cldk --help
```

### `release`

See [Releasing a dogu with `cldk release`](docs/operations/releasing_a_dogu_en.md) ([Deutsch](docs/operations/releasing_a_dogu_de.md)) for examples with `registry.cloudogu.com`.

- `cldk release` pushes the chart to `oci://<reg>/<ns>/dogu/v3/charts/<name>:<version>`, name and version are read from `Chart.yaml`. 
- Each image given with `-i` is copied unchanged (same digest) from its registry to `<reg>/<ns>/dogu/v3/images/<name>:<tag>`, named after the last path segment of the source and keeping its tag.
  - Use `-i <image>=<name>:<tag>` to set name and tag explicitly, e.g. `-i localhost:5001/local-dev/nexus/main:3.96.4-3=nexus:3.96.4-3`.
  - The images are read with the stored credentials of `docker login`.
  - Each image must be given with its registry, e.g. `docker.io/library/postgres:14.18` instead of `postgres:14.18`, so that nothing is pulled from docker.io by accident.
  - Images are only read from registries, never from the local docker daemon.
    Push a locally built image to a registry first.
  - With images, the chart needs a `mirror-patch-tpl.yaml`: each image replaces the entry of `values.images` with the same repository name, the `patches` are applied with the new references and the chart is repacked.
  - Use `--image-key <name>=<key>` (repeatable) to assign an image to the entry `values.images.<key>` explicitly, e.g. if no or several entries have its repository name.
  - The entries of all other images in `values.images` stay unchanged.
  - All images are pushed before the chart.
- Released artifacts are never overwritten silently. All targets are checked before the first push:
  - An image tag that already exists with the same digest is skipped, so a failed release can simply be run again.
  - An image tag with another digest or an already released chart version aborts the release.
  - Use `--force` to overwrite both, e.g. for testing releases.
- Use `--dry-run` to check a release without pushing anything.
  - It resolves the images, rewrites the chart locally and checks all targets like a release.
  - It prints each image with its source, target, digest and whether it would be pushed, skipped, overwritten or conflicts, and the chart target.
  - Mapping errors and conflicts fail the dry run, too.
- Use `--reg` for another target registry, `--plain-http` for a target registry without TLS and `--insecure` for a target registry with an untrusted (e.g. self-signed) certificate.
- The target registry is accessed with `--user`/`--pass` (or `CLDK_USER`/`CLDK_PASSWORD`) if given, else with the stored credentials of `helm registry login` or `docker login`.
- Credentials are never persisted.

### `dev-push`

`cldk dev-push` pushes the working state of a dogu v3 repository under a new dev version to a dev registry and installs it in a cluster.
See [Deploying a dogu with `cldk dev-push`](docs/operations/deploying_a_dogu_with_dev_push_en.md) ([Deutsch](docs/operations/deploying_a_dogu_with_dev_push_de.md)) for the prerequisites in the cluster, examples with `registry.cloudogu.com` and a local k3d cluster, and how to return to the regular dogu.

- The project directory (see below) needs a `cldk.yaml`:

  ```yaml
  namespace: official        # dogu namespace in the registry, required
  chart: k8s/helm            # chart directory, default k8s/helm
  images:                    # images to build, optional; without entries nothing is built
    nexus:                   # image name, pushed to <reg>/<namespace>/dogu/v3/images/<name>
      context: .             # build context, default .
      dockerfile: Dockerfile # Dockerfile, default <context>/Dockerfile
      key: nexus             # key in values.images of mirror-patch-tpl.yaml, default the entry with the repository name <name>
      target: ""             # stage of a multi-stage build, optional
      buildArgs:             # static build arguments, optional
        FOO: bar
  ```

  All paths are relative to the project directory; `nexus: {}` is a valid image entry with all defaults.

  Unknown fields in `cldk.yaml` abort the command, so that typos are not silently ignored.
- Each run creates a new dev version `<version>-dev.<UTC yyyymmddhhmmss>` from the `version` of `Chart.yaml`, e.g. `3.96.4-3-dev.20261103143012`.
  - The version in `Chart.yaml` must be a valid semantic version without build metadata (`+…`).
  - The chart is pushed to `oci://<reg>/<namespace>/dogu/v3/charts/<name>:<dev version>`, the same layout as `release`.
- The chart is loaded from its directory like `helm package` does, including `.helmignore`.
  - If `Chart.yaml` declares dependencies, they must be in `charts/`; run `helm dependency build <chart dir>` first.
  - The chart is checked with `helm lint` (including `values.schema.json`) before anything is built or pushed; errors abort, warnings are logged.
- The dev registry, its access and the cluster are given as flags or environment variables, flags first:

| Flag                           | Environment variable      | Description                                                                            |
|--------------------------------|---------------------------|----------------------------------------------------------------------------------------|
| `--reg <host[:port]>`          | `CLDK_DEV_REG`            | address of the dev registry to push to, required                                       |
| `--cluster-reg <host[:port]>`  | `CLDK_DEV_CLUSTER_REG`    | address of the dev registry as seen from the cluster, default `--reg`                  |
| `--namespace <ns>`             | `CLDK_DEV_NAMESPACE`      | dogu namespace, default the `namespace` of `cldk.yaml`                                 |
| `--plain-http`                 | `CLDK_DEV_PLAIN_HTTP`     | dev registry without TLS                                                               |
| `--insecure`                   | `CLDK_DEV_INSECURE`       | dev registry with an untrusted (e.g. self-signed) certificate                          |
| `--kube-context <name>`        | `CLDK_DEV_KUBE_CONTEXT`   | kubeconfig context of the cluster, default the current context                         |
| `--kube-namespace <ns>`        | `CLDK_DEV_KUBE_NAMESPACE` | Kubernetes namespace of the dogu, default the namespace of the context, else `default` |
| `--builder <bin>`              | `CLDK_DEV_BUILDER`        | Docker compatible build CLI, default `docker`                                          |
| `--platform <os/arch>`         | `CLDK_DEV_PLATFORM`       | platform the images are built for, passed to the builder                               |
| `--build-arg KEY[=VALUE]`      |                           | build argument for all images, repeatable                                              |
| `--replace-image <name>=<ref>` |                           | do not build the image `<name>`, write `<ref>` into the chart instead, repeatable      |
| `--skip-image <name>`          |                           | neither build the image `<name>` nor write it into the chart, repeatable               |
| `--dry-run`                    |                           | run all checks and print the plan, but build, push and apply nothing                   |

  - Boolean variables take the values of Go's `strconv.ParseBool`, e.g. `true`, `false`, `1`, `0`; an empty variable counts as not set.
  - The dev registry is accessed with the stored credentials of `docker login` or `helm registry login`.
- Put the settings of your environment into the `.env` of the dogu repository, e.g. for a k3d cluster with a local registry:

  ```shell
  CLDK_DEV_REG=localhost:5001
  CLDK_DEV_CLUSTER_REG=k3d-registry-proxy.localhost:5000
  CLDK_DEV_PLAIN_HTTP=true
  ```

#### Building images

- `dev-push` builds the images of `cldk.yaml` one after another, ordered by name, and pushes each right after its build:
  - `<builder> build -f <dockerfile> -t <reg>/<namespace>/dogu/v3/images/<name>:<dev version> [--target …] [--build-arg …] [--platform …] <context>`
  - `<builder> push <reg>/<namespace>/dogu/v3/images/<name>:<dev version>`
- The builder is any CLI with Docker compatible `build` and `push` commands, e.g. `docker` or `podman`.
  - It must be on the `PATH` if at least one image is built.
  - It inherits the environment of `cldk`, its output goes to stderr; the built images stay in its local store.
  - The builder must trust the dev registry and be logged in to it if needed, e.g. as insecure registry for a registry without TLS; `--plain-http` and `--insecure` only apply to the chart push of `cldk`.
- `--platform` takes exactly one platform; without it, the builder chooses.
- `--build-arg KEY=VALUE` applies to all images and overrides `buildArgs` of the same key; `--build-arg KEY` takes the value from the environment like `docker build`.
- A copy of the chart gets the references of the built images on the cluster address of the dev registry, e.g. `k3d-registry-proxy.localhost:5000/official/dogu/v3/images/nexus:<dev version>`.
  - `dev-push` renders `mirror-patch-tpl.yaml` with the new references and merges the patches into the chart.
  - The chart is rewritten and linted before the first build.
- The first failed build or push aborts with the image name and the exit code of the builder; images pushed before stay in the dev registry.
- Replace or skip single images of `cldk.yaml` for one run, e.g. to work on one image only:

  ```shell
  # build nexus, take an existing sidecar image and leave the postgres image of the chart as it is
  cldk dev-push --replace-image sidecar=registry.example.com/foo/sidecar:1.0 --skip-image postgres
  ```

  - `--replace-image <name>=<ref>` neither builds nor pushes the image; `<ref>` is written into the chart unchanged, to the same entry of `values.images` as the built image.
  - `--skip-image <name>` neither builds the image nor changes its reference in the chart, as if it were not in `cldk.yaml`.
  - Each name must be an image of `cldk.yaml` and may be given only once in both flags.
  - If all images are replaced or skipped, nothing is built and the builder is not needed; if all are skipped, the chart is not rewritten.

#### Installing in the cluster

- After pushing the chart, `dev-push` applies the dogu resource (`k8s.cloudogu.com/v3beta1`, kind `Dogu`) in the cluster per server-side apply with the field manager `cldk`:
  - `spec.version` is the dev version, `spec.doguApiVersion` is `v3`, `spec.name` and `metadata.name` are the `name` of `Chart.yaml`, `spec.doguNamespace` is the dogu namespace.
  - The annotation `k8s.cloudogu.com/dev-chart-url` holds the URL of the chart without tag from the cluster address of the dev registry, e.g. `oci://k3d-registry-proxy.localhost:5000/official/dogu/v3/charts/nexus`.
  - The annotation `k8s.cloudogu.com/dev-chart-insecure: "true"` is set with `--plain-http` or `--insecure` and removed without them.
  - All other fields of an existing dogu, e.g. `values`, `mappedValues` or `stopped`, are kept.
- `KUBECONFIG` may be set in the `.env`.
  - Select another context or namespace with `--kube-context` and `--kube-namespace`.
- Before linting and pushing, `dev-push` logs the context, server and namespace of the cluster.
- `dev-push` does not wait for the dogu to be ready.
- If applying fails after the push, the pushed images and chart stay in the dev registry.

#### Dry run

- Use `--dry-run` to check a dev push without building, pushing or applying anything:

  ```shell
  cldk dev-push --dry-run
  ```

  - It runs all checks of a dev push: `cldk.yaml`, the chart and its version, the builder (if an image is built), the connection to the cluster, and the rewrite and lint of the chart.
  - It prints the dev version, each image with its push reference or replaced reference or as skipped, the chart reference, the target cluster and the dogu resource as YAML, as it would be applied.
  - A failed check fails the dry run, too.
  - `--dry-run` has no environment variable, so that a `.env` never turns a dev push into a dry run.

### Project directory and `.env`

- Every command first loads the `.env` of the project directory into its environment, e.g. `CLDK_USER`/`CLDK_PASSWORD` for `release`.
- The project directory is the working directory or the one given with `--dir`/`-d`.
- Variables that are already set in the environment, even if empty, take precedence over the `.env`; flags take precedence over both.
- A missing `.env` is fine, a `.env` that cannot be parsed aborts the command.
- `cldk -v` logs the names of the variables taken from the `.env`, never their values; only `dev-push` logs the resolved values of its own settings.

## Building

Requirements: GNU make and Docker (the local build runs inside a `golang` container), Go 1.27 for running tests.

```shell
make                  # builds the static linux/amd64 binary target/cldk
make unit-test        # runs the unit tests, reports are written to target/unit-tests
make integration-test # runs the tests tagged with "integration"
make static-analysis  # runs golangci-lint
make mocks            # generates mocks with mockery (configured in .mockery.yaml)
make clean            # removes all build output
```

The build is based on the [Cloudogu makefiles](https://github.com/cloudogu/makefiles).
To update them, set `MAKEFILES_VERSION` in the `Makefile` and run `make update-makefiles`.

## What is the Cloudogu EcoSystem?
The Cloudogu EcoSystem is an open platform, which lets you choose how and where your team creates great software. Each service or tool is delivered as a Dogu, a Docker container. Each Dogu can easily be integrated in your environment just by pulling it from our registry.

We have a growing number of ready-to-use Dogus, e.g. SCM-Manager, Jenkins, Nexus Repository, SonarQube, Redmine and many more. Every Dogu can be tailored to your specific needs. Take advantage of a central authentication service, a dynamic navigation, that lets you easily switch between the web UIs and a smart configuration magic, which automatically detects and responds to dependencies between Dogus.

The Cloudogu EcoSystem is open source and it runs either on-premises or in the cloud. The Cloudogu EcoSystem is developed by Cloudogu GmbH under [AGPL-3.0-only](https://spdx.org/licenses/AGPL-3.0-only.html).

## License
Copyright © 2020 - present Cloudogu GmbH
This program is free software: you can redistribute it and/or modify it under the terms of the GNU Affero General Public License as published by the Free Software Foundation, version 3.
This program is distributed in the hope that it will be useful, but WITHOUT ANY WARRANTY; without even the implied warranty of MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the GNU Affero General Public License for more details.
You should have received a copy of the GNU Affero General Public License along with this program. If not, see https://www.gnu.org/licenses/.
See [LICENSE](LICENSE) for details.


---
MADE WITH :heart:&nbsp;FOR DEV ADDICTS. [Legal notice / Imprint](https://cloudogu.com/en/imprint/?mtm_campaign=ecosystem&mtm_kwd=imprint&mtm_source=github&mtm_medium=link)
