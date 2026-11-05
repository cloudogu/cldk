# Ein Dogu mit `cldk dev-push` in einen Cluster bringen

`cldk dev-push` bringt den Arbeitsstand eines v3-Dogus in einen Cluster, ohne das Dogu zu releasen.
Der Befehl baut die Images aus der `cldk.yaml`, pusht sie und das Chart unter einer neuen Dev-Version in eine Registry und legt die Dogu-CR an oder aktualisiert sie.
Der Dogu-Operator installiert das Dogu dann aus diesem Chart statt aus der Dogu-Registry.

Diese Anleitung zeigt zwei Beispiele mit dem Nexus-Dogu:

- [Beispiel 1](#beispiel-1-cluster-mit-registrycloudogucom-und-namespace-testing): ein normaler Cluster, der seine Dogus von `registry.cloudogu.com` bezieht
- [Beispiel 2](#beispiel-2-lokaler-k3d-cluster-mit-der-k3d-registry): ein lokaler k3d-Cluster mit der Registry von k3d

Alle Optionen beschreibt die [README](../../README.md#dev-push).

## Ablauf

1. `cldk` lädt die `cldk.yaml` und das Chart, prüft den Cluster und lintet das umgeschriebene Chart.
2. Jedes Image der `cldk.yaml` wird gebaut und nach `<reg>/<namespace>/dogu/v3/images/<name>:<dev-version>` gepusht.
3. Das Chart bekommt die neuen Image-Referenzen und wird nach `oci://<reg>/<namespace>/dogu/v3/charts/<name>:<dev-version>` gepusht.
4. Die Dogu-CR bekommt `spec.version: <dev-version>` und die Annotation `k8s.cloudogu.com/dev-chart-url` mit der URL des Charts.
5. Der Dogu-Operator legt im Dev-Modus ein `OCIRepository` auf diese URL an, Flux installiert das Chart.

Die Dev-Version ist `<version aus Chart.yaml>-dev.<UTC yyyymmddhhmmss>`, z.B. `3.96.4-3-dev.20261105131320`.
Jeder Push erzeugt eine neue Version, damit der Cluster den neuen Stand sofort übernimmt.

## Voraussetzungen

### Dogu-Repository

- Das Chart des Dogus liegt im Repository, standardmäßig unter `k8s/helm`.
  Die `version` in `Chart.yaml` muss eine gültige semantische Version sein, z.B. die Dogu-Version `3.96.4-3`.
- Sollen Images gebaut werden, braucht das Chart eine `mirror-patch-tpl.yaml`, über die `cldk` die Image-Referenzen umschreibt.
- Im Wurzelverzeichnis liegt eine `cldk.yaml`, für Nexus z.B.:

  ```yaml
  namespace: official   # Dogu-Namespace in der Registry, in Beispiel 1 per .env überschrieben
  images:
    nexus: {}           # baut ./Dockerfile, ersetzt values.images.nexus der mirror-patch-tpl.yaml
  ```

- Die Einstellungen der eigenen Umgebung gehören in die `.env` des Repositorys.
  Sie ist lokal und wird nicht committet; die Beispiele unten zeigen je eine passende `.env`.

### Dogu-Operator mit Dev-Modus

Der Dogu-Operator nutzt Dev-Charts nur, wenn sein Dev-Modus eingeschaltet ist.
In einem CES-Cluster geschieht das über die Component des Operators:

```shell
kubectl -n ecosystem edit component k8s-dogu-operator
```

```yaml
spec:
  valuesYamlOverwrite: |
    controllerManager:
      env:
        doguV3Enabled: true
        doguV3DevChartsEnabled: true
```

Bestehende Einträge in `valuesYamlOverwrite` bleiben erhalten, `doguV3DevChartsEnabled: true` kommt hinzu.

> **Achtung:** Der Dev-Modus ist nur für Entwicklungs- und Testcluster gedacht.
> Mit eingeschaltetem Dev-Modus kann jeder, der Dogu-CRs ändern darf, beliebige Charts in den Cluster bringen.

### Ziel-Cluster festlegen

Ohne Angabe nutzt `dev-push` den aktuellen Context der Kubeconfig.
Liegen in der Kubeconfig auch Produktivcluster, landet die Dogu-CR schnell im falschen Cluster.
Deshalb gehört der Cluster in die `.env`, entweder über eine eigene Kubeconfig oder über einen Context:

```shell
KUBECONFIG=/home/me/.kube/test-cluster
# oder
CLDK_DEV_KUBE_CONTEXT=test-cluster
```

`dev-push` gibt Context, Server und Namespace vor dem Bauen aus; `--dry-run` zeigt sie samt der Dogu-CR, ohne etwas zu ändern.

## Beispiel 1: Cluster mit `registry.cloudogu.com` und Namespace `testing`

Der Cluster bezieht seine Dogus von `registry.cloudogu.com`.
Der Dev-Stand wird in den Namespace `testing` gepusht, damit er nicht neben den Releases in `official` liegt.

### Anmelden

Der Builder pusht die Images, `cldk` das Chart; beide brauchen Zugangsdaten mit Schreibrecht auf `testing`:

```shell
docker login registry.cloudogu.com
```

`cldk` liest die gespeicherten Zugangsdaten von `docker login` oder `helm registry login`.

### `.env`

```shell
KUBECONFIG=/home/me/.kube/test-cluster
CLDK_DEV_KUBE_NAMESPACE=ecosystem
CLDK_DEV_REG=registry.cloudogu.com
CLDK_DEV_NAMESPACE=testing
```

`CLDK_DEV_CLUSTER_REG` bleibt leer: Der Cluster erreicht die Registry unter derselben Adresse.
`CLDK_DEV_PLAIN_HTTP` und `CLDK_DEV_INSECURE` bleiben leer, die Registry hat ein gültiges Zertifikat.

### Push

```shell
cldk dev-push --dry-run
cldk dev-push
```

Ergebnis:

| Was                                         | Referenz                                                                               |
|---------------------------------------------|----------------------------------------------------------------------------------------|
| Image                                       | `registry.cloudogu.com/testing/dogu/v3/images/nexus:3.96.4-3-dev.20261105131320`       |
| Chart                                       | `oci://registry.cloudogu.com/testing/dogu/v3/charts/nexus:3.96.4-3-dev.20261105131320` |
| Annotation `k8s.cloudogu.com/dev-chart-url` | `oci://registry.cloudogu.com/testing/dogu/v3/charts/nexus`                             |
| `spec.doguNamespace`                        | `testing`                                                                              |

Die Dogu-CR, wie `cldk` sie anwendet:

```yaml
apiVersion: k8s.cloudogu.com/v3beta1
kind: Dogu
metadata:
  name: nexus
  annotations:
    k8s.cloudogu.com/dev-chart-url: oci://registry.cloudogu.com/testing/dogu/v3/charts/nexus
spec:
  name: nexus
  doguNamespace: testing
  doguApiVersion: v3
  version: 3.96.4-3-dev.20261105131320
```

### Was der Cluster braucht

- Der Cluster lädt Chart und Images mit den Zugangsdaten des Secrets `ces-container-registries`.
  Diese Zugangsdaten müssen den Namespace `testing` lesen dürfen.
- Charts, die ihre Images mit `global.imagePullSecrets` auf `ces-container-registries` beziehen (wie Nexus), brauchen keine weitere Einstellung.

## Beispiel 2: Lokaler k3d-Cluster mit der k3d-Registry

Ein lokaler k3d-Cluster mit einer Registry, die k3d angelegt hat.
Die Registry ist vom Rechner und vom Cluster aus unter verschiedenen Adressen erreichbar und spricht nur HTTP.

### Adressen der Registry

|                        | Adresse                                   | Variable               |
|------------------------|-------------------------------------------|------------------------|
| vom Rechner aus (Push) | `localhost:<host-port>`                   | `CLDK_DEV_REG`         |
| vom Cluster aus (Pull) | `k3d-<registry-name>:<port im Container>` | `CLDK_DEV_CLUSTER_REG` |

`k3d registry list` und `docker ps` zeigen Name und Ports der Registries.

Im k3d-Setup von `k8s-ecosystem` (`ces-k3d`) gibt es zwei Registries mit gemeinsamem Speicher:
`k3d-registry-dev.localhost` nimmt auf `localhost:5001` Pushes an, und der Cluster liest über `k3d-registry-proxy.localhost:5000`.

### `.env`

Für das k3d-Setup von `k8s-ecosystem` mit der Instanz `dev1`:

```shell
KUBECONFIG=/home/me/.kube/dev1.k3ces.localdomain
CLDK_DEV_KUBE_NAMESPACE=ecosystem
CLDK_DEV_REG=localhost:5001
CLDK_DEV_CLUSTER_REG=k3d-registry-proxy.localhost:5000
CLDK_DEV_PLAIN_HTTP=true
```

Docker behandelt `localhost` ohnehin als unsichere Registry; eine Anmeldung ist bei den k3d-Registries nicht nötig.

### Push

```shell
cldk dev-push --dry-run
cldk dev-push
```

Ergebnis:

| Was                                              | Referenz                                                                                      |
|--------------------------------------------------|-----------------------------------------------------------------------------------------------|
| Image (Push)                                     | `localhost:5001/official/dogu/v3/images/nexus:3.96.4-3-dev.20261105131320`                    |
| Image im Chart                                   | `k3d-registry-proxy.localhost:5000/official/dogu/v3/images/nexus:3.96.4-3-dev.20261105131320` |
| Chart (Push)                                     | `oci://localhost:5001/official/dogu/v3/charts/nexus:3.96.4-3-dev.20261105131320`              |
| Annotation `k8s.cloudogu.com/dev-chart-url`      | `oci://k3d-registry-proxy.localhost:5000/official/dogu/v3/charts/nexus`                       |
| Annotation `k8s.cloudogu.com/dev-chart-insecure` | `"true"`                                                                                      |

Wegen `CLDK_DEV_PLAIN_HTTP=true` setzt `cldk` die Annotation `k8s.cloudogu.com/dev-chart-insecure`, und der Operator lädt das Chart per HTTP.

## Ergebnis prüfen

`dev-push` wartet nicht, bis das Dogu läuft, und gibt am Ende einen passenden Befehl zum Beobachten aus:

```shell
kubectl -n ecosystem get dogu nexus -w
```

Weitere Kontrollen:

```shell
# Events: DevChart, ChartAvailable, HelmRelease created/updated
kubectl -n ecosystem describe dogu nexus
# URL, Tag und insecure des Charts
kubectl -n ecosystem get ocirepository nexus -o yaml
# Image des neuen Pods
kubectl -n ecosystem get pods -l app.kubernetes.io/instance=nexus \
  -o jsonpath='{range .items[*]}{.metadata.name}: {.spec.containers[0].image}{"\n"}{end}'
```

`values`, `mappedValues` und `stopped` an der Dogu-CR bleiben bei jedem Push erhalten, `cldk` setzt nur seine eigenen Felder.

## Nur einzelne Images bauen

Mit `--replace-image` und `--skip-image` lassen sich Images für einen Push auslassen, z.B. wenn sich nur das Chart geändert hat:

```shell
# nichts bauen, die Image-Referenz aus values.yaml bleibt
cldk dev-push --skip-image nexus
# nichts bauen, ein vorhandenes Image ins Chart schreiben
cldk dev-push --replace-image nexus=registry.cloudogu.com/official/nexus:3.96.4-3
```

## Zurück zum regulären Dogu

Um wieder ein Release aus der Dogu-Registry zu nutzen, die Dev-Annotations entfernen und eine reguläre Version setzen:

```shell
kubectl -n ecosystem annotate dogu nexus k8s.cloudogu.com/dev-chart-url- k8s.cloudogu.com/dev-chart-insecure-
kubectl -n ecosystem patch dogu nexus --type merge -p '{"spec":{"version":"3.96.4-3","doguNamespace":"official"}}'
```

Der Operator reagiert nur auf Änderungen an der Spec.
Erst die neue `spec.version` sorgt dafür, dass er das Chart wieder aus der Dogu-Registry bezieht; die Annotations allein reichen nicht.
`doguNamespace` muss nur zurückgesetzt werden, wenn der Dev-Stand wie in Beispiel 1 in einem anderen Namespace lag.

## Fehlersuche

| Symptom                                                                              | Ursache und Lösung                                                                                                                                                                                                                                                                                                            |
|--------------------------------------------------------------------------------------|-------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `dev-push` zeigt den falschen Context                                                | Der Cluster kommt aus dem aktuellen Context der Kubeconfig. `KUBECONFIG` oder `CLDK_DEV_KUBE_CONTEXT` in die `.env` eintragen, siehe [Ziel-Cluster festlegen](#ziel-cluster-festlegen).                                                                                                                                       |
| Warning-Event `DevChartsDisabled`, das Dogu endet in `DownloadFailed`                | Der Dev-Modus des Operators ist aus, der Operator sucht die Dev-Version in der Dogu-Registry. Dev-Modus einschalten, siehe [Dogu-Operator mit Dev-Modus](#dogu-operator-mit-dev-modus).                                                                                                                                       |
| `dev-push` bricht ab, weil ein Classic-Dogu gleichen Namens installiert ist          | `dev-push` löst keine Migration von v2 nach v3 aus. Das Classic-Dogu erst entfernen oder migrieren.                                                                                                                                                                                                                           |
| Der Pod kann das Image nicht laden                                                   | Die Cluster-Adresse der Registry stimmt nicht (`CLDK_DEV_CLUSTER_REG`) oder die Zugangsdaten in `ces-container-registries` reichen nicht für den Namespace.                                                                                                                                                                   |
