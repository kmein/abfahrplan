# abfahrplan

Kompakte Abfahrtszeiten für eine Haltestelle – eine Seite Papier für die Wand,
im Stil der gedruckten BVG-Fahrpläne.

**[kmein.github.io/abfahrplan](https://kmein.github.io/abfahrplan/)**

## Warum

Man wohnt an einer Haltestelle und möchte wissen, wann der nächste Bus fährt,
ohne dafür eine App oder Google Maps zu öffnen. Früher konnte man bei der BVG
den Aushangfahrplan einer Linie an einer Haltestelle herunterladen; das geht
nicht mehr. Dieser Zettel enthält stattdessen alle Abfahrten der Haltestelle auf
einmal – alle Linien, beide Richtungen, Montag bis Sonntag.

## Website

Haltestelle suchen oder auf der Karte anklicken, Fahrplan als PDF herunterladen.

Das PDF entsteht im Browser: die Seite liefert den Typst-Compiler als
WebAssembly aus und setzt den Fahrplan beim Klick. Ohne JavaScript zeigt jede
Haltestellenseite ihre Abfahrten weiterhin als Tabelle.

## Kommandozeile

```bash
nix build .#abfahrplan
./result/bin/abfahrplan -s "Albrechtstr" -g GTFS.zip --pdf fahrplan.pdf
```

| Option | |
|---|---|
| `-s`, `--station` | Name der Haltestelle, als Teilzeichenkette |
| `-g`, `--gtfs` | GTFS-Zip-Datei |
| `-r`, `--route` | nur diese Linien, mehrfach angebbar |
| `--pdf` | Fahrplan zusätzlich als PDF schreiben |

Die ganze Website erzeugt `abfahrplan-generate`; siehe
[`.github/workflows/pages.yml`](.github/workflows/pages.yml), das sie täglich
baut und veröffentlicht.

## Wochentage

Ein GTFS-Dienst beschreibt seine Verkehrstage doppelt: als Wochentagsmuster in
`calendar.txt` und als einzelne Daten in `calendar_dates.txt`. Das Muster allein
genügt nicht – der VBB lässt es zunehmend leer und führt alle Tage einzeln auf,
und er zerlegt jeden Dienst in Abschnitte von wenigen Wochen.

Deshalb sammelt jede Abfahrt die Tage, an denen sie tatsächlich verkehrt. Ein
Wochentag kommt in den Fahrplan, wenn die Abfahrt an mindestens einem Viertel
der betreffenden Wochentage im Gültigkeitszeitraum fährt. Einzelne Sonder- und
Feiertagsfahrten bleiben so draußen, ein echter Zwei-Wochen-Takt bleibt drin.

Abfahrten, die nicht an allen Werktagen verkehren, tragen die Tage hochgestellt.
Alle Angaben ohne Gewähr.

## Daten

Fahrplandaten: [VBB Verkehrsverbund Berlin-Brandenburg
GmbH](https://daten.berlin.de/datensaetze/vbb-fahrplandaten-via-gtfs) (CC BY).
Karte: © OpenStreetMap-Mitwirkende, als
[PMTiles](https://protomaps.com/)-Ausschnitt. Schrift: Fira Sans, als freier
Ersatz für die hauseigene Transit der BVG; die Farben stammen aus deren
Druck-PDFs.

## Lizenz

AGPL-3.0
