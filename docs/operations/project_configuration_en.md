# Project configuration

`cldk` works on a project directory, usually the root directory of a dogu repository.
The project directory is the working directory or the one given with `--dir`/`-d`.

## `cldk.yaml`

`cldk dogu dev-push` and `cldk dogu release` without chart archive need a `cldk.yaml` in the project directory:

```yaml
doguNamespace: official    # dogu namespace in the registry, required
chart: k8s/helm            # chart directory, default k8s/helm
images:                    # images to build, optional; without entries nothing is built
  nexus:                   # image name, pushed to <reg>/<namespace>/dogu/v3/images/<name>
    context: .             # build context, default .
    dockerfile: Dockerfile # Dockerfile, default <context>/Dockerfile
    key: nexus             # key in values.images of mirror-patch-tpl.yaml, default the entry with the repository name <name>
    target: ""             # stage of a multi-stage build, optional
    buildArgs:             # static build arguments, optional
      FOO: bar
    tag: 3.96.4-1          # release tag of dogu release, optional; dev-push ignores it
```

All paths are relative to the project directory; `nexus: {}` is a valid image entry with all defaults.

Unknown fields in `cldk.yaml` abort the command, so that typos are not silently ignored.

## `.env`

- Every command first loads the `.env` of the project directory into its environment, e.g. `CLDK_USER`/`CLDK_PASSWORD` for `release`.
- Variables that are already set in the environment, even if empty, take precedence over the `.env`; flags take precedence over both.
- A missing `.env` is fine, a `.env` that cannot be parsed aborts the command.
- Debug logging with `-v`, e.g. `cldk -v dogu dev-push`, shows the names of the variables taken from the `.env`, never their values; `release` and `dev-push` log the resolved values of their own settings with their source, never a password.
