#!/usr/bin/env python3
"""Templates for four German clinical sublanguages.

Each entry is a list of paragraph templates. A document is assembled by
picking 1 header, N body paragraphs, and 1 footer, then filling slots
of the form `{SLOT_NAME}` with values from generators.py.

Slot names match GraSCCo PHI conventions so the existing label_map.yaml
mapping works without changes.

Slots can be optional: `{?SLOT}` is filled with 50% probability,
otherwise erased (along with surrounding whitespace context). This keeps
the generated text from looking template-rigid.
"""

from __future__ import annotations

# -- ED triage ------------------------------------------------------------

ED_TRIAGE_HEADERS = [
    """{LOCATION_HOSPITAL}
Notaufnahme — Triage-Notiz

Patient: {NAME_PATIENT}, geb. {DATE_BIRTH}
Aufnahme: {DATE}, {TIME}
Patienten-ID: {ID}
Triage-Stufe: {TRIAGE_LEVEL}
""",
    """Notaufnahme-Erstkontakt — {LOCATION_HOSPITAL}

Vorgestellt: {NAME_PATIENT} ({AGE} Jahre, geb. {DATE_BIRTH})
Datum/Uhrzeit: {DATE} {TIME}
Begleitperson: {NAME_RELATIVE}
ID: {ID}
""",
]

ED_TRIAGE_BODY = [
    "Hauptbeschwerde: akute Thoraxschmerzen seit {DATE} morgens, ausstrahlend in den linken Arm.",
    "Vorgestellt durch Rettungsdienst, übernommen von der Leitstelle um {TIME}.",
    "Anamnese laut Patient: Sturz vom Fahrrad, Aufschlag mit Helm; kurze Bewusstlosigkeit.",
    "Vitalzeichen: RR 145/90 mmHg, HF 102/min, SpO2 96 % unter Raumluft, Temperatur 37,8 °C.",
    "Allergien: keine bekannten Medikamentenallergien.",
    "Dauermedikation laut Patient: Ramipril 5 mg, ASS 100 mg, Metformin 850 mg.",
    "Untersuchung: wacher Patient, allseits orientiert, kein Meningismus.",
    "Begleitung durch Angehörige ({NAME_RELATIVE}) anwesend.",
    "Hausarzt vorinformiert: {NAME_DOCTOR}, Praxis in {LOCATION_CITY}.",
    "Versicherungsstatus: AOK, Mitglieds-ID nicht vorliegend.",
    "Triage-Entscheidung durch {NAME_DOCTOR}: stationäre Aufnahme erforderlich.",
    "Befundgespräch geplant mit {NAME_DOCTOR} ab {TIME}.",
    "Patientin lebt allein in {LOCATION_STREET}, {LOCATION_ZIP} {LOCATION_CITY}.",
    "Kontaktnummer Angehörige: {CONTACT_PHONE}.",
]

ED_TRIAGE_FOOTERS = [
    """
Untersucher: {NAME_DOCTOR}
Pflege: {NAME_DOCTOR}
Rückfragen: {CONTACT_PHONE} oder {CONTACT_EMAIL}
""",
    """
Triage abgeschlossen am {DATE} um {TIME}.
Verantwortlich: {NAME_DOCTOR}
Kontakt Notaufnahme: {CONTACT_PHONE}
""",
]

# -- OP report ------------------------------------------------------------

OP_HEADERS = [
    """{LOCATION_HOSPITAL}
Klinik für Allgemein- und Viszeralchirurgie

OP-Bericht

Patient: {NAME_PATIENT}, geb. {DATE_BIRTH}, Pat.-ID {ID}
Operationsdatum: {DATE}
Operateur: {NAME_DOCTOR}
1. Assistenz: {NAME_DOCTOR}
Anästhesie: {NAME_DOCTOR}
""",
    """OP-Bericht — {LOCATION_HOSPITAL}

Operationsdatum: {DATE}
Beginn: {TIME}
Patient: {NAME_PATIENT} ({AGE} Jahre)
ID: {ID}
Operateur: {NAME_DOCTOR}
Assistenz: {NAME_DOCTOR}
""",
]

OP_BODY = [
    "Indikation: symptomatische Cholezystolithiasis mit rezidivierenden Koliken.",
    "Diagnose präoperativ: Inguinalhernie rechts, schmerzhaft seit dem {DATE}.",
    "Vorbefunde aus dem Hause {LOCATION_HOSPITAL} liegen vor.",
    "Aufklärung: ausführliche schriftliche Aufklärung am Vortag durch {NAME_DOCTOR}.",
    "Lagerung: Rückenlage, beide Arme angelegt.",
    "Hautdesinfektion und steriles Abdecken in üblicher Weise.",
    "Schnitt: medianer Oberbauchschnitt, ca. 12 cm Länge.",
    "Eingehen ins Abdomen schichtgerecht ohne Komplikationen.",
    "Inspektion: keine Aszitesbildung, Leberoberfläche glatt.",
    "Präparation der Gallenblase mit Darstellung des Ductus cysticus und der A. cystica.",
    "Clipverschluss und Durchtrennung beider Strukturen.",
    "Bergung der Gallenblase im Bergebeutel.",
    "Histologie: Gewebe an Pathologie {LOCATION_HOSPITAL} versandt (Auftrag {ID}).",
    "Schichtweiser Wundverschluss, Hautnaht intracutan mit Monocryl 3-0.",
    "OP-Ende: {TIME}; Patient stabil zur Aufwachstation verlegt.",
    "Postoperative Anordnungen besprochen mit Stationsarzt {NAME_DOCTOR}.",
]

OP_FOOTERS = [
    """
{LOCATION_CITY}, den {DATE}

___________________
{NAME_DOCTOR}, Operateur
""",
    """
Operateur: {NAME_DOCTOR}
Diktat: {NAME_DOCTOR} am {DATE}
Bericht-Nr.: {ID}
""",
]

# -- Radiology ------------------------------------------------------------

RADIO_HEADERS = [
    """{LOCATION_HOSPITAL}
Institut für Diagnostische und Interventionelle Radiologie

Befund

Patient: {NAME_PATIENT}, geb. {DATE_BIRTH}
Patienten-ID: {ID}
Untersuchung: CT Thorax mit Kontrastmittel
Untersuchungsdatum: {DATE}, {TIME}
Zuweisender Arzt: {NAME_DOCTOR}
""",
    """Radiologischer Befund — {LOCATION_HOSPITAL}

Patient: {NAME_PATIENT}
Geboren am: {DATE_BIRTH}
ID: {ID}
Modalität: MRT Schädel nativ + KM
Datum: {DATE}
Befundender Arzt: {NAME_DOCTOR}
""",
]

RADIO_BODY = [
    "Klinische Angaben des zuweisenden Arztes {NAME_DOCTOR}: V.a. zentralen Lungenembolie.",
    "Voruntersuchung vom {DATE} aus dem Hause {LOCATION_HOSPITAL} liegt vor, Vergleich möglich.",
    "Technik: Multidetektor-CT, 80 ml Iomeron 350 i.v., Schichtdicke 1 mm.",
    "Befund Lunge: keine raumfordernden Prozesse, keine Pleuraergüsse beidseits.",
    "Mediastinum: unauffällige Konfiguration, keine pathologisch vergrößerten Lymphknoten.",
    "Herz: regelrechte Größe, keine perikardialen Ergüsse.",
    "Skelett: degenerative Veränderungen der BWS, im Übrigen altersentsprechend.",
    "Schädel: regelrechte Mark-Rinden-Differenzierung, keine intrakraniellen Blutungen.",
    "Beurteilung: kein Anhalt für eine zentrale Lungenembolie.",
    "Empfehlung: Verlaufskontrolle in 6 Monaten oder klinisch bei Bedarf.",
    "Befundbesprechung mit {NAME_DOCTOR} am {DATE} um {TIME} erfolgt.",
]

RADIO_FOOTERS = [
    """
{LOCATION_CITY}, {DATE}

{NAME_DOCTOR}
Facharzt für Radiologie

Rückfragen: {CONTACT_PHONE}, {CONTACT_EMAIL}
""",
    """
Befunddiktat: {NAME_DOCTOR}
Schreibkraft: {NAME_DOCTOR}
Bericht erstellt am {DATE} um {TIME}
""",
]

# -- Rehab discharge ------------------------------------------------------

REHAB_HEADERS = [
    """{LOCATION_HOSPITAL}
Abteilung für orthopädische Rehabilitation

Entlassungsbericht

Patient: {NAME_PATIENT}, geb. {DATE_BIRTH}
Beruf: {PROFESSION}
Wohnhaft: {LOCATION_STREET}, {LOCATION_ZIP} {LOCATION_CITY}
Patient-ID: {ID}
Aufnahme: {DATE}
Entlassung: {DATE}
Stationsarzt: {NAME_DOCTOR}
""",
    """Reha-Entlassungsbericht

Klinik: {LOCATION_HOSPITAL}
Patient: {NAME_PATIENT} ({AGE} Jahre)
ID-Nr.: {ID}
Hauptdiagnose: Z.n. Hüft-TEP rechts
Aufnahme: {DATE} — Entlassung: {DATE}
Behandelnder Arzt: {NAME_DOCTOR}
""",
]

REHAB_BODY = [
    "Aufnahmegrund: Anschlussheilbehandlung nach operativer Versorgung im Hause {LOCATION_HOSPITAL}.",
    "Anamnese: Der/die {AGE}-jährige Patient*in war bis zum {DATE} in der Klinik {LOCATION_HOSPITAL} stationär.",
    "Berufliche Anamnese: bis zur Erkrankung tätig als {PROFESSION}.",
    "Familienanamnese: Vater verstorben an Myokardinfarkt. Ehepartner: {NAME_RELATIVE}.",
    "Sozialanamnese: lebt mit Ehepartner in {LOCATION_CITY}, Treppen ohne Aufzug.",
    "Vorbefunde: OP-Bericht von {NAME_DOCTOR} vom {DATE} liegt vor.",
    "Therapieplan: Physiotherapie zweimal täglich, Ergotherapie nach Bedarf.",
    "Verlauf: kontinuierliche Mobilisationsfortschritte unter Anleitung durch {NAME_DOCTOR}.",
    "Belastung: schmerzadaptierte Vollbelastung ab dem {DATE} möglich.",
    "Soziale Beratung: Termin mit Sozialdienst der Klinik am {DATE} um {TIME}.",
    "Empfehlung: ambulante Weiterbehandlung beim Hausarzt {NAME_DOCTOR} in {LOCATION_CITY}.",
    "Medikation bei Entlassung: ASS 100 mg morgens, Pantoprazol 20 mg morgens.",
    "Hilfsmittel: Unterarmgehstützen für 4 weitere Wochen.",
    "Kontaktdaten Patient für Nachsorge: {CONTACT_PHONE}, {CONTACT_EMAIL}.",
]

REHAB_FOOTERS = [
    """
Mit kollegialen Grüßen

{NAME_DOCTOR}
Leitender Oberarzt
{LOCATION_HOSPITAL}
Tel.: {CONTACT_PHONE}
""",
    """
{LOCATION_CITY}, den {DATE}

{NAME_DOCTOR}              {NAME_DOCTOR}
Stationsarzt              Chefarzt

Rückfragen: {CONTACT_PHONE} / {CONTACT_EMAIL}
""",
]

SUBLANGUAGES = {
    "ed_triage": (ED_TRIAGE_HEADERS, ED_TRIAGE_BODY, ED_TRIAGE_FOOTERS),
    "op_report": (OP_HEADERS, OP_BODY, OP_FOOTERS),
    "radiology": (RADIO_HEADERS, RADIO_BODY, RADIO_FOOTERS),
    "rehab_discharge": (REHAB_HEADERS, REHAB_BODY, REHAB_FOOTERS),
}
