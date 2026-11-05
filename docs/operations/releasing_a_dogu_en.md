# Releasing a dogu with `cldk release`

`cldk release` publishes a packed chart of a dogu v3 and optionally its images to a registry.
The images are copied unchanged with the same digest, the chart gets the new image references.

This guide shows two examples with the Nexus dogu, both with `registry.cloudogu.com` and the namespace `testing`:

- [Example 1](#example-1-chart-without-images): release the chart only
- [Example 2](#example-2-chart-with-image): release the chart together with the Nexus image

All options are described in the [README](../../README.md#release).

## How it works

1. `cldk` reads the name and version from the `Chart.yaml` of the packed chart.
2. Each image given with `-i` is resolved in its source registry and copied to `<reg>/<namespace>/dogu/v3/images/<name>:<tag>`.
3. The chart gets the new image references via its `mirror-patch-tpl.yaml` and is repacked.
4. The chart is pushed to `oci://<reg>/<namespace>/dogu/v3/charts/<name>:<version>`, after all images.

`cldk` checks all targets before the first push.
Released artifacts are never overwritten silently, see [Existing artifacts](#existing-artifacts).

## Prerequisites

### Packed chart

`cldk release` takes the chart as `.tgz`, as created by `helm package`:

```shell
helm package k8s/helm -d target
# Successfully packaged chart and saved it to: target/nexus-1.2.3.tgz
```

The `version` in `Chart.yaml` is the released chart version, e.g. `1.2.3`.
It is independent of the version of the application and the tags of its images, e.g. `3.96.4` for Nexus.

### Images in a registry

`cldk release` only reads images from registries, never from the local docker daemon.
So push a locally built image to a registry first, e.g. to the registry of the CI:

```shell
docker build -t ci-registry.example.com/nexus:3.96.4 .
docker push ci-registry.example.com/nexus:3.96.4
```

Each image is given with its registry, i.e. `docker.io/library/postgres:14.18` instead of `postgres:14.18`.
This way, nothing is pulled from docker.io by accident.

To release images, the chart needs a `mirror-patch-tpl.yaml`.
For Nexus, it has one entry per image in `values.images`:

```yaml
values:
  images:
    nexus: registry.cloudogu.com/official/nexus:3.96.4
    nexusSaManager: registry.cloudogu.com/k8s/service-account-producer-sidecar:0.1.2
    postgresql: docker.io/library/postgres:14.18
```

Only the entries of the images given with `-i` change; all others stay as they are.

### Credentials

For the target registry, `cldk` uses the stored credentials of `helm registry login` or `docker login`:

```shell
docker login registry.cloudogu.com
```

Alternatively, `--user` and `--pass` or `CLDK_USER` and `CLDK_PASSWORD` apply, e.g. in the CI or in the `.env` of the repository.
The source images are always read with the credentials of `docker login`.
`cldk` never stores credentials.

The credentials need write access to the target namespace, `testing` in the examples.

## Example 1: Chart without images

The chart is released as it is; its image references keep pointing to the entries of `values.yaml`.

```shell
cldk release --dry-run --namespace testing target/nexus-1.2.3.tgz
```

```
Dry run, nothing has been pushed.
Would release chart oci://registry.cloudogu.com/testing/dogu/v3/charts/nexus:1.2.3
```

```shell
cldk release --namespace testing target/nexus-1.2.3.tgz
```

```
Released chart oci://registry.cloudogu.com/testing/dogu/v3/charts/nexus:1.2.3@sha256:…
```

`--namespace` is required, so that nothing ends up in `official` by accident.
`--reg` is not needed, `registry.cloudogu.com` is the default.

## Example 2: Chart with image

The Nexus image of the CI registry is released together with the chart:

```shell
cldk release --dry-run --namespace testing \
  -i ci-registry.example.com/nexus:3.96.4 \
  target/nexus-1.2.3.tgz
```

```
Dry run, nothing has been pushed.
Would push image ci-registry.example.com/nexus:3.96.4 -> registry.cloudogu.com/testing/dogu/v3/images/nexus:3.96.4@sha256:…
Would release chart oci://registry.cloudogu.com/testing/dogu/v3/charts/nexus:1.2.3
```

```shell
cldk release --namespace testing \
  -i ci-registry.example.com/nexus:3.96.4 \
  target/nexus-1.2.3.tgz
```

Result:

| What                       | Reference                                                        |
|----------------------------|------------------------------------------------------------------|
| Image                      | `registry.cloudogu.com/testing/dogu/v3/images/nexus:3.96.4`      |
| Chart                      | `oci://registry.cloudogu.com/testing/dogu/v3/charts/nexus:1.2.3` |
| `values.images.nexus`      | `registry.cloudogu.com/testing/dogu/v3/images/nexus:3.96.4`      |
| `values.images.postgresql` | unchanged `docker.io/library/postgres:14.18`                     |

The image keeps the name and tag of its source: the name is the last path segment, here `nexus`, the tag stays `3.96.4`.
It goes to the entry of `values.images` with the same repository name, here `nexus`.
The chart is released under its own version `1.2.3`.

### Setting name, tag and entry explicitly

If the name or tag of the source does not fit, set them explicitly with `=<name>:<tag>`:

```shell
cldk release --namespace testing \
  -i ci-registry.example.com/dogus/nexus-build:42=nexus:3.96.4 \
  target/nexus-1.2.3.tgz
```

If no or more than one entry of `values.images` fits the name, assign the image with `--image-key <name>=<key>`:

```shell
cldk release --namespace testing \
  -i ci-registry.example.com/sa-sidecar:0.1.2=service-account-producer-sidecar:0.1.2 \
  --image-key service-account-producer-sidecar=nexusSaManager \
  target/nexus-1.2.3.tgz
```

## Existing artifacts

`cldk` checks all targets before the first push:

| Target already exists          | Behavior                                          |
|--------------------------------|---------------------------------------------------|
| Image tag with the same digest | is skipped (`Skipped image … (already released)`) |
| Image tag with another digest  | abort, nothing is pushed                          |
| Chart version                  | abort, nothing is pushed                          |

So an aborted release can simply be run again: images copied before are skipped.

With `--force`, image tags and the chart version are overwritten on purpose, e.g. to correct a release in `testing`:

```shell
cldk release --force --namespace testing -i ci-registry.example.com/nexus:3.96.4 target/nexus-1.2.3.tgz
```

## Checking the result

```shell
# chart and its image references
helm show chart oci://registry.cloudogu.com/testing/dogu/v3/charts/nexus --version 1.2.3
helm show values oci://registry.cloudogu.com/testing/dogu/v3/charts/nexus --version 1.2.3 | grep -A3 'image:'
# digest of the image, equal to the digest of the source
docker buildx imagetools inspect registry.cloudogu.com/testing/dogu/v3/images/nexus:3.96.4
```

## Troubleshooting

| Symptom                                                              | Cause and solution                                                                                                               |
|----------------------------------------------------------------------|----------------------------------------------------------------------------------------------------------------------------------|
| The release aborts because of an existing image tag or chart version | The version has already been released. Build a new chart version or overwrite it on purpose in `testing` with `--force`.         |
| An image without registry is rejected                                | Images are always given with their registry, e.g. `docker.io/library/postgres:14.18`.                                            |
| A locally built image is not found                                   | `cldk` does not read from the docker daemon. Push the image to a registry first.                                                 |
| The chart is rejected with images                                    | The chart has no `mirror-patch-tpl.yaml`.                                                                                        |
| An image fits no or several entries of `values.images`               | Assign the entry with `--image-key <name>=<key>`.                                                                                |
| The target registry rejects the login                                | Run `docker login registry.cloudogu.com` or set `CLDK_USER`/`CLDK_PASSWORD`; the credentials need write access to the namespace. |
