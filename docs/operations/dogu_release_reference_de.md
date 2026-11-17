# Referenz von `cldk dogu release`

`cldk dogu release` veröffentlicht ein gepacktes Chart eines v3-Dogus und optional seine Images in einer Registry.
Eine Schritt-für-Schritt-Anleitung mit Beispielen für `registry.cloudogu.com` steht unter [Ein Dogu mit `cldk dogu release` veröffentlichen](releasing_a_dogu_de.md).

## Chart und Ziel

Übergeben wird ein gepacktes Dogu-Helm-Chart, der Ziel-Namespace ist mit `--dogu-namespace <ns>` Pflicht:

```shell
cldk dogu release --dogu-namespace <ns> <chart.tgz>
```

Standardmäßig wird das Chart nach `registry.cloudogu.com` veröffentlicht.
Name und Version stammen aus der `Chart.yaml`, daraus ergibt sich dieses Ziel:

```text
oci://<reg>/<ns>/dogu/v3/charts/<name>:<version>
```

## `--image`, `-i`: Container-Images mitveröffentlichen

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

## `--image-key`: Images Chart-Werten zuordnen

Passt kein Eintrag oder passen mehrere, ordnet `--image-key <name>=<key>` das Image explizit zu.
Dabei ist `<name>` der Ziel-Name des Images und `<key>` der Eintrag unter `values.images` in der `mirror-patch-tpl.yaml`:

```shell
cldk dogu release --dogu-namespace <ns> \
  -i docker.io/library/postgres:14.18 \
  --image-key postgres=postgresql \
  <chart.tgz>
```

Für weitere Images wird `--image-key` wiederholt.

## `--dry-run`: Release vorab prüfen

Mit `--dry-run` wird der Release geprüft, ohne etwas zu pushen:

```shell
cldk dogu release --dry-run --dogu-namespace <ns> \
  -i docker.io/library/postgres:14.18 \
  <chart.tgz>
```

Ein Dry Run löst die Images auf, schreibt das Chart lokal um und prüft alle Ziele wie ein Release.
Er gibt für jedes Image Quelle, Ziel, Digest und geplante Aktion aus (push, skip, overwrite oder conflict), dazu das Ziel des Charts.
Fehler bei der Zuordnung und Konflikte lassen den Dry Run fehlschlagen.

## `--force`: Bestehende Releases überschreiben

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

## `--reg`: Ziel-Registry festlegen

`--reg <registry>` veröffentlicht in eine andere Registry als die Standard-Registry `registry.cloudogu.com`:

```shell
cldk dogu release --dogu-namespace <ns> --reg registry.example.com <chart.tgz>
```

## `--plain-http` und `--insecure`: Verbindung zur Registry

| Flag           | Verbindung zur Ziel-Registry                                                  |
|----------------|-------------------------------------------------------------------------------|
| `--plain-http` | HTTP ohne TLS.                                                                |
| `--insecure`   | TLS ohne Prüfung des Zertifikats, z.B. bei einem selbstsignierten Zertifikat. |

## `--user` und `--pass`: An der Ziel-Registry anmelden

Beide Flags zusammen setzen die Zugangsdaten für die Ziel-Registry:

```shell
cldk dogu release --dogu-namespace <ns> --user <user> --pass <password> <chart.tgz>
```

Die Zugangsdaten für die Ziel-Registry werden in dieser Reihenfolge gewählt:

1. `--user` und `--pass`, zusammen angegeben.
2. `CLDK_USER` und `CLDK_PASSWORD`, zusammen gesetzt, wenn keines der beiden Flags angegeben ist; sie dürfen in der [`.env` des Projektverzeichnisses](project_configuration_de.md#env) stehen.
3. Gespeicherte Zugangsdaten von `helm registry login` oder `docker login`.

cldk speichert Zugangsdaten nie.
