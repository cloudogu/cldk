# cldk
Cloudogu (LOP) Development Kit helps developers in releasing and developing their Dogu helm-charts.

## Installation

Each [GitHub release](https://github.com/cloudogu/cldk/releases) provides `cldk` for Linux and macOS on `amd64` and `arm64`.

### Download with `curl`

The archives are named `cldk_<version>_<os>_<arch>.tar.gz` with `<os>` being `linux` or `darwin` and `<arch>` being `amd64` or `arm64`:

```shell
VERSION=0.1.0 OS=linux ARCH=amd64
curl -fsSLO "https://github.com/cloudogu/cldk/releases/download/v${VERSION}/cldk_${VERSION}_${OS}_${ARCH}.tar.gz"
curl -fsSLO "https://github.com/cloudogu/cldk/releases/download/v${VERSION}/checksums.txt"
sha256sum --check --ignore-missing checksums.txt
tar -xzf "cldk_${VERSION}_${OS}_${ARCH}.tar.gz" cldk
sudo install cldk /usr/local/bin/
```

### Install with mise

[mise](https://mise.jdx.dev/) picks the matching archive of the GitHub release:

```shell
mise use github:cloudogu/cldk@0.1.0
```

In a dogu repository, pin the version in its `mise.toml`:

```toml
[tools]
"github:cloudogu/cldk" = "0.1.0"
```

### Install with `go install`

This builds `cldk` from source and requires Go:

```shell
go install github.com/cloudogu/cldk@v0.1.0
```

## Usage

### Release a chart

Pass a packed Dogu Helm chart and specify the required target namespace with `--namespace <ns>`:

```shell
cldk release --namespace <ns> <chart.tgz>
```

By default, the chart is published to `registry.cloudogu.com`.
Its name and version are read from `Chart.yaml`, resulting in this target:

```text
oci://<reg>/<ns>/dogu/v3/charts/<name>:<version>
```

### `--image`, `-i`: Include container images

Use `--image <image>` (short form: `-i`) to include an image.
Repeat the flag for each image to release with the chart:

```shell
cldk release --namespace <ns> \
  -i registry.cloudogu.com/official/nexus:3.96.4-3 \
  -i docker.io/library/postgres:14.18 \
  <chart.tgz>
```

Each image is copied with the same digest to:

```text
<reg>/<ns>/dogu/v3/images/<name>:<tag>
```

The target name defaults to the last path segment of the source repository; the source tag is kept.
For example, `docker.io/library/postgres:14.18` becomes `<reg>/<ns>/dogu/v3/images/postgres:14.18`.

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

#### `-i <image>=<name>:<tag>`: Override the target name and tag

Use `-i <image>=<name>:<tag>` to override the defaults:

```shell
cldk release --namespace <ns> \
  -i localhost:5001/local-dev/nexus/main:3.96.4-3=nexus:3.96.4-3 \
  <chart.tgz>
```

### `--image-key`: Map images to chart values

If no entry matches, or several entries match, assign the image explicitly with `--image-key <name>=<key>`.
Here, `<name>` is the target image name and `<key>` is the entry under `values.images` in `mirror-patch-tpl.yaml`:

```shell
cldk release --namespace <ns> \
  -i docker.io/library/postgres:14.18 \
  --image-key postgres=postgresql \
  <chart.tgz>
```

Repeat `--image-key` to map additional images.

### `--dry-run`: Preview a release

Add `--dry-run` to check the release without pushing anything:

```shell
cldk release --dry-run --namespace <ns> \
  -i docker.io/library/postgres:14.18 \
  <chart.tgz>
```

A dry run resolves the images, rewrites the chart locally, and checks all targets as a release would.
It prints each image's source, target, digest, and planned action (push, skip, overwrite, or conflict), as well as the chart target.
Mapping errors and conflicts cause the dry run to fail.

### `--force`: Overwrite existing releases

Use `--force` to overwrite conflicting image tags and already released chart versions, for example when testing releases:

```shell
cldk release --namespace <ns> --force <chart.tgz>
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
cldk release --namespace <ns> --reg registry.example.com <chart.tgz>
```

### `--plain-http` and `--insecure`: Configure registry transport

| Flag           | Target registry connection                                                 |
|----------------|----------------------------------------------------------------------------|
| `--plain-http` | HTTP without TLS.                                                          |
| `--insecure`   | TLS without certificate verification, e.g. with a self-signed certificate. |

### `--user` and `--pass`: Authenticate to the target registry

Supply both flags together to set the target registry credentials:

```shell
cldk release --namespace <ns> --user <user> --pass <password> <chart.tgz>
```

Target registry credentials are selected in this order:

1. `--user` and `--pass`, supplied together.
2. `CLDK_USER` and `CLDK_PASSWORD`, set together when neither credential flag is supplied.
3. Stored credentials from `helm registry login` or `docker login`.

Credentials are never persisted by cldk.

### `--verbose`, `-v`: Enable debug logging

```shell
cldk -v release --namespace <ns> <chart.tgz>
```

### `--help`: Show commands and options

```shell
cldk --help         # show available commands
cldk release --help # show release options
```

### Print the version

Use `cldk version` to print the version and commit of cldk.

## Building

Requirements: GNU make and Docker (the local build runs inside a `golang` container), Go 1.27 for running tests.

```shell
make                  # builds the static linux/amd64 binary target/cldk
make unit-test        # runs the unit tests, reports are written to target/unit-tests
make integration-test # runs the tests tagged with "integration"
make static-analysis  # runs golangci-lint
make mocks            # generates mocks with mockery (configured in .mockery.yaml)
make release-assets   # builds the release archives of all platforms and checksums.txt into target/dist
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
