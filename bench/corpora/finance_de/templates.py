#!/usr/bin/env python3
"""Templates for five German financial document types.

Document assembly mirrors synth_clinical: each doc has a header pool, a
body pool (paragraphs sampled without replacement), and a footer pool.
Slots are `{SLOT_NAME}` markers filled by GENERATORS in generators.py.

Three doc types (Kontoauszug, Depot-Auszug) need dynamic row repetition
which a flat template can't express; those use the ROW_TEMPLATES dict
below — generate.py emits N rows from a row-template, each filled with
fresh slot values, and inserts the result at `{ROWS_<KEY>}` markers.

Non-PHI placeholders (filled with realistic values but not gold-tagged):

  * {AMOUNT}     — euro amount in German format "1.234,56 EUR"
  * {AMOUNT_NEG} — negative amount "-1.234,56 EUR" (debits)
  * {QTY}        — share count, e.g. "120"
  * {REF}        — reference text drawn from REF_PURPOSES
  * {SOURCE_OF_FUNDS} — paragraph drawn from SOURCE_OF_FUNDS
  * {SECURITY_NAME}   — non-PHI half of an (ISIN, name) pair (filled
                        alongside an ISIN slot via gen_security)
"""

from __future__ import annotations


# -- Kontoauszug (account statement) --------------------------------------

KONTOAUSZUG_HEADERS = [
    """{LOCATION_BANK}
Filiale {LOCATION_CITY}

Kontoauszug Nr. {CUSTOMER_NUMBER}/{DATE}

Kontoinhaber: {NAME_PATIENT}
Anschrift: {LOCATION_STREET}, {LOCATION_ZIP} {LOCATION_CITY}
IBAN: {IBAN}
BIC: {BIC}
Auszugszeitraum: {DATE} bis {DATE}
""",
    """{LOCATION_BANK} — Online-Banking

Kontoauszug

Für: {NAME_PATIENT_ANREDE}
Kundennummer: {CUSTOMER_NUMBER}
IBAN: {IBAN}
Erstellt am: {DATE}
""",
]

KONTOAUSZUG_BODY = [
    "Saldovortrag zum {DATE}: 4.582,17 EUR.",
    "Auf Rückfragen wenden Sie sich bitte an Ihren Berater {NAME_RELATIVE} unter {CONTACT_PHONE}.",
    "Für Online-Banking-Support erreichen Sie uns unter {CONTACT_EMAIL}.",
    "Die nachfolgenden Umsätze beziehen sich auf das Hauptkonto.",
    "Bitte prüfen Sie die Buchungen innerhalb von sechs Wochen.",
    "Beanstandungen richten Sie schriftlich an Ihre Filiale in {LOCATION_CITY}.",
    "Konto wird gemeinschaftlich geführt mit {NAME_RELATIVE} ({DATE_BIRTH}).",
    "Der nächste Auszug erscheint am {DATE}.",
]

KONTOAUSZUG_FOOTERS = [
    """
{ROWS_TRANSACTIONS}

Endsaldo zum {DATE}: 3.917,42 EUR.

Mit freundlichen Grüßen
{LOCATION_BANK}
""",
    """
{ROWS_TRANSACTIONS}

Erstellt durch Kundenberater {NAME_RELATIVE}.
Kontakt: {CONTACT_PHONE}, {CONTACT_EMAIL}
""",
]

# One row in the transactions list — repeated N times by generate.py.
KONTOAUSZUG_TX_ROW = "{DATE}  {NAME_RELATIVE} ({IBAN})  {REF}  {AMOUNT_NEG}"


# -- Überweisungsauftrag (transfer order) ---------------------------------

UEBERWEISUNG_HEADERS = [
    """{LOCATION_BANK}

SEPA-Überweisungsauftrag

Auftraggeber:
  {NAME_PATIENT}
  {LOCATION_STREET}, {LOCATION_ZIP} {LOCATION_CITY}
  IBAN: {IBAN}
  BIC: {BIC}
""",
    """Überweisungsauftrag — Online-Banking

Datum: {DATE}
Auftraggeber: {NAME_PATIENT_ANREDE}
Kundennummer: {CUSTOMER_NUMBER}
""",
]

UEBERWEISUNG_BODY = [
    "Empfänger: {NAME_RELATIVE}",
    "IBAN Empfänger: {IBAN}",
    "BIC Empfänger: {BIC}",
    "Betrag: {AMOUNT}",
    "Verwendungszweck: {REF}",
    "Ausführungsdatum: {DATE}",
    "Bestätigungs-Telefon: {CONTACT_PHONE}",
    "Bestätigungs-E-Mail: {CONTACT_EMAIL}",
    "Hinweis: Beauftragt durch {NAME_PATIENT_ANREDE}, wohnhaft in {LOCATION_CITY}.",
    "Auftrag vorbereitet durch Mitarbeiter {NAME_RELATIVE} der Filiale {LOCATION_CITY}.",
    "Kontonummer (alt): {CUSTOMER_NUMBER}.",
]

UEBERWEISUNG_FOOTERS = [
    """
Ort, Datum: {LOCATION_CITY}, {DATE}

________________________
Unterschrift Auftraggeber
{NAME_PATIENT}
""",
    """
TAN-Bestätigung erfolgte am {DATE} via SMS an {CONTACT_PHONE}.
Auftrag freigegeben durch {NAME_PATIENT_ANREDE}.
""",
]


# -- Kreditantrag (loan application) --------------------------------------

KREDITANTRAG_HEADERS = [
    """{LOCATION_BANK}
Abteilung Privatkredit

Antrag auf Ratenkredit

Antragsteller: {NAME_PATIENT}
Geburtsdatum: {DATE_BIRTH}
Steuerliche Identifikationsnummer: {STEUER_ID}
Anschrift: {LOCATION_STREET}, {LOCATION_ZIP} {LOCATION_CITY}
Telefon: {CONTACT_PHONE}
E-Mail: {CONTACT_EMAIL}
""",
    """Kreditantrag — {LOCATION_BANK}

Antragsdatum: {DATE}
Antragsteller: {NAME_PATIENT_ANREDE} ({AGE} Jahre)
Steuer-ID: {STEUER_ID}
Wohnsitz: {LOCATION_STREET}, {LOCATION_ZIP} {LOCATION_CITY}
""",
]

KREDITANTRAG_BODY = [
    "Beantragte Kreditsumme: {AMOUNT}.",
    "Laufzeit: 60 Monate, Sollzinssatz 4,9 % p.a.",
    "Verwendungszweck: Anschaffung eines Gebrauchtfahrzeugs.",
    "Verwendungszweck: Renovierung der Eigentumswohnung in {LOCATION_CITY}.",
    "Verwendungszweck: Umschuldung bestehender Verbindlichkeiten bei der {LOCATION_BANK}.",
    "Arbeitgeber: {LOCATION_EMPLOYER}, beschäftigt seit {DATE}.",
    "Beruf: {PROFESSION}, Nettoeinkommen monatlich 3.412,00 EUR.",
    "Familienstand: verheiratet, ein Kind (geb. {DATE_BIRTH}).",
    "Vorhandenes Vermögen: Tagesgeldkonto bei der {LOCATION_BANK}, IBAN {IBAN}.",
    "Bestehende Kreditkarte: Kundennummer {CUSTOMER_NUMBER}, Limit 2.000,00 EUR.",
    "Auszahlungswunsch: Überweisung auf das Konto IBAN {IBAN} (BIC {BIC}).",
    "Schufa-Auskunft liegt bei, ausgestellt am {DATE}.",
    "Erreichbarkeit für Rückfragen: {CONTACT_PHONE} (mobil) oder {CONTACT_EMAIL}.",
    "Referenz: Vermittlung über Berater {NAME_RELATIVE}.",
    "Steuerlich veranlagt beim Finanzamt {LOCATION_CITY}.",
]

KREDITANTRAG_FOOTERS = [
    """
{LOCATION_CITY}, den {DATE}

____________________________
{NAME_PATIENT}
Antragsteller

____________________________
Bearbeiter: {NAME_RELATIVE}, {LOCATION_BANK}
""",
    """
Antrag angenommen am {DATE}.
Sachbearbeiter: {NAME_RELATIVE}
Filialkontakt: {CONTACT_PHONE} — {CONTACT_EMAIL}
""",
]


# -- Depot-Auszug (securities portfolio statement) ------------------------

DEPOT_HEADERS = [
    """{LOCATION_BROKER}

Depot-Auszug zum Stichtag {DATE}

Depotinhaber: {NAME_PATIENT}
Anschrift: {LOCATION_STREET}, {LOCATION_ZIP} {LOCATION_CITY}
Depotnummer: {CUSTOMER_NUMBER}
Verrechnungskonto IBAN: {IBAN}
""",
    """Wertpapierdepot — {LOCATION_BROKER}

Stichtag: {DATE}
Inhaber: {NAME_PATIENT_ANREDE}, geb. {DATE_BIRTH}
Depotnummer: {CUSTOMER_NUMBER}
Verrechnungskonto: {IBAN} ({BIC})
""",
]

DEPOT_BODY = [
    "Verwahrart: Girosammelverwahrung bei Clearstream Banking Frankfurt.",
    "Bewertung: Schlusskurse XETRA am Stichtag.",
    "Bestände wurden korrekt mit den Daten der WM Datenservice abgeglichen.",
    "Sämtliche Erträge wurden auf das Verrechnungskonto IBAN {IBAN} gutgeschrieben.",
    "Kapitalertragsteuer wurde unter Steuer-ID {STEUER_ID} einbehalten.",
    "Freistellungsauftrag liegt vor und ist gültig bis {DATE}.",
    "Depotumsätze des Zeitraums siehe Anlage 2.",
    "Beratungsbedarf? Wenden Sie sich an Ihren Betreuer {NAME_RELATIVE} unter {CONTACT_PHONE}.",
]

DEPOT_FOOTERS = [
    """
Bestandsübersicht zum {DATE}:

{ROWS_HOLDINGS}

Gesamtdepotwert: 87.432,15 EUR.

Mit freundlichen Grüßen
{LOCATION_BROKER}
""",
    """
{ROWS_HOLDINGS}

Erstellt durch {LOCATION_BROKER}.
Rückfragen: {CONTACT_EMAIL}, {CONTACT_PHONE}.
""",
]

# One holding row.
DEPOT_HOLDING_ROW = "  {ISIN}  {SECURITY_NAME}  Stück {QTY}  Kurswert {AMOUNT}"


# -- KYC-Anfrage (KYC questionnaire) --------------------------------------

KYC_HEADERS = [
    """{LOCATION_BANK}
Geldwäscheprävention / Know Your Customer

KYC-Fragebogen

Eingangsdatum: {DATE}
Antragsteller: {NAME_PATIENT}
Geburtsdatum: {DATE_BIRTH}
Geburtsort: {LOCATION_CITY}
Staatsangehörigkeit: deutsch
""",
    """KYC-Selbstauskunft — {LOCATION_BANK}

Bearbeitungsdatum: {DATE}
Person: {NAME_PATIENT_ANREDE} ({AGE} Jahre)
Geburtsort: {LOCATION_CITY}
""",
]

KYC_BODY = [
    "Aktuelle Wohnanschrift: {LOCATION_STREET}, {LOCATION_ZIP} {LOCATION_CITY}.",
    "Steuerliche Identifikationsnummer: {STEUER_ID}.",
    "Telefonisch erreichbar unter: {CONTACT_PHONE}.",
    "E-Mail-Adresse für Korrespondenz: {CONTACT_EMAIL}.",
    "Beruf / Tätigkeit: {PROFESSION}.",
    "Aktueller Arbeitgeber: {LOCATION_EMPLOYER}, Beschäftigt seit {DATE}.",
    "Vorheriger Arbeitgeber: {LOCATION_EMPLOYER}.",
    "Steuerlicher Wohnsitz: Deutschland.",
    "Konto bei der Hausbank: {LOCATION_BANK}, IBAN {IBAN}.",
    "Politisch exponierte Person (PEP): nein.",
    "Mittelherkunft: {SOURCE_OF_FUNDS}",
    "Voraussichtliches monatliches Einkommen: {AMOUNT}.",
    "Geplanter Eingang aus Erbschaft im Jahr {DATE}.",
    "Bisherige Geschäftsbeziehung besteht seit {DATE}.",
    "Referenzperson bei der Bank: Berater {NAME_RELATIVE}.",
]

KYC_FOOTERS = [
    """
Ich versichere, dass die obigen Angaben der Wahrheit entsprechen.

{LOCATION_CITY}, den {DATE}

____________________________
{NAME_PATIENT}
""",
    """
KYC-Prüfung erfasst am {DATE}.
Sachbearbeiter: {NAME_RELATIVE}
Rückfragen: {CONTACT_PHONE} / {CONTACT_EMAIL}
""",
]


# -- Dispatch tables ------------------------------------------------------

# (header_pool, body_pool, footer_pool, row_template_or_None,
#  row_marker_or_None, (row_count_lo, row_count_hi))
DOCTYPES = {
    "kontoauszug": (
        KONTOAUSZUG_HEADERS, KONTOAUSZUG_BODY, KONTOAUSZUG_FOOTERS,
        KONTOAUSZUG_TX_ROW, "ROWS_TRANSACTIONS", (4, 8),
    ),
    "ueberweisung": (
        UEBERWEISUNG_HEADERS, UEBERWEISUNG_BODY, UEBERWEISUNG_FOOTERS,
        None, None, (0, 0),
    ),
    "kreditantrag": (
        KREDITANTRAG_HEADERS, KREDITANTRAG_BODY, KREDITANTRAG_FOOTERS,
        None, None, (0, 0),
    ),
    "depot_auszug": (
        DEPOT_HEADERS, DEPOT_BODY, DEPOT_FOOTERS,
        DEPOT_HOLDING_ROW, "ROWS_HOLDINGS", (3, 7),
    ),
    "kyc_anfrage": (
        KYC_HEADERS, KYC_BODY, KYC_FOOTERS,
        None, None, (0, 0),
    ),
}

# Body pick size range per doctype. Tuned so each doc emits 5-15 PHI spans
# total — most templates contain 2-4 slots, the header carries 5-9 PHI,
# so body adds 0-6 more.
BODY_PICK_RANGES = {
    "kontoauszug": (3, 6),
    "ueberweisung": (5, 9),
    "kreditantrag": (5, 9),
    "depot_auszug": (3, 6),
    "kyc_anfrage": (6, 11),
}
