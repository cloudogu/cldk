# Projektkonfiguration

`cldk` arbeitet auf einem Projektverzeichnis, meist dem Wurzelverzeichnis eines Dogu-Repositorys.
Das Projektverzeichnis ist das Arbeitsverzeichnis oder das mit `--dir`/`-d` angegebene.

## `cldk.yaml`

`cldk dogu dev-push` und `cldk dogu release` ohne Chart-Archiv brauchen eine `cldk.yaml` im Projektverzeichnis:

```yaml
doguNamespace: official    # Dogu-Namespace in der Registry, Pflicht
chart: k8s/helm            # Chart-Verzeichnis, Standard k8s/helm
images:                    # zu bauende Images, optional; ohne Einträge wird nichts gebaut
  nexus:                   # Image-Name, gepusht nach <reg>/<namespace>/dogu/v3/images/<name>
    context: .             # Build-Kontext, Standard .
    dockerfile: Dockerfile # Dockerfile, Standard <context>/Dockerfile
    key: nexus             # Schlüssel in values.images der mirror-patch-tpl.yaml, Standard der Eintrag mit dem Repository-Namen <name>
    target: ""             # Stage eines Multi-Stage-Builds, optional
    buildArgs:             # statische Build-Argumente, optional
      FOO: bar
    tag: 3.96.4-1          # Release-Tag von dogu release, optional; dev-push ignoriert ihn
```

Alle Pfade sind relativ zum Projektverzeichnis; `nexus: {}` ist ein gültiger Image-Eintrag mit allen Standardwerten.

Unbekannte Felder in der `cldk.yaml` brechen den Befehl ab, damit Tippfehler nicht stillschweigend ignoriert werden.

## `.env`

- Jeder Befehl lädt zuerst die `.env` des Projektverzeichnisses in seine Umgebung, z.B. `CLDK_USER`/`CLDK_PASSWORD` für `release`.
- Variablen, die schon in der Umgebung gesetzt sind, auch leer, haben Vorrang vor der `.env`; Flags haben Vorrang vor beiden.
- Eine fehlende `.env` ist kein Problem, eine `.env`, die sich nicht parsen lässt, bricht den Befehl ab.
- Das Debug-Logging mit `-v`, z.B. `cldk -v dogu dev-push`, zeigt die Namen der Variablen aus der `.env`, nie ihre Werte; `release` und `dev-push` loggen die aufgelösten Werte ihrer eigenen Einstellungen mit ihrer Quelle, nie ein Passwort.
