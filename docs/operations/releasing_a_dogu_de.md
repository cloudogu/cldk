# Ein Dogu mit `cldk dogu release` veröffentlichen

`cldk dogu release` veröffentlicht ein v3-Dogu, also sein Chart und seine Images, in einem Namespace einer Registry.
Es gibt zwei Modi:

- **Projekt-Release** (ohne Argument): `cldk` baut die Images des Dogu-Repositorys und veröffentlicht sie zusammen mit dem Chart des Repositorys.
- **Archiv-Release** (mit `.tgz` als Argument): `cldk` veröffentlicht ein gepacktes Chart; Images werden unverändert aus anderen Registries kopiert.

Diese Anleitung zeigt Beispiele mit dem Nexus-Dogu, alle mit `registry.cloudogu.com` und dem Namespace `testing`:

- [Beispiel 1](#beispiel-1-projekt-release): das Dogu aus seinem Repository veröffentlichen
- [Beispiel 2](#beispiel-2-projekt-release-mit-einem-image-aus-der-ci): das Dogu mit einem in der CI gebauten Image veröffentlichen
- [Beispiel 3](#beispiel-3-chart-ohne-images): nur ein gepacktes Chart veröffentlichen
- [Beispiel 4](#beispiel-4-chart-mit-image): ein gepacktes Chart zusammen mit dem Nexus-Image veröffentlichen

Alle Optionen beschreibt die [README](../../README.md#dogu-release).

## Projekt-Release

### Ablauf

1. `cldk` liest die `cldk.yaml` und das Chart-Verzeichnis im Projektverzeichnis (`--dir`, Default das Arbeitsverzeichnis).
2. Es prüft die Release-Tags, den Builder und das Chart: Das Chart wird über seine `mirror-patch-tpl.yaml` auf die veröffentlichten Images umgeschrieben und gelintet.
3. Es prüft alle Ziele: Ein bestehender Release-Tag oder eine bestehende Chart-Version bricht das Release ab, siehe [Bestehende Artefakte](#bestehende-artefakte-eines-projekt-release).
4. Es baut alle Images der `cldk.yaml` mit dem Builder (Default `docker`) und pusht sie danach nach `<reg>/<namespace>/dogu/v3/images/<name>:<Release-Tag>`.
5. Es kopiert die Images, die mit `-i` angegeben sind.
6. Es pusht das Chart nach `oci://<reg>/<namespace>/dogu/v3/charts/<name>:<version>`, mit der `version` aus `Chart.yaml`.

Gebaut wird erst, wenn alle Prüfungen bestanden sind, und ein fehlgeschlagener Build hinterlässt nichts in der Registry.
Das Projektverzeichnis bleibt unverändert.

### Release-Tags in der `cldk.yaml`

Jedes zu bauende Image braucht einen Release-Tag, den Tag, unter dem es veröffentlicht wird:

```yaml
namespace: official
images:
  nexus:
    tag: 3.96.4-1
  sidecar:
    dockerfile: sidecar/Dockerfile
    key: nexusSaManager
    tag: 1.2.0
```

`--tag <name>=<tag>` überschreibt den Tag der `cldk.yaml`, z.B. in der CI oder für ein Testing-Release:

```shell
cldk dogu release --namespace testing --tag nexus=3.96.4-1-rc.1
```

Fehlt einem Image der Release-Tag, bricht `cldk` vor dem ersten Build ab und nennt alle Images ohne Tag.
Die Release-Tags sind unabhängig von der Chart-Version, die immer die `version` aus `Chart.yaml` ist.

Der Namespace kommt aus der `cldk.yaml`; `--namespace` oder `CLDK_NAMESPACE` überschreiben ihn.

### Builder und Zugangsdaten

Der Builder baut und pusht die Images mit seinem eigenen Login, `cldk` meldet ihn nicht an:

```shell
docker login registry.cloudogu.com
```

`--user`/`--pass` und `CLDK_USER`/`CLDK_PASSWORD` gelten für alles, was `cldk` selbst tut: Ziele prüfen, Digests der gepushten Images lesen, `-i`-Images kopieren und das Chart pushen.
Den Builder wählen `--builder` oder `CLDK_BUILDER`, z.B. `podman`; `--platform`/`CLDK_PLATFORM`, `--no-cache`/`CLDK_NO_CACHE` und `--build-arg` wirken wie bei `cldk dogu dev-push`.

### Beispiel 1: Projekt-Release

```shell
cd nexus
cldk dogu release --dry-run --namespace testing
```

```
Dry run, nothing has been pushed.
Would build image nexus -> registry.cloudogu.com/testing/dogu/v3/images/nexus:3.96.4-1
Would build image sidecar -> registry.cloudogu.com/testing/dogu/v3/images/sidecar:1.2.0
Would release chart oci://registry.cloudogu.com/testing/dogu/v3/charts/nexus:1.2.3
```

```shell
cldk dogu release --namespace testing
```

```
Released image nexus -> registry.cloudogu.com/testing/dogu/v3/images/nexus:3.96.4-1@sha256:…
Released image sidecar -> registry.cloudogu.com/testing/dogu/v3/images/sidecar:1.2.0@sha256:…
Released chart oci://registry.cloudogu.com/testing/dogu/v3/charts/nexus:1.2.3@sha256:…
```

Das Chart bekommt die veröffentlichten Images in den Einträgen von `values.images` seiner `mirror-patch-tpl.yaml`: `nexus` über den Repository-Namen, `sidecar` über seinen `key` `nexusSaManager`.

### Beispiel 2: Projekt-Release mit einem Image aus der CI

Ein Image, das mit `-i` angegeben ist, wird kopiert statt gebaut, wenn sein Zielname ein Image der `cldk.yaml` ist; es behält den `key` aus der `cldk.yaml`.
Die übrigen Images der `cldk.yaml` werden gebaut:

```shell
cldk dogu release --namespace testing -i ci-registry.example.com/nexus/sidecar:1.2.0
```

```
Released image nexus -> registry.cloudogu.com/testing/dogu/v3/images/nexus:3.96.4-1@sha256:…
Released image ci-registry.example.com/nexus/sidecar:1.2.0 -> registry.cloudogu.com/testing/dogu/v3/images/sidecar:1.2.0@sha256:…
Released chart oci://registry.cloudogu.com/testing/dogu/v3/charts/nexus:1.2.3@sha256:…
```

Ein `-i`-Image ohne Gegenstück in der `cldk.yaml` wird zusätzlich veröffentlicht, wie beim Archiv-Release.
`--image-key` wirkt nur auf `-i`-Images und überschreibt für ein ersetzendes `-i`-Image den `key` aus der `cldk.yaml`; gebaute Images nehmen immer den `key` aus der `cldk.yaml`.
Sind alle Images per `-i` angegeben, wird kein Builder gebraucht.

### Bestehende Artefakte eines Projekt-Release

| Ziel existiert schon                | Verhalten                                                                   |
|-------------------------------------|-----------------------------------------------------------------------------|
| Release-Tag eines gebauten Images   | Abbruch vor dem ersten Build                                                |
| Image-Tag eines `-i`-Images         | wie beim Archiv-Release, siehe [Bestehende Artefakte](#bestehende-artefakte) |
| Chart-Version                       | Abbruch vor dem ersten Build                                                |

Ein gebautes Image wird nie übersprungen, weil sein Digest erst nach dem Build feststeht.
Mit `--force` werden bestehende Release-Tags und die Chart-Version bewusst überschrieben.
Scheitert ein Push, gibt `cldk` die bis dahin veröffentlichten Images aus; das Chart wird nicht veröffentlicht.

## Archiv-Release

### Ablauf

1. `cldk` liest Name und Version aus der `Chart.yaml` des gepackten Charts.
2. Jedes Image, das mit `-i` angegeben ist, wird in seiner Quell-Registry aufgelöst und nach `<reg>/<namespace>/dogu/v3/images/<name>:<tag>` kopiert.
3. Das Chart bekommt die neuen Image-Referenzen über seine `mirror-patch-tpl.yaml` und wird neu gepackt.
4. Das Chart wird nach `oci://<reg>/<namespace>/dogu/v3/charts/<name>:<version>` gepusht, nach allen Images.

Vor dem ersten Push prüft `cldk` alle Ziele.
Bereits veröffentlichte Artefakte werden nie still überschrieben, siehe [Bestehende Artefakte](#bestehende-artefakte).

### Voraussetzungen

#### Gepacktes Chart

Ein Archiv-Release nimmt das Chart als `.tgz`, wie `helm package` es erzeugt:

```shell
helm package k8s/helm -d target
# Successfully packaged chart and saved it to: target/nexus-1.2.3.tgz
```

Die `version` in `Chart.yaml` ist die veröffentlichte Chart-Version, z.B. `1.2.3`.
Sie ist unabhängig von der Version der Anwendung und den Tags ihrer Images, bei Nexus z.B. `3.96.4`.

#### Images in einer Registry

Ein Archiv-Release liest Images nur aus Registries, nie aus dem lokalen Docker-Daemon.
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

#### Zugangsdaten

Für die Ziel-Registry nutzt `cldk` die gespeicherten Zugangsdaten von `helm registry login` oder `docker login`:

```shell
docker login registry.cloudogu.com
```

Alternativ gelten `--user` und `--pass` bzw. `CLDK_USER` und `CLDK_PASSWORD`, z.B. in der CI oder in der `.env` des Repositorys.
Die Quell-Images werden immer mit den Zugangsdaten von `docker login` gelesen.
`cldk` speichert keine Zugangsdaten.

Die Zugangsdaten brauchen Schreibrecht auf den Ziel-Namespace, in den Beispielen `testing`.

### Beispiel 3: Chart ohne Images

Das Chart wird so veröffentlicht, wie es ist; seine Image-Referenzen zeigen weiter auf die Einträge aus `values.yaml`.

```shell
cldk dogu release --dry-run --namespace testing target/nexus-1.2.3.tgz
```

```
Dry run, nothing has been pushed.
Would release chart oci://registry.cloudogu.com/testing/dogu/v3/charts/nexus:1.2.3
```

```shell
cldk dogu release --namespace testing target/nexus-1.2.3.tgz
```

```
Released chart oci://registry.cloudogu.com/testing/dogu/v3/charts/nexus:1.2.3@sha256:…
```

`--namespace` ist Pflicht, damit nichts versehentlich in `official` landet.
`--reg` ist nicht nötig, `registry.cloudogu.com` ist der Default.

### Beispiel 4: Chart mit Image

Das Nexus-Image aus der CI-Registry wird zusammen mit dem Chart veröffentlicht:

```shell
cldk dogu release --dry-run --namespace testing \
  -i ci-registry.example.com/nexus:3.96.4 \
  target/nexus-1.2.3.tgz
```

```
Dry run, nothing has been pushed.
Would push image ci-registry.example.com/nexus:3.96.4 -> registry.cloudogu.com/testing/dogu/v3/images/nexus:3.96.4@sha256:…
Would release chart oci://registry.cloudogu.com/testing/dogu/v3/charts/nexus:1.2.3
```

```shell
cldk dogu release --namespace testing \
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

#### Name, Tag und Eintrag explizit setzen

Passen Name oder Tag der Quelle nicht, setzt `=<name>:<tag>` sie explizit:

```shell
cldk dogu release --namespace testing \
  -i ci-registry.example.com/dogus/nexus-build:42=nexus:3.96.4 \
  target/nexus-1.2.3.tgz
```

Passt kein oder mehr als ein Eintrag von `values.images` zum Namen, ordnet `--image-key <name>=<key>` das Image zu:

```shell
cldk dogu release --namespace testing \
  -i ci-registry.example.com/sa-sidecar:0.1.2=service-account-producer-sidecar:0.1.2 \
  --image-key service-account-producer-sidecar=nexusSaManager \
  target/nexus-1.2.3.tgz
```

### Bestehende Artefakte

`cldk` prüft alle Ziele vor dem ersten Push:

| Ziel existiert schon               | Verhalten                                                |
|------------------------------------|----------------------------------------------------------|
| Image-Tag mit demselben Digest     | wird übersprungen (`Skipped image … (already released)`) |
| Image-Tag mit einem anderen Digest | Abbruch, nichts wird gepusht                             |
| Chart-Version                      | Abbruch, nichts wird gepusht                             |

Ein abgebrochenes Release lässt sich deshalb einfach wiederholen: Schon kopierte Images werden übersprungen.

Mit `--force` werden Image-Tags und Chart-Version bewusst überschrieben, z.B. um ein Release in `testing` zu korrigieren:

```shell
cldk dogu release --force --namespace testing -i ci-registry.example.com/nexus:3.96.4 target/nexus-1.2.3.tgz
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
| Das Projekt-Release bricht mit „no release tag for the images …“ ab                           | `tag:` der Images in der `cldk.yaml` setzen oder `--tag <name>=<tag>` angeben.                                                                    |
| Der Push eines gebauten Images schlägt fehl                                                   | Der Builder pusht mit seinem eigenen Login: `<builder> login <registry>` ausführen, z.B. `docker login registry.cloudogu.com`.                    |
| `--tag` oder `--builder` wird mit einer `.tgz` abgelehnt                                      | Diese Flags gelten nur für das Projekt-Release; ein Archiv-Release kopiert Images mit `-i`.                                                       |
