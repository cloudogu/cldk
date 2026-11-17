# `cldk dogu release` reference

`cldk dogu release` publishes a packed chart of a dogu v3 and optionally its images to a registry.
For a step-by-step guide with examples with `registry.cloudogu.com`, see [Releasing a dogu with `cldk dogu release`](releasing_a_dogu_en.md).

## Chart and target

Pass a packed Dogu Helm chart and specify the required target namespace with `--dogu-namespace <ns>`:

```shell
cldk dogu release --dogu-namespace <ns> <chart.tgz>
```

By default, the chart is published to `registry.cloudogu.com`.
Its name and version are read from `Chart.yaml`, resulting in this target:

```text
oci://<reg>/<ns>/dogu/v3/charts/<name>:<version>
```

## `--image`, `-i`: Include container images

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

## `--image-key`: Map images to chart values

If no entry matches, or several entries match, assign the image explicitly with `--image-key <name>=<key>`.
Here, `<name>` is the target image name and `<key>` is the entry under `values.images` in `mirror-patch-tpl.yaml`:

```shell
cldk dogu release --dogu-namespace <ns> \
  -i docker.io/library/postgres:14.18 \
  --image-key postgres=postgresql \
  <chart.tgz>
```

Repeat `--image-key` to map additional images.

## `--dry-run`: Preview a release

Add `--dry-run` to check the release without pushing anything:

```shell
cldk dogu release --dry-run --dogu-namespace <ns> \
  -i docker.io/library/postgres:14.18 \
  <chart.tgz>
```

A dry run resolves the images, rewrites the chart locally, and checks all targets as a release would.
It prints each image's source, target, digest, and planned action (push, skip, overwrite, or conflict), as well as the chart target.
Mapping errors and conflicts cause the dry run to fail.

## `--force`: Overwrite existing releases

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

## `--reg`: Set the target registry

Use `--reg <registry>` to publish to a registry other than the default `registry.cloudogu.com`:

```shell
cldk dogu release --dogu-namespace <ns> --reg registry.example.com <chart.tgz>
```

## `--plain-http` and `--insecure`: Configure registry transport

| Flag           | Target registry connection                                                 |
|----------------|----------------------------------------------------------------------------|
| `--plain-http` | HTTP without TLS.                                                          |
| `--insecure`   | TLS without certificate verification, e.g. with a self-signed certificate. |

## `--user` and `--pass`: Authenticate to the target registry

Supply both flags together to set the target registry credentials:

```shell
cldk dogu release --dogu-namespace <ns> --user <user> --pass <password> <chart.tgz>
```

Target registry credentials are selected in this order:

1. `--user` and `--pass`, supplied together.
2. `CLDK_USER` and `CLDK_PASSWORD`, set together when neither credential flag is supplied; they may be set in the [`.env` of the project directory](project_configuration_en.md#env).
3. Stored credentials from `helm registry login` or `docker login`.

Credentials are never persisted by cldk.
