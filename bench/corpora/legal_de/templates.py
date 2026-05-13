#!/usr/bin/env python3
"""Templates for five German legal document types.

Each entry is a list of paragraph templates. A document is assembled by
picking 1 header, N body paragraphs, and 1 footer, then filling slots
of the form `{SLOT_NAME}` with values from generators.py.

Document types:

  klageschrift     — statement of claim
  beschluss        — court order / ruling
  vergleich        — settlement agreement
  vollmacht        — power of attorney
  anwaltsschreiben — lawyer's letter / demand

Slot names map to entries in generators.GENERATORS. Slot names not in
GENERATORS are filled with non-PHI placeholder values via
NON_PHI_FILLS in generate.py (e.g. {AMOUNT}, {DEMAND}). The Streitwert
/ settlement amount is deliberately non-PHI — emitted in text but
without a gold span — matching the spec.
"""

from __future__ import annotations

# -- Klageschrift (statement of claim) --------------------------------------

KLAGE_HEADERS = [
    """{COURT}

Aktenzeichen: {AKTENZEICHEN}

In dem Rechtsstreit

{NAME_PARTY}, wohnhaft in {CITY},
- Kläger -

gegen

{NAME_PARTY}, wohnhaft in {CITY},
- Beklagter -

wegen Forderung aus Kaufvertrag,
Streitwert: {AMOUNT} EUR

erhebe ich namens und im Auftrag des Klägers Klage und beantrage:
""",
    """An das {COURT}

— Geschäftsstelle der Zivilkammer —

Az.: {AKTENZEICHEN}

Klage

der Firma {COMPANY},
- Klägerin -

gegen

{NAME_PARTY}, wohnhaft {STREET}, {ZIP} {CITY},
- Beklagten -

Streitwert: vorläufig {AMOUNT} EUR.
""",
]

KLAGE_BODY = [
    "Der Kläger und der Beklagte schlossen am {DATE} einen Kaufvertrag über die Lieferung von Maschinenteilen.",
    "Der vereinbarte Kaufpreis in Höhe von {AMOUNT} EUR war binnen 30 Tagen nach Lieferung zur Zahlung fällig.",
    "Die Lieferung erfolgte ordnungsgemäß am {DATE}, eine Abnahme wurde vom Beklagten nicht beanstandet.",
    "Trotz Mahnung vom {DATE} mit Fristsetzung bis zum {DATE_DEADLINE} leistete der Beklagte keine Zahlung.",
    "Der Beklagte ist mit Schreiben vom {DATE} unter Fristsetzung bis {DATE_DEADLINE} zur Leistung aufgefordert worden.",
    "Beweis: Zeugnis des Herrn {NAME_WITNESS}, ladungsfähige Anschrift {STREET}, {ZIP} {CITY}.",
    "Beweis: Vorlage der Rechnung Nr. {AKTENZEICHEN} vom {DATE}, Anlage K1.",
    "Die örtliche Zuständigkeit des angerufenen {COURT} ergibt sich aus § 29 ZPO.",
    "Der Beklagte hat sich gegenüber dem Klägervertreter {NAME_ATTORNEY} mit Schreiben vom {DATE} geweigert, die Forderung anzuerkennen.",
    "Der Kläger ist von Beruf {PROFESSION} und auf die ausstehende Zahlung wirtschaftlich angewiesen.",
    "Der Kläger ist {AGE} Jahre alt und wohnt seit {DATE} unter der angegebenen Adresse.",
    "Vorgerichtlich wurde die Sache durch {NAME_ATTORNEY} der {KANZLEI} bearbeitet; deren Kostennote ist als Anlage K3 beigefügt.",
    "Der Beklagte handelt nach unserer Auffassung schuldhaft und ist zur vollständigen Erfüllung verpflichtet.",
]

KLAGE_FOOTERS = [
    """
{CITY}, den {DATE}

___________________
{NAME_ATTORNEY}
Rechtsanwalt
{KANZLEI}
""",
    """
{CITY}, {DATE}

Mit freundlichen kollegialen Grüßen

{NAME_ATTORNEY}
— Prozessbevollmächtigter des Klägers —
Rückfragen: {PHONE} oder {EMAIL}
""",
]

# -- Beschluss (court order) ------------------------------------------------

BESCHLUSS_HEADERS = [
    """{COURT}

Aktenzeichen: {AKTENZEICHEN}

Beschluss

In dem Rechtsstreit
{NAME_PARTY} ./. {NAME_PARTY}

hat das Gericht — Zivilkammer — durch den Vorsitzenden Richter
{NAME_JUDGE} am {DATE} beschlossen:
""",
    """{COURT}

— Geschäftsstelle —

Az.: {AKTENZEICHEN}

Beschluss vom {DATE}

In der Familiensache

{NAME_PARTY}, geb. {DATE_BIRTH},
- Antragsteller -

und

{NAME_PARTY}, geb. {DATE_BIRTH},
- Antragsgegnerin -

erlässt das Gericht durch {NAME_JUDGE} folgenden Beschluss:
""",
]

BESCHLUSS_BODY = [
    "Der Antrag des Klägers vom {DATE} wird zurückgewiesen.",
    "Die Kosten des Verfahrens trägt der Beklagte.",
    "Der Streitwert wird auf {AMOUNT} EUR festgesetzt.",
    "Die sofortige Beschwerde ist binnen zwei Wochen nach Zustellung dieses Beschlusses beim {COURT} einzulegen.",
    "Die Berufung wird zugelassen, soweit sie die Höhe des zugesprochenen Betrags von {AMOUNT} EUR betrifft.",
    "Auf den Beweisbeschluss vom {DATE} wird verwiesen; der benannte Zeuge {NAME_WITNESS} ist zu laden.",
    "Die Akten werden zur weiteren Entscheidung an das {COURT} abgegeben.",
    "Der Vorsitzende Richter {NAME_JUDGE} hat in der mündlichen Verhandlung vom {DATE} die Sach- und Rechtslage erörtert.",
    "Die Klagepartei wird vertreten durch {NAME_ATTORNEY} der {KANZLEI}.",
    "Eine Ausfertigung dieses Beschlusses wird beiden Parteien sowie deren Bevollmächtigten zugestellt.",
    "Die Frist zur Stellungnahme läuft bis zum {DATE_DEADLINE}.",
    "Bei Säumnis ist mit weiteren Maßnahmen nach §§ 330 ff. ZPO zu rechnen.",
]

BESCHLUSS_FOOTERS = [
    """
{CITY}, den {DATE}

{NAME_JUDGE}
Vorsitzender Richter
""",
    """
Ausgefertigt am {DATE} durch die Geschäftsstelle des {COURT}.

{NAME_JUDGE}
""",
]

# -- Vergleich (settlement agreement) ---------------------------------------

VERGLEICH_HEADERS = [
    """Vergleich

zum Aktenzeichen {AKTENZEICHEN} des {COURT}

Zwischen

{NAME_PARTY}, geboren am {DATE_BIRTH}, wohnhaft in {CITY},
- Partei zu 1) -

und

{NAME_PARTY}, geboren am {DATE_BIRTH}, wohnhaft in {CITY},
- Partei zu 2) -

wird folgender Vergleich geschlossen:
""",
    """Vergleichsvereinbarung — Az. {AKTENZEICHEN}

Geschlossen zwischen

der {COMPANY},
- Partei zu 1) -

und

{NAME_PARTY}, geb. {DATE_BIRTH}, {STREET}, {ZIP} {CITY},
- Partei zu 2) -

Anwaltliche Vertretung: {NAME_ATTORNEY} bzw. {NAME_ATTORNEY}.
""",
]

VERGLEICH_BODY = [
    "Die Parteien sind sich einig, dass mit Erfüllung dieses Vergleichs sämtliche Ansprüche aus dem Rechtsstreit erledigt sind.",
    "Die Partei zu 1) zahlt an die Partei zu 2) einen Betrag von {AMOUNT} EUR bis spätestens {DATE_DEADLINE}.",
    "Die Zahlung erfolgt auf das Konto des Bevollmächtigten der Partei zu 2), {NAME_ATTORNEY}, IBAN {IBAN}.",
    "Die Kosten des Rechtsstreits werden gegeneinander aufgehoben.",
    "Außergerichtliche Kosten trägt jede Partei selbst.",
    "Dieser Vergleich wird am {DATE} in {CITY} geschlossen.",
    "Beide Parteien verzichten auf das Recht zum Widerruf gemäß § 779 BGB.",
    "Die Partei zu 1) verpflichtet sich, die {COMPANY} aus jeglicher Haftung freizustellen.",
    "Beide Bevollmächtigten — {NAME_ATTORNEY} und {NAME_ATTORNEY} — bestätigen die Zustimmung ihrer Mandantschaften.",
    "Ein Zeuge dieses Vergleichs ist {NAME_WITNESS}, wohnhaft {STREET}, {ZIP} {CITY}.",
    "Die Geschäftsstelle des {COURT} ist über den Vergleich unverzüglich in Kenntnis zu setzen.",
]

VERGLEICH_FOOTERS = [
    """
{CITY}, den {DATE}


_______________________            _______________________
{NAME_PARTY}                     {NAME_PARTY}
Partei zu 1)                       Partei zu 2)


_______________________            _______________________
{NAME_ATTORNEY}                  {NAME_ATTORNEY}
Bevollmächtigter                  Bevollmächtigter
""",
    """
Geschlossen zu {CITY} am {DATE}.

Für die Partei zu 1):                Für die Partei zu 2):
{NAME_ATTORNEY}                    {NAME_ATTORNEY}
{KANZLEI}                            {KANZLEI}
""",
]

# -- Vollmacht (power of attorney) ------------------------------------------

VOLLMACHT_HEADERS = [
    """Vollmacht

Hiermit erteile ich,

{NAME_PARTY}, geboren am {DATE_BIRTH}, wohnhaft in {CITY},
Personalausweis-Nr.: {PERSONALAUSWEIS},
Steuer-Identifikationsnummer: {STEUER_ID},

— im Folgenden Vollmachtgeber —

Vollmacht an

{NAME_PARTY}, von Beruf {PROFESSION}, wohnhaft in {CITY},

— im Folgenden Bevollmächtigter —
""",
    """Generalvollmacht

Vollmachtgeberin: {NAME_PARTY}, geb. {DATE_BIRTH},
ansässig {STREET}, {ZIP} {CITY},
Personalausweisnummer {PERSONALAUSWEIS},
Steuer-ID {STEUER_ID}.

Bevollmächtigte/r: {NAME_PARTY}, Beruf {PROFESSION}.
""",
]

VOLLMACHT_BODY = [
    "Der Bevollmächtigte ist befugt, mich in allen vermögensrechtlichen Angelegenheiten zu vertreten.",
    "Die Vollmacht umfasst insbesondere die Verwaltung von Konten bei in- und ausländischen Kreditinstituten.",
    "Der Bevollmächtigte ist von den Beschränkungen des § 181 BGB ausdrücklich befreit.",
    "Die Vollmacht erstreckt sich auch auf Erklärungen gegenüber dem {COURT}.",
    "Die Vollmacht gilt ab dem {DATE} und ist bis auf Widerruf gültig.",
    "Im Rahmen der Vollmacht darf der Bevollmächtigte auch Rechtsanwalt {NAME_ATTORNEY} der {KANZLEI} mandatieren.",
    "Eine Untervollmacht ist nur mit ausdrücklicher schriftlicher Zustimmung des Vollmachtgebers zulässig.",
    "Der Vollmachtgeber wurde durch {NAME_NOTARY} im Kammerbezirk {KAMMERBEZIRK} über Tragweite und Inhalt belehrt.",
    "Die Beurkundung erfolgte am {DATE} vor dem Notar {NAME_NOTARY}, Anwaltsnummer {LAWYER_REG}.",
    "Der Vollmachtgeber ist zum Zeitpunkt der Beurkundung {AGE} Jahre alt und nach Auffassung des Notars geschäftsfähig.",
    "Bei Rückfragen ist der Bevollmächtigte unter {PHONE} oder {EMAIL} erreichbar.",
]

VOLLMACHT_FOOTERS = [
    """
{CITY}, den {DATE}


_______________________
{NAME_PARTY}
— Vollmachtgeber —


Beglaubigt durch:
{NAME_NOTARY}
Kammerbezirk {KAMMERBEZIRK}
""",
    """
{CITY}, {DATE}

Unterschrift Vollmachtgeber: ________________________
({NAME_PARTY})

Notarielle Beglaubigung durch {NAME_NOTARY}, RA-NR {LAWYER_REG}.
""",
]

# -- Anwaltsschreiben (lawyer's letter) -------------------------------------

ANWALT_HEADERS = [
    """{KANZLEI}
Tel.: {PHONE}  •  E-Mail: {EMAIL}

An
{NAME_PARTY}, {CITY}


{CITY}, den {DATE}

Unser Zeichen: {AKTENZEICHEN}

Mandant: {NAME_PARTY}, geb. {DATE_BIRTH}

Sehr geehrte Damen und Herren,
""",
    """{KANZLEI} — {NAME_ATTORNEY}
{STREET}, {ZIP} {CITY}
T {PHONE}

{COMPANY}
zu Händen der Geschäftsleitung

Datum: {DATE}
Az.: {AKTENZEICHEN}

In Sachen unserer Mandantschaft, {NAME_PARTY}, geb. {DATE_BIRTH},

Sehr geehrte Damen und Herren,
""",
]

ANWALT_BODY = [
    "namens und im Auftrag unseres Mandanten zeigen wir dessen Vertretung an.",
    "Wir nehmen Bezug auf den Vorgang vom {DATE}, der Gegenstand des Verfahrens vor dem {COURT} ist.",
    "Unsere Mandantschaft macht gegen Sie eine Forderung in Höhe von {AMOUNT} EUR geltend.",
    "Wir fordern Sie auf, den vorgenannten Betrag bis spätestens {DATE_DEADLINE} auf das Anderkonto unserer Kanzlei mit der IBAN {IBAN} zu überweisen.",
    "Für den Fall, dass der Betrag nicht fristgerecht eingeht, werden wir ohne weitere Ankündigung Klage beim {COURT} erheben.",
    "Eine vorgerichtliche Streitbeilegung ist ausdrücklich angestrebt; einen entsprechenden Vergleichsvorschlag fügen wir bei.",
    "Auf die Schweigepflicht des Unterzeichners als {PROFESSION} ist hinzuweisen.",
    "Beweisangebot: Zeugnis des Herrn {NAME_WITNESS}, wohnhaft {STREET}, {ZIP} {CITY}.",
    "Für die Korrespondenz bitten wir um Verwendung unseres Aktenzeichens {AKTENZEICHEN}.",
    "Vertretungsberechtigt ist der unterzeichnende Rechtsanwalt {NAME_ATTORNEY} der {KANZLEI}.",
    "Bei Rückfragen wenden Sie sich bitte werktags zwischen 9 und 17 Uhr unter {PHONE} oder per E-Mail an {EMAIL}.",
    "Wir weisen darauf hin, dass wir unsere Mandantschaft seit dem {DATE} in dieser Angelegenheit vertreten.",
]

ANWALT_FOOTERS = [
    """
Hochachtungsvoll


{NAME_ATTORNEY}
— Rechtsanwalt —
{KANZLEI}
""",
    """
Mit freundlichen Grüßen

{NAME_ATTORNEY}
{KANZLEI}
RA-NR {LAWYER_REG}
Tel.: {PHONE}  E-Mail: {EMAIL}
""",
]

DOCTYPES = {
    "klageschrift": (KLAGE_HEADERS, KLAGE_BODY, KLAGE_FOOTERS),
    "beschluss": (BESCHLUSS_HEADERS, BESCHLUSS_BODY, BESCHLUSS_FOOTERS),
    "vergleich": (VERGLEICH_HEADERS, VERGLEICH_BODY, VERGLEICH_FOOTERS),
    "vollmacht": (VOLLMACHT_HEADERS, VOLLMACHT_BODY, VOLLMACHT_FOOTERS),
    "anwaltsschreiben": (ANWALT_HEADERS, ANWALT_BODY, ANWALT_FOOTERS),
}
