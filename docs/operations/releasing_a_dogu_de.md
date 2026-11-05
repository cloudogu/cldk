# Ein Dogu mit `cldk release` veröffentlichen

`cldk release` veröffentlicht ein gepacktes Chart eines v3-Dogus und optional seine Images in einer Registry.
Die Images werden unverändert mit demselben Digest kopiert, das Chart bekommt die neuen Image-Referenzen.

Diese Anleitung zeigt zwei Beispiele mit dem Nexus-Dogu, beide mit `registry.cloudogu.com` und dem Namespace `testing`:

- [Beispiel 1](#beispiel-1-chart-ohne-images): nur das Chart veröffentlichen
- [Beispiel 2](#beispiel-2-chart-mit-image): das Chart zusammen mit dem Nexus-Image veröffentlichen

Alle Optionen beschreibt die [README](../../README.md#release).

## Ablauf

1. `cldk` liest Name und Version aus der `Chart.yaml` des gepackten Charts.
2. Jedes Image, das mit `-i` angegeben ist, wird in seiner Quell-Registry aufgelöst und nach `<reg>/<namespace>/dogu/v3/images/<name>:<tag>` kopiert.
3. Das Chart bekommt die neuen Image-Referenzen über seine `mirror-patch-tpl.yaml` und wird neu gepackt.
4. Das Chart wird nach `oci://<reg>/<namespace>/dogu/v3/charts/<name>:<version>` gepusht, nach allen Images.

Vor dem ersten Push prüft `cldk` alle Ziele.
Bereits veröffentlichte Artefakte werden nie still überschrieben, siehe [Bestehende Artefakte](#bestehende-artefakte).

## Voraussetzungen

### Gepacktes Chart

`cldk release` nimmt das Chart als `.tgz`, wie `helm package` es erzeugt:

```shell
helm package k8s/helm -d target
# Successfully packaged chart and saved it to: target/nexus-1.2.3.tgz
```

Die `version` in `Chart.yaml` ist die veröffentlichte Chart-Version, z.B. `1.2.3`.
Sie ist unabhängig von der Version der Anwendung und den Tags ihrer Images, bei Nexus z.B. `3.96.4`.

### Images in einer Registry

`cldk release` liest Images nur aus Registries, nie aus dem lokalen Docker-Daemon.
Ein lokal gebautes Image muss deshalb vorher in eine Registry gepusht werden, z.B. in die Registry der CI:

```shell
docker build -t ci-registry.example.com/nexus:3.96.4 .
docker push ci-registry.example.com/nexus:3.96.4
```

Jedes Image wird mit seiner Registry angegeben, also `docker.io/library/postgres:14.18` statt `postgres:14.18`.
So wird nichts versehentlich von docker.io gezogen.

Sollen Images veröffentlicht werden, braucht das Chart eine `mirror-patch-tpl.yaml`.
Für Nexus enthält sie unter `values.images` je einen Eintrag pro Image:

```yaml
values:
  images:
    nexus: registry.cloudogu.com/official/nexus:3.96.4
    nexusSaManager: registry.cloudogu.com/k8s/service-account-producer-sidecar:0.1.2
    postgresql: docker.io/library/postgres:14.18
```

Nur die Einträge der Images, die mit `-i` angegeben sind, ändern sich; alle anderen bleiben, wie sie sind.

### Zugangsdaten

Für die Ziel-Registry nutzt `cldk` die gespeicherten Zugangsdaten von `helm registry login` oder `docker login`:

```shell
docker login registry.cloudogu.com
```

Alternativ gelten `--user` und `--pass` bzw. `CLDK_USER` und `CLDK_PASSWORD`, z.B. in der CI oder in der `.env` des Repositorys.
Die Quell-Images werden immer mit den Zugangsdaten von `docker login` gelesen.
`cldk` speichert keine Zugangsdaten.

Die Zugangsdaten brauchen Schreibrecht auf den Ziel-Namespace, in den Beispielen `testing`.

## Beispiel 1: Chart ohne Images

Das Chart wird so veröffentlicht, wie es ist; seine Image-Referenzen zeigen weiter auf die Einträge aus `values.yaml`.

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

`--namespace` ist Pflicht, damit nichts versehentlich in `official` landet.
`--reg` ist nicht nötig, `registry.cloudogu.com` ist der Default.

## Beispiel 2: Chart mit Image

Das Nexus-Image aus der CI-Registry wird zusammen mit dem Chart veröffentlicht:

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

Ergebnis:

| Was                        | Referenz                                                         |
|----------------------------|------------------------------------------------------------------|
| Image                      | `registry.cloudogu.com/testing/dogu/v3/images/nexus:3.96.4`      |
| Chart                      | `oci://registry.cloudogu.com/testing/dogu/v3/charts/nexus:1.2.3` |
| `values.images.nexus`      | `registry.cloudogu.com/testing/dogu/v3/images/nexus:3.96.4`      |
| `values.images.postgresql` | unverändert `docker.io/library/postgres:14.18`                   |

Das Image behält Namen und Tag der Quelle: Der Name ist das letzte Pfadsegment, hier `nexus`, der Tag bleibt `3.96.4`.
Es landet im Eintrag von `values.images` mit demselben Repository-Namen, hier `nexus`.
Das Chart wird unter seiner eigenen Version `1.2.3` veröffentlicht.

### Name, Tag und Eintrag explizit setzen

Passen Name oder Tag der Quelle nicht, setzt `=<name>:<tag>` sie explizit:

```shell
cldk release --namespace testing \
  -i ci-registry.example.com/dogus/nexus-build:42=nexus:3.96.4 \
  target/nexus-1.2.3.tgz
```

Passt kein oder mehr als ein Eintrag von `values.images` zum Namen, ordnet `--image-key <name>=<key>` das Image zu:

```shell
cldk release --namespace testing \
  -i ci-registry.example.com/sa-sidecar:0.1.2=service-account-producer-sidecar:0.1.2 \
  --image-key service-account-producer-sidecar=nexusSaManager \
  target/nexus-1.2.3.tgz
```

## Bestehende Artefakte

`cldk` prüft alle Ziele vor dem ersten Push:

| Ziel existiert schon               | Verhalten                                                |
|------------------------------------|----------------------------------------------------------|
| Image-Tag mit demselben Digest     | wird übersprungen (`Skipped image … (already released)`) |
| Image-Tag mit einem anderen Digest | Abbruch, nichts wird gepusht                             |
| Chart-Version                      | Abbruch, nichts wird gepusht                             |

Ein abgebrochenes Release lässt sich deshalb einfach wiederholen: Schon kopierte Images werden übersprungen.

Mit `--force` werden Image-Tags und Chart-Version bewusst überschrieben, z.B. um ein Release in `testing` zu korrigieren:

```shell
cldk release --force --namespace testing -i ci-registry.example.com/nexus:3.96.4 target/nexus-1.2.3.tgz
```

## Ergebnis prüfen

```shell
# Chart und seine Image-Referenzen
helm show chart oci://registry.cloudogu.com/testing/dogu/v3/charts/nexus --version 1.2.3
helm show values oci://registry.cloudogu.com/testing/dogu/v3/charts/nexus --version 1.2.3 | grep -A3 'image:'
# Digest des Images, gleich dem Digest der Quelle
docker buildx imagetools inspect registry.cloudogu.com/testing/dogu/v3/images/nexus:3.96.4
```

## Fehlersuche

| Symptom                                                                                       | Ursache und Lösung                                                                                                                                |
|-----------------------------------------------------------------------------------------------|---------------------------------------------------------------------------------------------------------------------------------------------------|
| Das Release bricht wegen eines bestehenden Image-Tags oder einer bestehenden Chart-Version ab | Die Version ist schon veröffentlicht. Eine neue Chart-Version bauen oder in `testing` bewusst mit `--force` überschreiben.                        |
| Ein Image ohne Registry wird abgelehnt                                                        | Images werden immer mit ihrer Registry angegeben, z.B. `docker.io/library/postgres:14.18`.                                                        |
| Ein lokal gebautes Image wird nicht gefunden                                                  | `cldk` liest nicht aus dem Docker-Daemon. Das Image vorher in eine Registry pushen.                                                               |
| Das Chart wird mit Images abgelehnt                                                           | Dem Chart fehlt die `mirror-patch-tpl.yaml`.                                                                                                      |
| Ein Image passt zu keinem oder mehreren Einträgen von `values.images`                         | Den Eintrag mit `--image-key <name>=<key>` zuordnen.                                                                                              |
| Die Ziel-Registry lehnt die Anmeldung ab                                                      | `docker login registry.cloudogu.com` ausführen oder `CLDK_USER`/`CLDK_PASSWORD` setzen; die Zugangsdaten brauchen Schreibrecht auf den Namespace. |
