# cldk
Cloudogu (LOP) Development Kit helps developers in releasing and developing their Dogu helm-charts.

## Usage

```shell
cldk release --namespace <ns> <chart.tgz>   # release a packed dogu chart to registry.cloudogu.com
cldk release --namespace <ns> -i <image> ... <chart.tgz>   # release the chart together with its images
cldk version        # print the version and commit of cldk
cldk -v <command>   # enable debug logging
cldk --help
```

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
- Use `--reg` for another target registry, `--plain-http` for a target registry without TLS and `--insecure` for a target registry with an untrusted (e.g. self-signed) certificate.
- The target registry is accessed with `--user`/`--pass` (or `CLDK_USER`/`CLDK_PASSWORD`) if given, else with the stored credentials of `helm registry login` or `docker login`.
- Credentials are never persisted.

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
