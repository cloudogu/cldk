# Referenz von `cldk dogu dev-push`

`cldk dogu dev-push` pusht den Arbeitsstand eines v3-Dogu-Repositorys unter einer neuen Dev-Version in eine Dev-Registry und installiert ihn in einem Cluster.
Eine Schritt-für-Schritt-Anleitung mit den Voraussetzungen im Cluster, Beispielen mit `registry.cloudogu.com` und einem lokalen k3d-Cluster und dem Weg zurück zum regulären Dogu steht unter [Ein Dogu mit `cldk dogu dev-push` in einen Cluster bringen](deploying_a_dogu_with_dev_push_de.md).
Das Projektverzeichnis braucht eine `cldk.yaml`, siehe [Projektkonfiguration](project_configuration_de.md#cldkyaml).

## Dev-Version und Chart

- Jeder Lauf erzeugt aus der `version` der `Chart.yaml` eine neue Dev-Version `<version>-dev.<UTC yyyymmddhhmmss>`, z.B. `1.2.3-dev.20261103143012`.
  - Die Version in der `Chart.yaml` muss eine gültige semantische Version ohne Build-Metadaten (`+…`) sein.
  - Das Chart wird nach `oci://<reg>/<namespace>/dogu/v3/charts/<name>:<dev-version>` gepusht, im selben Schema wie bei `release`.
- Das Chart wird wie bei `helm package` aus seinem Verzeichnis geladen, einschließlich `.helmignore`.
  - Deklariert die `Chart.yaml` Abhängigkeiten, müssen sie in `charts/` liegen; vorher `helm dependency build <chart dir>` ausführen.
  - Das Chart wird mit `helm lint` (einschließlich `values.schema.json`) geprüft, bevor etwas gebaut oder gepusht wird; Fehler brechen ab, Warnungen werden geloggt.
  - Mit `--values` wird das Chart mit diesen Values gelintet, wie bei `helm lint --values`.

## Optionen

Die Dev-Registry, der Zugriff darauf und der Cluster werden als Flags oder Umgebungsvariablen angegeben, Flags zuerst:

| Flag                           | Umgebungsvariable         | Beschreibung                                                                            |
|--------------------------------|---------------------------|-----------------------------------------------------------------------------------------|
| `--reg <host[:port]>`          | `CLDK_DEV_REG`            | Adresse der Dev-Registry, in die gepusht wird, Pflicht                                  |
| `--cluster-reg <host[:port]>`  | `CLDK_DEV_CLUSTER_REG`    | Adresse der Dev-Registry aus Sicht des Clusters, Standard `--reg`                       |
| `--dogu-namespace <ns>`        | `CLDK_DEV_DOGU_NAMESPACE` | Dogu-Namespace, Standard der `doguNamespace` der `cldk.yaml`                            |
| `--plain-http`                 | `CLDK_DEV_PLAIN_HTTP`     | Dev-Registry ohne TLS                                                                   |
| `--insecure`                   | `CLDK_DEV_INSECURE`       | Dev-Registry mit nicht vertrauenswürdigem (z.B. selbstsigniertem) Zertifikat            |
| `--kube-context <name>`        | `CLDK_DEV_KUBE_CONTEXT`   | kubeconfig-Kontext des Clusters, Standard der aktuelle Kontext                          |
| `--kube-namespace <ns>`        | `CLDK_DEV_KUBE_NAMESPACE` | Kubernetes-Namespace des Dogus, Standard der Namespace des Kontexts, sonst `default`    |
| `--builder <bin>`              | `CLDK_BUILDER`            | Docker-kompatibles Build-CLI, Standard `docker`; gemeinsam mit `release`                |
| `--platform <os/arch>`         | `CLDK_PLATFORM`           | Plattform der Images, wird an den Builder durchgereicht; gemeinsam mit `release`        |
| `--no-cache`                   | `CLDK_NO_CACHE`           | Images ohne den Cache des Builders bauen, z.B. um einen sauberen Build zu erzwingen     |
| `--values <file>`              | `CLDK_DEV_VALUES`         | YAML-Datei mit Helm-Values für das Dogu, relativ zum Projektverzeichnis                 |
| `--build-arg KEY[=VALUE]`      |                           | Build-Argument für alle Images, wiederholbar                                            |
| `--replace-image <name>=<ref>` |                           | Image `<name>` nicht bauen, stattdessen `<ref>` ins Chart schreiben, wiederholbar       |
| `--skip-image <name>`          |                           | Image `<name>` weder bauen noch ins Chart schreiben, wiederholbar                       |
| `--dry-run`                    |                           | alle Prüfungen ausführen und den Plan ausgeben, aber nichts bauen, pushen oder anwenden |

- Boolesche Variablen nehmen die Werte von Gos `strconv.ParseBool` an, z.B. `true`, `false`, `1`, `0`; eine leere Variable gilt als nicht gesetzt.
- Auf die Dev-Registry wird mit den gespeicherten Zugangsdaten von `docker login` oder `helm registry login` zugegriffen.
- Mit `-v` loggt `dev-push` den aufgelösten Wert jeder Option zusammen mit ihrer Quelle, also Flag, Umgebungsvariable oder Standardwert.
- Die Einstellungen der eigenen Umgebung gehören in die [`.env`](project_configuration_de.md#env) des Dogu-Repositorys, z.B. für einen k3d-Cluster mit lokaler Registry:

  ```shell
  CLDK_DEV_REG=localhost:5001
  CLDK_DEV_CLUSTER_REG=k3d-registry-proxy.localhost:5000
  CLDK_DEV_PLAIN_HTTP=true
  ```

## Images bauen

- `dev-push` baut die Images der `cldk.yaml` nacheinander, sortiert nach Namen, und pusht sie danach in derselben Reihenfolge:
  - `<builder> build -f <dockerfile> -t <reg>/<namespace>/dogu/v3/images/<name>:<dev-version> [--target …] [--build-arg …] [--platform …] [--no-cache] <context>`
  - `<builder> push <reg>/<namespace>/dogu/v3/images/<name>:<dev-version>`
- Der Builder ist ein beliebiges CLI mit Docker-kompatiblen Befehlen `build` und `push`, z.B. `docker` oder `podman`.
  - Er muss im `PATH` liegen, wenn mindestens ein Image gebaut wird.
  - Er erbt die Umgebung von `cldk`, seine Ausgabe geht nach stderr; die gebauten Images bleiben in seinem lokalen Store.
  - Der Builder muss der Dev-Registry vertrauen und bei Bedarf dort angemeldet sein, z.B. als insecure Registry bei einer Registry ohne TLS; `--plain-http` und `--insecure` gelten nur für den Chart-Push von `cldk`.
- `--platform` nimmt genau eine Plattform; ohne die Option entscheidet der Builder.
- `--build-arg KEY=VALUE` gilt für alle Images und überschreibt `buildArgs` mit demselben Schlüssel; `--build-arg KEY` übernimmt den Wert wie `docker build` aus der Umgebung.
- Eine Kopie des Charts bekommt die Referenzen der gebauten Images auf der Cluster-Adresse der Dev-Registry, z.B. `k3d-registry-proxy.localhost:5000/official/dogu/v3/images/nexus:<dev-version>`.
  - `dev-push` rendert die `mirror-patch-tpl.yaml` mit den neuen Referenzen und führt die Patches ins Chart ein.
  - Das Chart wird vor dem ersten Build umgeschrieben und gelintet.
- Der erste fehlgeschlagene Build oder Push bricht mit dem Image-Namen und dem Exit-Code des Builders ab; ein fehlgeschlagener Build pusht nichts, vor einem fehlgeschlagenen Push gepushte Images bleiben in der Dev-Registry.
- Gebaut werden nur die Images der `cldk.yaml`; die übrigen Images des Charts bleiben ohne weiteres Flag unverändert.
- Einzelne Images der `cldk.yaml` lassen sich für einen Lauf ersetzen oder überspringen, z.B. um nur an einem Image zu arbeiten:

  ```shell
  # mit den Images nexus, sidecar und postgres in der cldk.yaml:
  #   nur nexus bauen, ein vorhandenes sidecar-Image nehmen und das postgres-Image des Charts behalten
  cldk dogu dev-push --replace-image sidecar=registry.example.com/foo/sidecar:1.0 --skip-image postgres
  ```

  - `--replace-image <name>=<ref>` baut und pusht das Image nicht; `<ref>` wird unverändert ins Chart geschrieben, in denselben Eintrag von `values.images` wie das gebaute Image.
  - `--skip-image <name>` baut das Image nicht und ändert seine Referenz im Chart nicht, als stünde es nicht in der `cldk.yaml`.
  - Jeder Name muss ein Image der `cldk.yaml` sein und darf über beide Flags hinweg nur einmal vorkommen.
  - Werden alle Images ersetzt oder übersprungen, wird nichts gebaut und kein Builder gebraucht; werden alle übersprungen, wird das Chart nicht umgeschrieben.

## Im Cluster installieren

- Nach dem Push des Charts wendet `dev-push` die Dogu-CR (`k8s.cloudogu.com/v3beta1`, Kind `Dogu`) per Server-side Apply mit dem Field Manager `cldk` im Cluster an:
  - `spec.version` ist die Dev-Version, `spec.doguApiVersion` ist `v3`, `spec.name` und `metadata.name` sind der `name` aus der `Chart.yaml`, `spec.doguNamespace` ist der Dogu-Namespace.
  - Die Annotation `k8s.cloudogu.com/dev-chart-url` enthält die URL des Charts ohne Tag auf der Cluster-Adresse der Dev-Registry, z.B. `oci://k3d-registry-proxy.localhost:5000/official/dogu/v3/charts/nexus`.
  - Die Annotation `k8s.cloudogu.com/dev-chart-insecure: "true"` wird mit `--plain-http` oder `--insecure` gesetzt und ohne sie entfernt.
  - Mit `--values` bekommt `spec.values` die Values der Datei; eine leere Datei setzt keine Values.
  - Alle anderen Felder eines bestehenden Dogus, z.B. `mappedValues`, `stopped` oder von anderen gesetzte Values, bleiben erhalten.
- Values gehören pro Schlüssel ihrem Setzer:
  - Ein Value der Datei überschreibt denselben Value, der vorher gesetzt wurde, z.B. mit `kubectl`.
  - Ein Value, den `cldk` vorher gesetzt hat und der nicht mehr in der Datei steht, wird aus dem Dogu entfernt, ebenso bei einem Push ohne `--values`.
  - Listen werden als Ganzes ersetzt.
  - Steht `CLDK_DEV_VALUES` in der `.env`, setzt jeder Push dieselben Values.
- `KUBECONFIG` darf in der `.env` gesetzt werden.
  - Einen anderen Kontext oder Namespace wählen `--kube-context` und `--kube-namespace`.
- Vor Lint und Push loggt `dev-push` Kontext, Server und Namespace des Clusters.
- `dev-push` wartet nicht, bis das Dogu bereit ist.
- Schlägt das Anwenden nach dem Push fehl, bleiben die gepushten Images und das Chart in der Dev-Registry.

## Dry Run

- `--dry-run` prüft einen Dev-Push, ohne etwas zu bauen, zu pushen oder anzuwenden:

  ```shell
  cldk dogu dev-push --dry-run
  ```

  - Er führt alle Prüfungen eines Dev-Pushs aus: `cldk.yaml`, das Chart und seine Version, den Builder (wenn ein Image gebaut wird), die Verbindung zum Cluster sowie Umschreiben und Lint des Charts.
  - Er gibt die Dev-Version aus, jedes Image mit seiner Push-Referenz, seiner ersetzten Referenz oder als übersprungen, die Chart-Referenz, den Ziel-Cluster und die Dogu-CR als YAML, so wie sie angewendet würde.
  - Eine fehlgeschlagene Prüfung lässt auch den Dry Run fehlschlagen.
  - `--dry-run` hat keine Umgebungsvariable, damit eine `.env` einen Dev-Push nie in einen Dry Run verwandelt.
