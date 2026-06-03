// pipeline-documents.ts — Generated demo documents for the pipeline demo.
// 10 detailed cases (DB + S3) and 100 short cases (mass demo).

export interface PipelineDocument {
    fall_nr:   string;
    kategorie: string;
    titel:     string;
    inhalt:    string;
    quelle:    'db' | 's3';
    s3_key?:   string;
}

// ── 10 detailed cases (Fall 1 + Fall 2: DB and S3) ──────────────────────────

export const DETAILED_DOCUMENTS: PipelineDocument[] = [
  {
    fall_nr: 'SCH-2024-0001', kategorie: 'Einbruch', quelle: 'db',
    titel: 'Einbruch Elektronikmarkt Stuttgart',
    inhalt: `Schadensfall SCH-2024-0001 — Einbruch
Datum: 14. Januar 2024, 02:17 Uhr
Ort: Elektronikmarkt MediaPlus, Königstrasse 45, 70173 Stuttgart

Sachverhalt:
In der Nacht vom 13. auf den 14. Januar 2024 drangen unbekannte Täter durch
ein aufgehebeltes Kellerfenster in den Elektronikmarkt ein. Die Alarmanlage
wurde durch Manipulation des Sicherungskastens deaktiviert. Entwendet wurden
47 Laptops (Gesamtwert ca. 38.400 EUR), 23 Smartphones (ca. 18.200 EUR) und
Bargeld aus der Hauptkasse in Höhe von 2.340 EUR.

Besonderheiten:
Der Täter oder die Täter kannten offensichtlich die Position des Sicherungskastens
und die Lagerstruktur. Kein Mitarbeiter war zum Tatzeitpunkt anwesend.
Kameraaufnahmen zeigen drei vermummte Personen, eine davon mit auffälliger
roter Jacke. Fluchtwagen war ein weißer Sprinter ohne Kennzeichen.

Schadenshöhe: 58.940 EUR
Versicherungssumme beantragt: 58.940 EUR
Bearbeiter: Ingrid Hoffmann`,
  },
  {
    fall_nr: 'SCH-2024-0002', kategorie: 'Wasserschaden', quelle: 'db',
    titel: 'Rohrbruch Wohnhaus München-Schwabing',
    inhalt: `Schadensfall SCH-2024-0002 — Wasserschaden
Datum: 3. Februar 2024
Ort: Mehrfamilienhaus, Leopoldstrasse 112, 80802 München

Sachverhalt:
Am 3. Februar 2024 platzte im 3. Obergeschoss ein Heizungsrohr infolge
eines Frostschadens. Das austretende Wasser durchdrang die Decke und
beschädigte die darunterliegenden Wohnungen im 2. und 1. Obergeschoss.
Der Schaden wurde erst am Morgen gegen 07:30 Uhr durch einen Mieter entdeckt.

Betroffene Wohneinheiten:
- Whg. 3.02 (Eigentümer): Parkett total, Tapeten, Elektrik Teilschaden
- Whg. 2.02: Laminat, Mobiliar, feuchte Wände
- Whg. 1.02: Feuchtigkeitsschaden Decke, Schimmelgefahr

Gutachter: Dipl.-Ing. Klaus Berger, Gutachten vom 10.02.2024
Schadenshöhe: 34.700 EUR
Notunterbringung Mieter: 1.200 EUR
Gesamtschaden: 35.900 EUR`,
  },
  {
    fall_nr: 'SCH-2024-0003', kategorie: 'KFZ-Unfall', quelle: 'db',
    titel: 'Auffahrunfall A8 Karlsruhe — Verdacht auf Vortäuschung',
    inhalt: `Schadensfall SCH-2024-0003 — KFZ-Unfall / Betrugsverdacht
Datum: 22. März 2024, 07:42 Uhr
Ort: A8 Richtung München, km 297, Karlsruhe

Sachverhalt:
Herr Thomas Grundmann (VN) meldete einen Auffahrunfall auf der A8.
Laut Aussage fuhr ihm Frau Petra Voss auf. Schadensersatz beantragt:
Heckschaden 4.200 EUR, Mietwagenkosten 840 EUR, Schmerzensgeld 1.500 EUR.

Auffälligkeiten:
- Unfallgegner Frau Voss war bereits an drei ähnlichen Unfällen beteiligt
  (SCH-2023-0412, SCH-2022-0318, SCH-2021-0901) mit demselben Anwalt.
- Alle drei Unfälle ereigneten sich auf Autobahnen am frühen Morgen.
- Kein unabhängiger Zeuge vorhanden.
- Schadensbilder stimmen nicht mit geschildertem Unfallhergang überein
  (Gutachter: Dr. Maria Schlegel, 01.04.2024).
- VN und Unfallgegnerin wohnen im selben Ortsteil.

Empfehlung Sachbearbeiter: Weitergabe an Sonderermittlung Betrug.
Status: Zahlung vorläufig zurückgestellt.`,
  },
  {
    fall_nr: 'SCH-2024-0004', kategorie: 'Brand', quelle: 'db',
    titel: 'Küchenbrand Einfamilienhaus Freiburg',
    inhalt: `Schadensfall SCH-2024-0004 — Brand
Datum: 8. April 2024, 19:23 Uhr
Ort: Einfamilienhaus, Rieselfeldallee 33, 79111 Freiburg

Sachverhalt:
Brand in der Küche durch unbeaufsichtigtes Frittierfett. Die Flammen
erfassten die Einbauküche vollständig und griffen auf den angrenzenden
Wohnbereich über. Feuerwehreinsatz ab 19:31 Uhr (8 Einsatzkräfte).
Brand konnte um 20:15 Uhr unter Kontrolle gebracht werden.

Schäden:
- Einbauküche: Totalschaden (Neupreis 18.400 EUR, Zeitwert 11.200 EUR)
- Wohnzimmer: Rußschaden, Möbel, Boden: ca. 9.800 EUR
- Strukturschäden Decke und Wand: 6.200 EUR
- Elektrische Anlage Teilbereich: 3.100 EUR
- Brandmeldeanlage ausgelöst: funktionsfähig

Familie vorübergehend im Hotel untergebracht (14 Nächte à 145 EUR).
Gesamtschaden inkl. Unterbringung: 32.330 EUR`,
  },
  {
    fall_nr: 'SCH-2024-0005', kategorie: 'Einbruch', quelle: 's3',
    s3_key: 'schaeden/2024/SCH-2024-0005.txt',
    titel: 'Einbruch Zahnarztpraxis Hamburg — Betrugsverdacht',
    inhalt: `Schadensfall SCH-2024-0005 — Einbruch / Betrugsverdacht
Datum: gemeldet 19. Mai 2024
Ort: Zahnarztpraxis Dr. Hans Weiler, Mönckebergstrasse 7, 20095 Hamburg

Sachverhalt laut VN:
In der Nacht vom 18. auf den 19. Mai 2024 sollen unbekannte Täter in die
Praxis eingebrochen sein. Entwendet: 3 Laptops, 1 Röntgengerät (12.400 EUR),
medizinische Instrumente (4.800 EUR), Bargeld 1.200 EUR.

Widersprüche bei Prüfung:
- Kein Einbruchspuren an Türen oder Fenstern festgestellt (Polizei).
- Alarmanlage war laut Protokoll nicht aktiviert (obwohl VN regelmäßige
  Aktivierung versicherte).
- Das angeblich entwendete Röntgengerät ist laut Hersteller-Datenbank
  seit 2021 als defekt abgemeldet und hätte keinen Marktwert.
- Kreditkartenabrechnungen zeigen Dr. Weiler am Abend vor dem Einbruch
  in einem Casino mit Transaktionen von 3.200 EUR.

Status: Anzeige gegen VN wegen Verdacht auf Versicherungsbetrug erstattet.
Schadenszahlung abgelehnt.`,
  },
  {
    fall_nr: 'SCH-2024-0006', kategorie: 'Wasserschaden', quelle: 's3',
    s3_key: 'schaeden/2024/SCH-2024-0006.txt',
    titel: 'Überschwemmung Kellergeschoss Düsseldorf',
    inhalt: `Schadensfall SCH-2024-0006 — Elementarschaden / Überschwemmung
Datum: 21. Juni 2024
Ort: Mehrfamilienhaus, Grafenberger Allee 88, 40237 Düsseldorf

Sachverhalt:
Nach Starkregen (62 mm in 3 Stunden laut DWD) trat Regenwasser durch
den Lichtschacht in den Kellergang ein. Kellerboxen von 8 Parteien betroffen.
Gesamte Kellernutzfläche (ca. 180 m²) stand unter 40 cm Wasser.

Einzelschäden (Zusammenfassung Gutachter):
- Heizungsanlage (Baujahr 2019): Kompletttausch notwendig, 14.200 EUR
- Waschmaschinen/Trockner 3 Parteien: 4.100 EUR
- Lagerware/Möbel 8 Parteien: 8.900 EUR gesamt
- Sanierungskosten (Trocknung, Desinfektion): 11.300 EUR

Gesamtschaden: 38.500 EUR
Elementarschadenversicherung greift: Eigenanteil 500 EUR
Auszahlungsbetrag: 38.000 EUR`,
  },
  {
    fall_nr: 'SCH-2024-0007', kategorie: 'KFZ-Unfall', quelle: 'db',
    titel: 'Parkplatzschaden Einkaufszentrum Hannover',
    inhalt: `Schadensfall SCH-2024-0007 — KFZ-Unfall Parkplatz
Datum: 5. Juli 2024, ca. 14:30 Uhr
Ort: Parkhaus Ernst-August-Galerie, Hannover

Sachverhalt:
Beim Ausparken stieß Frau Sabine Krüger mit ihrem VW Passat gegen
den ordnungsgemäß geparkten BMW 320i von Herrn Martin Becker.
Fremdschaden am BMW: Delle und Kratzer Fahrerseite, Anbauteile.

Dokumentation:
- Fotos vom Unfallort vorhanden
- Parkhaus-Kamera bestätigt Hergang
- Polizei nicht vor Ort, Europäischer Unfallbericht ausgefüllt

Schadenskalkulation:
- Fremdschaden BMW (Gutachten DEKRA): 3.870 EUR
- Wertminderung: 350 EUR
- Mietwagen 4 Tage: 480 EUR
- Sachverständigenkosten: 650 EUR
Gesamtregulierung: 5.350 EUR
Selbstverschulden Frau Krüger: 100%`,
  },
  {
    fall_nr: 'SCH-2024-0008', kategorie: 'Brand', quelle: 's3',
    s3_key: 'schaeden/2024/SCH-2024-0008.txt',
    titel: 'Lagerhallenbrand Logistikunternehmen Leipzig',
    inhalt: `Schadensfall SCH-2024-0008 — Großbrand Gewerbe
Datum: 12. August 2024, 03:45 Uhr
Ort: Logistikzentrum FastShip GmbH, Industriegebiet Leipzig-Nord

Sachverhalt:
Brand in einer Lagerhalle (ca. 2.400 m²) ausgebrochen, Ursache laut
Brandursachenermittlung: technischer Defekt an Ladestation für
Elektrostapler. Feuerwehr 3 Züge, Einsatzdauer 6 Stunden.

Schäden:
- Gebäudesubstanz (Totalverlust Halle B): 840.000 EUR
- Lagerware Eigenbestand: 220.000 EUR
- Lagerware Drittparteien (Haftpflicht): 180.000 EUR
- Betriebsunterbrechung (geschätzt 3 Monate): 450.000 EUR
- Aufräum- und Entsorgungskosten: 95.000 EUR

Gesamtschaden: ca. 1.785.000 EUR
Rückversicherung ab 500.000 EUR: Swiss Re
Erstzahlung Abschlag: 300.000 EUR genehmigt`,
  },
  {
    fall_nr: 'SCH-2024-0009', kategorie: 'Einbruch', quelle: 'db',
    titel: 'Juwelierdiebstahl Köln Innenstadt',
    inhalt: `Schadensfall SCH-2024-0009 — Einbruch / Diebstahl
Datum: 3. September 2024, 01:12 Uhr
Ort: Juwelier Goldstein, Schildergasse 54, 50667 Köln

Sachverhalt:
Mittels eines gestohlenen Fahrzeugs wurde die Schaufensterscheibe
(ESG 12mm) gerammt. Täter betraten das Geschäft und entwendeten
die ausgestellten Exponate aus drei Vitrinen binnen ca. 4 Minuten.
Alarm ausgelöst, Polizei vor Ort nach 6 Minuten, Täter geflüchtet.

Entwendete Waren (Inventarliste):
- 23 Ringe (Brillanten, Platin/Gold): 67.400 EUR
- 14 Armbanduhren (Luxusmarken): 89.200 EUR
- 8 Halsketten und Anhänger: 22.100 EUR
- Bargeld Tageseinnahmen: 3.400 EUR

Sachschaden:
- Schaufensterscheibe und Rahmen: 8.900 EUR
- Vitrineneinrichtung: 4.200 EUR

Gesamtschaden: 195.200 EUR
Entwendete Waren versichert bis 250.000 EUR. Auszahlung nach Inventarprüfung.`,
  },
  {
    fall_nr: 'SCH-2024-0010', kategorie: 'KFZ-Unfall', quelle: 's3',
    s3_key: 'schaeden/2024/SCH-2024-0010.txt',
    titel: 'Geisterfahrer-Unfall A3 Frankfurt — Betrugsverdacht',
    inhalt: `Schadensfall SCH-2024-0010 — KFZ-Unfall / Schwerer Betrugsverdacht
Datum: gemeldet 15. Oktober 2024
Ort: A3, Höhe Frankfurter Kreuz

Sachverhalt laut VN:
Herr Ralf Schuster meldete Totalschaden seines BMW M5 (Neupreis 112.000 EUR,
Zeitwert laut VN: 78.000 EUR) durch Kollision mit Geisterfahrer.
Unfallgegner angeblich geflüchtet, kein Zeuge.

Kritische Prüfpunkte:
- BMW M5 war laut CARFAX-Daten seit 4 Monaten mit Motorschaden
  (Kolbenfresser) in verschiedenen Werkstätten, kein wirtschaftlicher
  Reparaturauftrag erteilt.
- Fahrzeug-Ortung GPS: Kein Signal für 72 Stunden vor gemeldetem Unfall.
- VN hat laufende Privatinsolvenz seit März 2024.
- Vorschaden 2022 (SCH-2022-0774): identisches Schadensbild, anderer VN.
- Unfallaufnahme Polizei: keine Spurenlage die zu Geisterfahrtszenario passt.
- Fahrzeugident-Nummer weicht um eine Stelle von Fahrzeugschein ab.

Status: Strafanzeige erstattet, Fahrzeug sichergestellt.
Zahlung abgelehnt.`,
  },
];

// ── 100 short cases for mass demo ───────────────────────────────────────────

const KATEGORIEN = ['Einbruch', 'Wasserschaden', 'Brand', 'KFZ-Unfall', 'Glasbruch'] as const;
const STAEDTE    = ['Berlin', 'Hamburg', 'München', 'Köln', 'Frankfurt', 'Stuttgart',
                    'Düsseldorf', 'Leipzig', 'Dortmund', 'Essen', 'Bremen', 'Dresden'];

const EINBRUCH_TEXTE = [
  'Kellereinbruch, Werkzeug entwendet, Hebelspuren an Tür.',
  'Büroeinbruch über Fenster, Laptops und Bargeld gestohlen.',
  'Fahrzeug aufgebrochen, Navi und Lenkrad entwendet.',
  'Gartengeräteentnahme aus Schuppen, Schloss aufgeflext.',
  'Wohnungseinbruch tagsüber, Schmuck und Elektronik weg.',
  'Ladeneinbruch durch Dach, Kasse leer, Waren beschädigt.',
  'Einbruch Lager, Elektrowerkzeug ca. 8.000 EUR gestohlen.',
  'Schuleinbruch Wochenende, IT-Ausstattung entwendet.',
];
const WASSER_TEXTE = [
  'Spülmaschine defekt, Küche unter Wasser, Boden zerstört.',
  'Frostschaden Außenwasserleitung, Rohrbruch Keller.',
  'Geschirrspüler-Zulaufschlauch geplatzt, drei Etagen betroffen.',
  'Starkregen, Keller vollgelaufen, Heizung beschädigt.',
  'Waschmaschine Leck, Laminat aufgequollen, Schimmelgefahr.',
  'Dachrinne verstopft, Wasser ins Mauerwerk eingedrungen.',
  'Hauswasserwerk defekt, Schaden im Hauswirtschaftsraum.',
];
const BRAND_TEXTE = [
  'Adventskranz Zimmerbrand, Sofa und Vorhänge verbrannt.',
  'Technischer Defekt Kühlschrank, Küche beschädigt.',
  'Elektroverteiler Überhitzung, Dachstuhl angekokelt.',
  'Gartengrill zu nah an Holzterasse, Brandflecken.',
  'Kerze umgefallen, Tisch und Teppich verbrannt.',
  'Kurzschluss E-Roller Garage, Fahrzeug und Garagen Schaden.',
  'Kaminbrand, Rußrückstau in Wohnzimmer.',
];
const KFZ_TEXTE = [
  'Parkplatzrempler, Delle und Kratzer, Zeuge vorhanden.',
  'Vorfahrtsverletzung Kreuzung, Seitenaufprall, leichter Sachschaden.',
  'Wildunfall B27, Reh, Frontschaden ca. 3.200 EUR.',
  'Auffahrunfall Stau, minimaler Stoßstangenschaden.',
  'Hagelschaden während Gewitter, 40 Dellen, Glasschaden.',
  'Diebstahl Außenspiegel in der Innenstadt.',
  'Unfall enger Parkplatz Einkaufszentrum, Spiegel abgerissen.',
];
const GLAS_TEXTE = [
  'Steinwurf Vandalen, Schaufensterscheibe zerbrochen.',
  'Ball gegen Fensterscheibe, Wohnzimmer, Kind spielte im Garten.',
  'Glasbruch Terrassentür durch Windböe, Sturm.',
  'Autoscheibe eingeschlagen, Einbruchsversuch abgebrochen.',
  'Dachziegelfall auf Wintergartenverglasung.',
];

function pickText(kat: string, i: number): string {
  const idx = i % 7;
  switch (kat) {
    case 'Einbruch':    return EINBRUCH_TEXTE[i % EINBRUCH_TEXTE.length]!;
    case 'Wasserschaden': return WASSER_TEXTE[i % WASSER_TEXTE.length]!;
    case 'Brand':       return BRAND_TEXTE[i % BRAND_TEXTE.length]!;
    case 'KFZ-Unfall':  return KFZ_TEXTE[i % KFZ_TEXTE.length]!;
    default:            return GLAS_TEXTE[i % GLAS_TEXTE.length]!;
  }
}

export const MASS_DOCUMENTS: PipelineDocument[] = Array.from({ length: 100 }, (_, i) => {
  const kat   = KATEGORIEN[i % KATEGORIEN.length]!;
  const nr    = String(i + 1).padStart(4, '0');
  const stadt = STAEDTE[i % STAEDTE.length]!;
  const schaden = 800 + (i * 137 + 400) % 12000;
  // Every 7th case gets a suspicious note for the fraud detection demo.
  const betrug = i % 7 === 0
    ? ' Kein unabhängiger Zeuge. VN war bereits in ähnlichem Schadensfall involviert.'
    : '';
  return {
    fall_nr:   `SCH-2025-${nr}`,
    kategorie: kat,
    titel:     `${kat} ${stadt} (${nr})`,
    inhalt:    `Schadensfall SCH-2025-${nr} — ${kat}\nOrt: ${stadt}\nSchadenshöhe: ${schaden.toLocaleString('de-DE')} EUR\n\n${pickText(kat, i)}${betrug}`,
    quelle:    'db',
  };
});
