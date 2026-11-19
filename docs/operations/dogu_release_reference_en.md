# `cldk dogu release` reference

`cldk dogu release` publishes a dogu, its chart and its images, to a namespace of a registry.
For a step-by-step guide with examples with `registry.cloudogu.com`, see [Releasing a dogu with `cldk dogu release`](releasing_a_dogu_en.md).

`dogu release` has two modes:

| Mode            | Invocation                      | Images                                                                     |
|-----------------|---------------------------------|----------------------------------------------------------------------------|
| Project release | `cldk dogu release`             | built from the project directory with their release tags, or copied (`-i`) |
| Archive release | `cldk dogu release <chart.tgz>` | copied from other registries (`-i`)                                        |

## Project release

Without argument, `dogu release` releases the dogu in the project directory (`--dir`, default the working directory):

```shell
cldk dogu release
```

- It reads `cldk.yaml` (see [Project configuration](project_configuration_en.md#cldkyaml)) and the chart directory.
- Each image of `cldk.yaml` is built with the builder and pushed to `<reg>/<namespace>/dogu/v3/images/<name>:<release tag>`.
  - The release tag is `tag:` of the image in `cldk.yaml`, overridden with `--tag <name>=<tag>` (repeatable).
  - Without a release tag for an image, the release aborts before the first build and names all images without one.
- The chart is rewritten to the released images via `mirror-patch-tpl.yaml`, using `key` of `cldk.yaml`, linted and pushed with the unchanged `version` of `Chart.yaml`.
- The namespace defaults to `doguNamespace` of `cldk.yaml`.
- Order: check everything, build all images, push all built images, copy the `-i` images, push the chart.
  - A failed build leaves nothing in the target registry.
  - Built images are pushed by the builder with its own login, e.g. `docker login registry.cloudogu.com`; `--user`/`--pass` apply to everything else.
- An existing release tag or chart version is a conflict before the first build; `--force` overwrites both.
  Built images are never skipped, because their digest is unknown before the build.
- `-i <image>` copies an image instead of building it if its target name is an image of `cldk.yaml`; it keeps the `key` of `cldk.yaml` unless `--image-key` assigns another.
  `-i` images without counterpart are released additionally; with only `-i` images, no builder is needed.
- `--builder`, `--platform`, `--no-cache` and `--build-arg` work like in [`dogu dev-push`](dogu_dev_push_reference_en.md#building-images).
- `--dry-run` checks `cldk.yaml`, the release tags, the builder, the lint, the `-i` sources and all targets and prints the plan, but builds and pushes nothing:

  ```text
  Dry run, nothing has been pushed.
  Would build image nexus -> registry.cloudogu.com/official/dogu/v3/images/nexus:3.96.4-1
  Would release chart oci://registry.cloudogu.com/official/dogu/v3/charts/nexus:1.2.3
  ```

## Archive release

Pass a packed Dogu Helm chart and specify the required target namespace with `--dogu-namespace <ns>`:

```shell
cldk dogu release --dogu-namespace <ns> <chart.tgz>
```

`--tag`, `--builder`, `--platform`, `--no-cache` and `--build-arg` are rejected with a chart archive; their environment variables are ignored.
By default, the chart is published to `registry.cloudogu.com`.
Its name and version are read from `Chart.yaml`, resulting in this target:

```text
oci://<reg>/<ns>/dogu/v3/charts/<name>:<version>
```

### `--image`, `-i`: Include container images

Use `--image <image>` (short form: `-i`) to include an image.
Repeat the flag for each image to release with the chart:

```shell
cldk dogu release --dogu-namespace <ns> \
  -i registry.cloudogu.com/official/nexus:3.96.4 \
  -i docker.io/library/postgres:14.18 \
  <chart.tgz>
```

Each image is copied with the same digest to:

```text
<reg>/<ns>/dogu/v3/images/<name>:<tag>
```

The target name defaults to the last path segment of the source repository; the source tag is kept.
For example, `docker.io/library/postgres:14.18` becomes `<reg>/<ns>/dogu/v3/images/postgres:14.18`.
Use `-i <image>=<name>:<tag>` to override the defaults:

```shell
cldk dogu release --dogu-namespace <ns> \
  -i localhost:5001/local-dev/nexus/main:3.96.4=nexus:3.96.4 \
  <chart.tgz>
```

Image sources must meet these requirements:

- Include the registry in every reference, e.g. `docker.io/library/postgres:14.18` instead of `postgres:14.18`, to avoid accidental pulls from Docker Hub.
- Push locally built images to a registry first.
  Images are read from registries, never from the local Docker daemon.

Source registries are accessed with the stored credentials of `docker login`.

When including images, the chart must contain a `mirror-patch-tpl.yaml` file.
For each image, cldk updates the entry under `values.images` whose repository name matches, applies the file's patches with the new image reference, and repacks the chart.
Other image entries stay unchanged.
All images are pushed before the chart.
If a push fails, cldk prints the images released before the failure together with the error.
Running the release again skips these images, because their tags already exist with the same digest.

### `--image-key`: Map images to chart values

If no entry matches, or several entries match, assign the image explicitly with `--image-key <name>=<key>`.
Here, `<name>` is the target image name and `<key>` is the entry under `values.images` in `mirror-patch-tpl.yaml`:

```shell
cldk dogu release --dogu-namespace <ns> \
  -i docker.io/library/postgres:14.18 \
  --image-key postgres=postgresql \
  <chart.tgz>
```

Repeat `--image-key` to map additional images.

### `--dry-run`: Preview a release

Add `--dry-run` to check the release without pushing anything:

```shell
cldk dogu release --dry-run --dogu-namespace <ns> \
  -i docker.io/library/postgres:14.18 \
  <chart.tgz>
```

A dry run resolves the images, rewrites the chart locally, and checks all targets as a release would.
It prints each image's source, target, digest, and planned action (push, skip, overwrite, or conflict), as well as the chart target.
Mapping errors and conflicts cause the dry run to fail.

### `--force`: Overwrite existing releases

Use `--force` to overwrite conflicting image tags and already released chart versions, for example when testing releases:

```shell
cldk dogu release --dogu-namespace <ns> --force <chart.tgz>
```

All targets are checked before the first push.
Existing artifacts are handled as follows:

| Existing target                   | Default behavior                                  |
|-----------------------------------|---------------------------------------------------|
| Image tag with the same digest    | Skip the image; a failed release can be retried.  |
| Image tag with a different digest | Abort the release.                                |
| Chart with the same version       | Abort the release.                                |

### `--reg`: Set the target registry

Use `--reg <registry>` to publish to a registry other than the default `registry.cloudogu.com`:

```shell
cldk dogu release --dogu-namespace <ns> --reg registry.example.com <chart.tgz>
```

### `--plain-http` and `--insecure`: Configure registry transport

| Flag           | Target registry connection                                                 |
|----------------|----------------------------------------------------------------------------|
| `--plain-http` | HTTP without TLS.                                                          |
| `--insecure`   | TLS without certificate verification, e.g. with a self-signed certificate. |

### `--user` and `--pass`: Authenticate to the target registry

Supply both flags together to set the target registry credentials:

```shell
cldk dogu release --dogu-namespace <ns> --user <user> --pass <password> <chart.tgz>
```

Target registry credentials are selected in this order:

1. `--user` and `--pass`, supplied together.
2. `CLDK_USER` and `CLDK_PASSWORD`, set together when neither credential flag is supplied; they may be set in the [`.env` of the project directory](project_configuration_en.md#env).
3. Stored credentials from `helm registry login` or `docker login`.

Credentials are never persisted by cldk.

## Options

The settings of the target registry and of the builds can also be given as environment variables, including in the [`.env` of the project directory](project_configuration_en.md#env); flags take precedence:

| Flag                                 | Environment variable        | Description                                                                                                               |
|--------------------------------------|-----------------------------|---------------------------------------------------------------------------------------------------------------------------|
| `--reg <host[:port]>`                | `CLDK_REG`                  | target registry, default `registry.cloudogu.com`                                                                          |
| `--dogu-namespace <ns>`              | `CLDK_DOGU_NAMESPACE`       | dogu namespace in the target registry; project release: default `doguNamespace` of `cldk.yaml`, archive release: required |
| `--plain-http`                       | `CLDK_PLAIN_HTTP`           | target registry without TLS                                                                                               |
| `--insecure`                         | `CLDK_INSECURE`             | target registry with an untrusted (e.g. self-signed) certificate                                                          |
| `--user`/`--pass`                    | `CLDK_USER`/`CLDK_PASSWORD` | credentials of the target registry, only as a pair from one source                                                        |
| `--builder <bin>`                    | `CLDK_BUILDER`              | project release: Docker compatible build CLI, default `docker`                                                            |
| `--platform <os/arch>`               | `CLDK_PLATFORM`             | project release: platform the images are built for                                                                        |
| `--no-cache`                         | `CLDK_NO_CACHE`             | project release: build without the cache of the builder                                                                   |
| `--build-arg KEY[=VALUE]`            |                             | project release: build argument for all images, overrides `buildArgs` of `cldk.yaml`, repeatable                          |
| `--tag <name>=<tag>`                 |                             | project release: release tag of an image of `cldk.yaml`, overrides its `tag:`, repeatable                                 |
| `-i`, `--image <ref>[=<name>:<tag>]` |                             | copy an image instead of building it or in addition, repeatable                                                           |
| `--image-key <name>=<key>`           |                             | assign an image of `-i` to `values.images.<key>` in `mirror-patch-tpl.yaml`, repeatable                                   |
| `--force`                            |                             | overwrite existing release tags, image tags with another digest and chart versions                                        |
| `--dry-run`                          |                             | check everything and print the plan, but build and push nothing                                                           |

`cldk -v` logs each resolved value with its source, the password masked; check it and the references of `--dry-run` before a release, so that a forgotten line in the `.env` does not redirect it.
