# cldk
Cloudogu (LOP) Development Kit helps developers in releasing and developing their Dogu helm-charts.

## Overview

A dogu v3 is a Helm chart plus its container images, both stored in an OCI registry:

```text
oci://<reg>/<namespace>/dogu/v3/charts/<name>:<version>
<reg>/<namespace>/dogu/v3/images/<name>:<tag>
```

The chart refers to its images in `values.images` of its `mirror-patch-tpl.yaml`.
`cldk` writes the new image references there whenever it publishes images together with a chart.

`cldk` has two commands for this:

| Command              | Use                          | What it does                                                                                                                              |
|----------------------|------------------------------|-------------------------------------------------------------------------------------------------------------------------------------------|
| `cldk dogu release`  | Releasing, e.g. in the CI    | Copies existing images unchanged to the registry and publishes the packed chart with their references.                                    |
| `cldk dogu dev-push` | Developing against a cluster | Builds the images of the repository, pushes them and the chart under a new dev version and installs this version in a Kubernetes cluster. |

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

```shell
cldk dogu release --dogu-namespace <ns> <chart.tgz>   # release a packed dogu chart to registry.cloudogu.com
cldk dogu release --dogu-namespace <ns> -i <image> ... <chart.tgz>   # release the chart together with its images
cldk dogu release --dry-run --dogu-namespace <ns> -i <image> ... <chart.tgz>   # print the plan without pushing anything
cldk dogu dev-push   # push the dogu v3 under development in the working directory and install it in the current cluster
cldk version        # print the version and commit of cldk
cldk -v <command>   # enable debug logging of cldk, debug output of libraries like Helm is left out
cldk -d <dir> <command>   # use <dir> as project directory instead of the working directory
cldk --help         # show available commands
cldk dogu release --help   # show the options of a command
```

### `dogu release`

`cldk dogu release` publishes a packed chart of a dogu v3 and optionally its images to a registry, by default `registry.cloudogu.com`.
The images are copied unchanged with the same digest, the chart gets the new image references.

- Guide: [Releasing a dogu with `cldk dogu release`](docs/operations/releasing_a_dogu_en.md) ([Deutsch](docs/operations/releasing_a_dogu_de.md))
- All options: [`cldk dogu release` reference](docs/operations/dogu_release_reference_en.md) ([Deutsch](docs/operations/dogu_release_reference_de.md))

### `dogu dev-push`

`cldk dogu dev-push` pushes the working state of a dogu v3 repository under a new dev version to a dev registry and installs it in a cluster.
The project directory is not changed.

- Guide: [Deploying a dogu with `cldk dogu dev-push`](docs/operations/deploying_a_dogu_with_dev_push_en.md) ([Deutsch](docs/operations/deploying_a_dogu_with_dev_push_de.md))
- All options: [`cldk dogu dev-push` reference](docs/operations/dogu_dev_push_reference_en.md) ([Deutsch](docs/operations/dogu_dev_push_reference_de.md))

### Project directory, `cldk.yaml` and `.env`

`cldk` works on a project directory, the working directory or the one given with `--dir`/`-d`.
It contains the `cldk.yaml` of the dogu and an optional `.env`, which is loaded into the environment of every command.
See [Project configuration](docs/operations/project_configuration_en.md) ([Deutsch](docs/operations/project_configuration_de.md)).

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
