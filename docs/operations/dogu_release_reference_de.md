# Referenz von `cldk dogu release`

`cldk dogu release` veröffentlicht ein Dogu, also sein Chart und seine Images, in einem Namespace einer Registry.
Eine Schritt-für-Schritt-Anleitung mit Beispielen für `registry.cloudogu.com` steht unter [Ein Dogu mit `cldk dogu release` veröffentlichen](releasing_a_dogu_de.md).

`dogu release` hat zwei Modi:

| Modus           | Aufruf                          | Images                                                                           |
|-----------------|---------------------------------|----------------------------------------------------------------------------------|
| Projekt-Release | `cldk dogu release`             | aus dem Projektverzeichnis mit ihren Release-Tags gebaut, oder kopiert (`-i`)    |
| Archiv-Release  | `cldk dogu release <chart.tgz>` | aus anderen Registries kopiert (`-i`)                                            |

## Projekt-Release

Ohne Argument veröffentlicht `dogu release` das Dogu im Projektverzeichnis (`--dir`, Standard das Arbeitsverzeichnis):

```shell
cldk dogu release
```

- Es liest die `cldk.yaml` (siehe [Projektkonfiguration](project_configuration_de.md#cldkyaml)) und das Chart-Verzeichnis.
- Jedes Image der `cldk.yaml` wird mit dem Builder gebaut und nach `<reg>/<namespace>/dogu/v3/images/<name>:<Release-Tag>` gepusht.
  - Der Release-Tag ist `tag:` des Images in der `cldk.yaml`, überschrieben mit `--tag <name>=<tag>` (wiederholbar).
  - Fehlt einem Image der Release-Tag, bricht das Release vor dem ersten Build ab und nennt alle Images ohne Tag.
- Das Chart wird über die `mirror-patch-tpl.yaml` mit dem `key` aus der `cldk.yaml` auf die veröffentlichten Images umgeschrieben, gelintet und mit der unveränderten `version` aus `Chart.yaml` gepusht.
- Der Namespace ist standardmäßig `doguNamespace` aus der `cldk.yaml`.
- Reihenfolge: alles prüfen, alle Images bauen, alle gebauten Images pushen, die `-i`-Images kopieren, das Chart pushen.
  - Ein fehlgeschlagener Build hinterlässt nichts in der Ziel-Registry.
  - Gebaute Images pusht der Builder mit seinem eigenen Login, z.B. `docker login registry.cloudogu.com`; `--user`/`--pass` gelten für alles andere.
- Ein bestehender Release-Tag oder eine bestehende Chart-Version ist ein Konflikt vor dem ersten Build; `--force` überschreibt beides.
  Gebaute Images werden nie übersprungen, weil ihr Digest vor dem Build unbekannt ist.
- `-i <image>` kopiert ein Image, statt es zu bauen, wenn sein Zielname ein Image der `cldk.yaml` ist; es behält den `key` aus der `cldk.yaml`, sofern `--image-key` keinen anderen zuordnet.
  `-i`-Images ohne Gegenstück werden zusätzlich veröffentlicht; sind nur `-i`-Images angegeben, wird kein Builder gebraucht.
- `--builder`, `--platform`, `--no-cache` und `--build-arg` wirken wie bei [`dogu dev-push`](dogu_dev_push_reference_de.md#images-bauen).
- `--dry-run` prüft die `cldk.yaml`, die Release-Tags, den Builder, den Lint, die `-i`-Quellen und alle Ziele und gibt den Plan aus, baut und pusht aber nichts:

  ```text
  Dry run, nothing has been pushed.
  Would build image nexus -> registry.cloudogu.com/official/dogu/v3/images/nexus:3.96.4-1
  Would release chart oci://registry.cloudogu.com/official/dogu/v3/charts/nexus:1.2.3
  ```

## Archiv-Release

Übergeben wird ein gepacktes Dogu-Helm-Chart, der Ziel-Namespace ist mit `--dogu-namespace <ns>` Pflicht:

```shell
cldk dogu release --dogu-namespace <ns> <chart.tgz>
```

`--tag`, `--builder`, `--platform`, `--no-cache` und `--build-arg` werden mit einem Chart-Archiv abgelehnt; ihre Umgebungsvariablen werden ignoriert.
Standardmäßig wird das Chart nach `registry.cloudogu.com` veröffentlicht.
Name und Version stammen aus der `Chart.yaml`, daraus ergibt sich dieses Ziel:

```text
oci://<reg>/<ns>/dogu/v3/charts/<name>:<version>
```

### `--image`, `-i`: Container-Images mitveröffentlichen

`--image <image>` (Kurzform: `-i`) nimmt ein Image mit auf.
Für jedes Image, das mit dem Chart veröffentlicht werden soll, wird das Flag wiederholt:

```shell
cldk dogu release --dogu-namespace <ns> \
  -i registry.cloudogu.com/official/nexus:3.96.4 \
  -i docker.io/library/postgres:14.18 \
  <chart.tgz>
```

Jedes Image wird mit demselben Digest hierhin kopiert:

```text
<reg>/<ns>/dogu/v3/images/<name>:<tag>
```

Der Zielname ist standardmäßig das letzte Pfadsegment des Quell-Repositorys, der Tag der Quelle bleibt erhalten.
Aus `docker.io/library/postgres:14.18` wird z.B. `<reg>/<ns>/dogu/v3/images/postgres:14.18`.
Mit `-i <image>=<name>:<tag>` lassen sich die Standardwerte überschreiben:

```shell
cldk dogu release --dogu-namespace <ns> \
  -i localhost:5001/local-dev/nexus/main:3.96.4=nexus:3.96.4 \
  <chart.tgz>
```

Für die Quellen der Images gilt:

- Jede Referenz enthält die Registry, z.B. `docker.io/library/postgres:14.18` statt `postgres:14.18`, damit nicht versehentlich von Docker Hub gezogen wird.
- Lokal gebaute Images müssen vorher in eine Registry gepusht werden.
  Images werden immer aus Registries gelesen, nie aus dem lokalen Docker-Daemon.

Für die Quell-Registries nutzt `cldk` die gespeicherten Zugangsdaten von `docker login`.

Werden Images mitveröffentlicht, muss das Chart eine `mirror-patch-tpl.yaml` enthalten.
Für jedes Image aktualisiert cldk den Eintrag unter `values.images`, dessen Repository-Name passt, wendet die Patches der Datei mit der neuen Image-Referenz an und packt das Chart neu.
Andere Image-Einträge bleiben unverändert.
Alle Images werden vor dem Chart gepusht.
Schlägt ein Push fehl, gibt cldk die bis dahin veröffentlichten Images zusammen mit dem Fehler aus.
Ein erneuter Release überspringt diese Images, weil ihre Tags schon mit demselben Digest existieren.

### `--image-key`: Images Chart-Werten zuordnen

Passt kein Eintrag oder passen mehrere, ordnet `--image-key <name>=<key>` das Image explizit zu.
Dabei ist `<name>` der Ziel-Name des Images und `<key>` der Eintrag unter `values.images` in der `mirror-patch-tpl.yaml`:

```shell
cldk dogu release --dogu-namespace <ns> \
  -i docker.io/library/postgres:14.18 \
  --image-key postgres=postgresql \
  <chart.tgz>
```

Für weitere Images wird `--image-key` wiederholt.

### `--dry-run`: Release vorab prüfen

Mit `--dry-run` wird der Release geprüft, ohne etwas zu pushen:

```shell
cldk dogu release --dry-run --dogu-namespace <ns> \
  -i docker.io/library/postgres:14.18 \
  <chart.tgz>
```

Ein Dry Run löst die Images auf, schreibt das Chart lokal um und prüft alle Ziele wie ein Release.
Er gibt für jedes Image Quelle, Ziel, Digest und geplante Aktion aus (push, skip, overwrite oder conflict), dazu das Ziel des Charts.
Fehler bei der Zuordnung und Konflikte lassen den Dry Run fehlschlagen.

### `--force`: Bestehende Releases überschreiben

`--force` überschreibt kollidierende Image-Tags und bereits veröffentlichte Chart-Versionen, z.B. beim Testen von Releases:

```shell
cldk dogu release --dogu-namespace <ns> --force <chart.tgz>
```

Alle Ziele werden vor dem ersten Push geprüft.
Bestehende Artefakte werden so behandelt:

| Bestehendes Ziel                   | Standardverhalten                                                       |
|------------------------------------|-------------------------------------------------------------------------|
| Image-Tag mit demselben Digest     | Image wird übersprungen; ein fehlgeschlagener Release ist wiederholbar. |
| Image-Tag mit einem anderen Digest | Abbruch des Releases.                                                   |
| Chart mit derselben Version        | Abbruch des Releases.                                                   |

### `--reg`: Ziel-Registry festlegen

`--reg <registry>` veröffentlicht in eine andere Registry als die Standard-Registry `registry.cloudogu.com`:

```shell
cldk dogu release --dogu-namespace <ns> --reg registry.example.com <chart.tgz>
```

### `--plain-http` und `--insecure`: Verbindung zur Registry

| Flag           | Verbindung zur Ziel-Registry                                                  |
|----------------|-------------------------------------------------------------------------------|
| `--plain-http` | HTTP ohne TLS.                                                                |
| `--insecure`   | TLS ohne Prüfung des Zertifikats, z.B. bei einem selbstsignierten Zertifikat. |

### `--user` und `--pass`: An der Ziel-Registry anmelden

Beide Flags zusammen setzen die Zugangsdaten für die Ziel-Registry:

```shell
cldk dogu release --dogu-namespace <ns> --user <user> --pass <password> <chart.tgz>
```

Die Zugangsdaten für die Ziel-Registry werden in dieser Reihenfolge gewählt:

1. `--user` und `--pass`, zusammen angegeben.
2. `CLDK_USER` und `CLDK_PASSWORD`, zusammen gesetzt, wenn keines der beiden Flags angegeben ist; sie dürfen in der [`.env` des Projektverzeichnisses](project_configuration_de.md#env) stehen.
3. Gespeicherte Zugangsdaten von `helm registry login` oder `docker login`.

cldk speichert Zugangsdaten nie.

## Optionen

Die Einstellungen der Ziel-Registry und der Builds lassen sich auch als Umgebungsvariablen angeben, ebenso in der [`.env` des Projektverzeichnisses](project_configuration_de.md#env); Flags haben Vorrang:

| Flag                                 | Umgebungsvariable           | Beschreibung                                                                                                                |
|--------------------------------------|-----------------------------|-----------------------------------------------------------------------------------------------------------------------------|
| `--reg <host[:port]>`                | `CLDK_REG`                  | Ziel-Registry, Standard `registry.cloudogu.com`                                                                             |
| `--dogu-namespace <ns>`              | `CLDK_DOGU_NAMESPACE`       | Dogu-Namespace in der Ziel-Registry; Projekt-Release: Standard `doguNamespace` aus der `cldk.yaml`, Archiv-Release: Pflicht |
| `--plain-http`                       | `CLDK_PLAIN_HTTP`           | Ziel-Registry ohne TLS                                                                                                      |
| `--insecure`                         | `CLDK_INSECURE`             | Ziel-Registry mit nicht vertrauenswürdigem (z.B. selbstsigniertem) Zertifikat                                               |
| `--user`/`--pass`                    | `CLDK_USER`/`CLDK_PASSWORD` | Zugangsdaten der Ziel-Registry, nur als Paar aus einer Quelle                                                               |
| `--builder <bin>`                    | `CLDK_BUILDER`              | Projekt-Release: Docker-kompatibles Build-CLI, Standard `docker`                                                            |
| `--platform <os/arch>`               | `CLDK_PLATFORM`             | Projekt-Release: Plattform, für die die Images gebaut werden                                                                |
| `--no-cache`                         | `CLDK_NO_CACHE`             | Projekt-Release: ohne den Cache des Builders bauen                                                                          |
| `--build-arg KEY[=VALUE]`            |                             | Projekt-Release: Build-Argument für alle Images, überschreibt `buildArgs` der `cldk.yaml`, wiederholbar                     |
| `--tag <name>=<tag>`                 |                             | Projekt-Release: Release-Tag eines Images der `cldk.yaml`, überschreibt dessen `tag:`, wiederholbar                         |
| `-i`, `--image <ref>[=<name>:<tag>]` |                             | Image kopieren, statt es zu bauen, oder zusätzlich, wiederholbar                                                            |
| `--image-key <name>=<key>`           |                             | Image aus `-i` dem Eintrag `values.images.<key>` in `mirror-patch-tpl.yaml` zuordnen, wiederholbar                          |
| `--force`                            |                             | bestehende Release-Tags, Image-Tags mit anderem Digest und Chart-Versionen überschreiben                                    |
| `--dry-run`                          |                             | alles prüfen und den Plan ausgeben, aber nichts bauen oder pushen                                                           |

`cldk -v` loggt jeden aufgelösten Wert mit seiner Quelle, das Passwort maskiert; prüfe die Ausgabe und die Referenzen von `--dry-run` vor einem Release, damit eine vergessene Zeile in der `.env` es nicht umleitet.
