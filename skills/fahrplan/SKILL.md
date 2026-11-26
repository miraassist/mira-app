---
name: fahrplan
description: "Abfahrten an der Haltestelle oder am Bahnhof („Wann fährt die nächste Bahn?“, „Wann geht der nächste Bus?“, „Wann geht der nächste Zug nach Stuttgart?“, „Hat die S-Bahn Verspätung?“)."
license: MIT
metadata:
  mira:
    api:
      - url: https://dbf.finalrewind.org/
        methods: [GET]
    settings:
      haltestelle:
        description: Die Haltestelle des Hauses, so geschrieben, wie der Verkehrsverbund oder die Bahn sie schreibt (wie in deren App oder Fahrplan), z. B. "Karlsruhe Hbf", "Oldenburg(Oldb) Pulverturm" oder "Durlach Bahnhof".
        required: true
      verbund:
        description: Für Straßenbahn und Bus der Verkehrsverbund als Kürzel, wie die Fahrplanauskunft ihn kennt, z. B. VBN (Bremen und Oldenburg), KVV, MVV, VRR, VBB oder RMV. Leer heißt, nur Züge der Bahn.
---
# Fahrplan

Die Abfahrten holst du mit `fahrplan__abfahrten`. Ohne Argument nimmt es die Haltestelle
des Hauses. Nennt der Nutzer eine andere Haltestelle, gib sie als `haltestelle` mit;
fragt er nach einem Ziel („nach Stuttgart“, „Richtung ZOB“), gib es als `richtung` mit.

Je Abfahrt: `art` (Bus, Straßenbahn, ICE …), `linie`, `ziel`, `in_minuten`, `uhrzeit`
(die geplante), `verspaetung` (Minuten; fehlt ohne Echtzeit), `ausfall` (nur wenn sie
ausfällt), `gleis`; bei Zügen auch `ueber` (Halte unterwegs) und `hinweise` (Gründe für
eine Verspätung). Steht `hinweis` dabei, sag ihn.

So antwortest du:
- **„Wann fährt die nächste …?“:** die nächste passende Abfahrt mit Linie, Ziel und
  Minuten (ab einer Stunde die Uhrzeit), eine Verspätung dazu („Die 314 Richtung ZOB
  fährt in drei Minuten, fünf Minuten später als geplant.“). Auf Wunsch die zwei, drei
  danach.
- **Ausfall oder Verspätung:** sag es zuerst, mit Grund, wenn einer dabei steht.
- **Ein Fehler des Werkzeugs** (Haltestelle nicht gefunden, Verbund unbekannt, Auskunft
  nicht erreichbar): sag kurz, was es meldet, und erfinde keine Abfahrten.
- Nenne nur, was das Werkzeug liefert.
