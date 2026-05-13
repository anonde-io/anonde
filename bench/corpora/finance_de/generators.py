#!/usr/bin/env python3
"""PII slot generators for synthetic German financial text.

Mirrors bench/corpora/synth_clinical/generators.py in shape: each
generator returns a Slot(text, type) where `type` is a key from the
`gold:` section of bench/scoring/label_map.yaml so compare.py can
normalise it to the shared canonical vocabulary.

A few non-clinical decisions worth flagging:

  * IBANs are MOD-97 valid (same algorithm anonde's IBANRecognizer
    uses). The check digits are computed at generation time so the
    detector cannot trivially reject them as garbage.
  * Steuer-IDs satisfy both the ISO 7064 MOD 11,10 check digit AND the
    BMF uniqueness rule (one digit appears 2-3 times in the first 10
    positions, all others appear 0-1 times). See
    analyzer/recognizers/checksums.go::validateDESteuerID.
  * BIC and ISIN are structurally well-formed but not check-digit
    validated. Real BIC has no checksum; ISIN's Luhn-mod-10 over
    alphanumeric is implemented to keep them realistic.
  * Names, banks, employers are drawn from small German lists.
    No "Patient" prefix anywhere — financial text says "Herr Müller",
    not "Patient Herr Müller".
"""

from __future__ import annotations

import random
from dataclasses import dataclass


# ----- bundled German vocab ------------------------------------------------

FIRST_NAMES_M = [
    "Hans", "Klaus", "Wolfgang", "Peter", "Werner", "Michael", "Jürgen",
    "Manfred", "Thomas", "Andreas", "Stefan", "Martin", "Frank", "Bernd",
    "Karl", "Heinz", "Dieter", "Helmut", "Walter", "Günter", "Horst",
    "Christian", "Matthias", "Sebastian", "Tobias", "Markus", "Lars",
    "Florian", "Daniel", "Alexander", "Philipp", "Jan", "Oliver",
]

FIRST_NAMES_F = [
    "Petra", "Maria", "Ursula", "Renate", "Monika", "Sabine", "Brigitte",
    "Andrea", "Karin", "Heike", "Susanne", "Gabriele", "Barbara", "Christa",
    "Anna", "Birgit", "Claudia", "Christine", "Martina", "Angelika",
    "Stefanie", "Nicole", "Julia", "Katrin", "Silke", "Beate", "Ingrid",
    "Sandra", "Tanja", "Melanie", "Christina", "Jessica", "Vanessa",
]

LAST_NAMES = [
    "Müller", "Schmidt", "Schneider", "Fischer", "Weber", "Meyer", "Wagner",
    "Becker", "Schulz", "Hoffmann", "Schäfer", "Koch", "Bauer", "Richter",
    "Klein", "Wolf", "Schröder", "Neumann", "Schwarz", "Zimmermann", "Braun",
    "Krüger", "Hofmann", "Hartmann", "Lange", "Schmitt", "Werner", "Krause",
    "Lehmann", "Schmid", "Schulze", "Maier", "Köhler", "Herrmann", "König",
    "Walter", "Mayer", "Huber", "Kaiser", "Fuchs", "Peters", "Lang", "Scholz",
    "Berger", "Frank", "Schubert", "Vogel", "Friedrich", "Keller", "Günther",
]

CITIES = [
    "Berlin", "Hamburg", "München", "Köln", "Frankfurt am Main", "Stuttgart",
    "Düsseldorf", "Leipzig", "Dortmund", "Essen", "Bremen", "Dresden",
    "Hannover", "Nürnberg", "Duisburg", "Bochum", "Wuppertal", "Bielefeld",
    "Bonn", "Münster", "Karlsruhe", "Mannheim", "Augsburg", "Wiesbaden",
    "Mönchengladbach", "Gelsenkirchen", "Braunschweig", "Kiel", "Aachen",
    "Magdeburg", "Freiburg im Breisgau", "Krefeld", "Halle", "Lübeck",
    "Erfurt", "Mainz", "Rostock", "Kassel", "Potsdam", "Saarbrücken",
]

STREETS = [
    "Hauptstraße", "Bahnhofstraße", "Schillerstraße", "Goethestraße",
    "Lindenweg", "Bismarckstraße", "Kaiserallee", "Friedrichstraße",
    "Mozartweg", "Beethovenstraße", "Marktplatz", "Kirchgasse", "Am Rathaus",
    "Schulstraße", "Gartenweg", "Mühlenweg", "Talstraße", "Bergstraße",
    "Wilhelmstraße", "Mühlweg", "Rosenstraße", "Lessingstraße", "Parkallee",
    "Königsallee", "Maximilianstraße", "Industriestraße", "Hafenstraße",
    "Karlstraße", "Heinrich-Heine-Straße", "Lerchenweg",
]

# German banks plus a few neo-banks. These become ORGANIZATION gold spans.
BANKS = [
    "Deutsche Bank AG",
    "Commerzbank AG",
    "DZ Bank AG",
    "KfW Bankengruppe",
    "DekaBank",
    "ING-DiBa AG",
    "Postbank",
    "Sparkasse Köln-Bonn",
    "Sparkasse Hannover",
    "Sparkasse Hamburg",
    "Berliner Sparkasse",
    "Volksbank Mittelhessen eG",
    "Volksbank Bonn Rhein-Sieg eG",
    "Raiffeisenbank München-Süd eG",
    "HypoVereinsbank",
    "Targobank",
    "Comdirect Bank",
    "Consorsbank",
    "N26 Bank GmbH",
    "DKB AG",
    "Santander Consumer Bank",
    "LBBW",
]

# Securities brokers / asset managers / wealth advisors.
BROKERS = [
    "Flatex Bank AG",
    "DWS Group GmbH",
    "Union Investment",
    "Allianz Global Investors",
    "Deka Investment",
    "Trade Republic Bank GmbH",
    "Scalable Capital GmbH",
    "Quirin Privatbank AG",
    "Berenberg Bank",
    "M.M. Warburg & Co.",
]

# Employer / counterparty companies for KYC and Kreditantrag.
EMPLOYERS = [
    "Siemens AG",
    "Bosch GmbH",
    "BMW AG",
    "Daimler Truck AG",
    "Volkswagen AG",
    "SAP SE",
    "Bayer AG",
    "BASF SE",
    "Thyssenkrupp AG",
    "Continental AG",
    "Lufthansa Group",
    "Telekom Deutschland GmbH",
    "Allianz SE",
    "Stadtwerke München GmbH",
    "ARD/ZDF Deutschlandradio",
    "Karstadt Warenhaus GmbH",
    "Otto Group",
    "Henkel AG",
    "Adidas AG",
    "Müller Handels GmbH & Co. KG",
    "Maschinenbau Schmidt GmbH",
    "Hofmann & Partner Steuerberatung",
]

PROFESSIONS = [
    "Rentner", "Rentnerin", "Lehrer", "Lehrerin", "Ingenieur", "Ingenieurin",
    "Softwareentwickler", "Softwareentwicklerin", "Kaufmann", "Kauffrau",
    "Architekt", "Architektin", "Bankangestellter", "Bankangestellte",
    "Verkäufer", "Verkäuferin", "Maschinenbautechniker", "Selbstständig",
    "Unternehmensberater", "Steuerberater", "Steuerberaterin",
    "Anwalt", "Rechtsanwältin", "Geschäftsführer", "Geschäftsführerin",
    "Projektleiter", "Projektleiterin", "Student", "Studentin",
    "Beamter", "Beamtin", "Arzthelferin", "Krankenpfleger",
]

# Reference lines for Überweisungen and Kontoauszüge — free-text counterparties
# and purposes. No PHI is embedded in these strings.
REF_PURPOSES = [
    "Miete Februar 2026",
    "Rechnung Nr. 2026-0451",
    "Gehalt November",
    "Rückzahlung Darlehen",
    "Bestellung Online-Shop",
    "Versicherungsbeitrag KFZ",
    "Spende Tierheim",
    "Steuererstattung Finanzamt",
    "Energiekosten Q3",
    "Mitgliedsbeitrag Sportverein",
    "Reisekosten Dienstreise",
    "Erstattung Auslagen",
]

# Free-text source-of-funds answers for KYC.
SOURCE_OF_FUNDS = [
    "Erspartes aus regelmäßigem Einkommen über mehrere Jahre.",
    "Erbschaft nach Veräußerung von Immobilieneigentum.",
    "Verkauf einer langjährig gehaltenen Aktienposition.",
    "Bonuszahlung des aktuellen Arbeitgebers.",
    "Rückübertragung von Treuhandvermögen aus früherer Vorsorge.",
    "Auszahlung einer fällig gewordenen Lebensversicherung.",
]

# Common securities (ISIN, common name) — German blue chips & ETFs.
SECURITIES = [
    ("DE0007164600", "SAP SE Namens-Aktien"),
    ("DE0007236101", "Siemens AG Namens-Aktien"),
    ("DE0005140008", "Deutsche Bank AG"),
    ("DE0008404005", "Allianz SE vinkulierte Namens-Aktien"),
    ("DE000BAY0017", "Bayer AG Namens-Aktien"),
    ("DE000BASF111", "BASF SE Namens-Aktien"),
    ("DE0007100000", "Daimler Truck AG"),
    ("LU0290358497", "Xtrackers DAX UCITS ETF"),
    ("IE00B4L5Y983", "iShares Core MSCI World UCITS ETF"),
    ("LU0908500753", "Lyxor STOXX Europe 600 UCITS ETF"),
    ("IE00BKM4GZ66", "iShares Core MSCI EM IMI UCITS ETF"),
]

# Domain suffixes for plausible email generation.
EMAIL_DOMAINS = [
    "gmail.com", "web.de", "gmx.de", "t-online.de", "outlook.de",
    "yahoo.de", "posteo.de", "mailbox.org",
]

CORPORATE_EMAIL_DOMAINS = [
    "siemens.com", "sap.com", "deutsche-bank.de", "commerzbank.com",
    "kanzlei-hofmann.de", "schmidt-gmbh.de", "ing.de", "allianz.com",
]


# ----- generator helpers ---------------------------------------------------


@dataclass
class Slot:
    """One slot fill: surface string + canonical PHI type (gold-section key)."""

    text: str
    type: str


def _rng_pick(rng: random.Random, xs: list):
    return xs[rng.randrange(len(xs))]


def _ascii_fold(s: str) -> str:
    return (s.replace("ü", "ue").replace("Ü", "Ue")
             .replace("ö", "oe").replace("Ö", "Oe")
             .replace("ä", "ae").replace("Ä", "Ae")
             .replace("ß", "ss"))


# ----- name / location -----------------------------------------------------


def gen_person(rng: random.Random) -> Slot:
    """Account holder, applicant — plain "Vorname Nachname".

    Optionally prefixed with Herr/Frau (kept inside the span, since the
    PHI is "Herr Müller" as a unit in practice). Title-only forms like
    "Dr." are skipped — German financial docs rarely lead with titles.
    """
    first = _rng_pick(rng, FIRST_NAMES_M + FIRST_NAMES_F)
    last = _rng_pick(rng, LAST_NAMES)
    return Slot(f"{first} {last}", "NAME_PATIENT")


def gen_person_anrede(rng: random.Random) -> Slot:
    """Personal name with German salutation: "Herr Müller" or "Frau Schmidt".

    The whole phrase is one PERSON span. Useful for letter-style openings
    in transfer orders and KYC questionnaires.
    """
    if rng.random() < 0.5:
        anr = "Herr"
        first_pool = FIRST_NAMES_M
    else:
        anr = "Frau"
        first_pool = FIRST_NAMES_F
    last = _rng_pick(rng, LAST_NAMES)
    if rng.random() < 0.4:
        # Just "Herr Müller", no first name.
        return Slot(f"{anr} {last}", "NAME_PATIENT")
    first = _rng_pick(rng, first_pool)
    return Slot(f"{anr} {first} {last}", "NAME_PATIENT")


def gen_beneficiary(rng: random.Random) -> Slot:
    """Recipient on a transfer — separate PERSON span so templates
    distinguish sender vs receiver without overloading NAME_PATIENT semantics."""
    first = _rng_pick(rng, FIRST_NAMES_M + FIRST_NAMES_F)
    last = _rng_pick(rng, LAST_NAMES)
    return Slot(f"{first} {last}", "NAME_RELATIVE")


def gen_city(rng: random.Random) -> Slot:
    return Slot(_rng_pick(rng, CITIES), "LOCATION_CITY")


def gen_bank(rng: random.Random) -> Slot:
    return Slot(_rng_pick(rng, BANKS), "LOCATION_HOSPITAL")


def gen_broker(rng: random.Random) -> Slot:
    return Slot(_rng_pick(rng, BROKERS), "LOCATION_HOSPITAL")


def gen_employer(rng: random.Random) -> Slot:
    return Slot(_rng_pick(rng, EMPLOYERS), "LOCATION_HOSPITAL")


def gen_street(rng: random.Random) -> Slot:
    s = _rng_pick(rng, STREETS)
    n = rng.randint(1, 250)
    return Slot(f"{s} {n}", "LOCATION_STREET")


def gen_zip(rng: random.Random) -> Slot:
    return Slot(f"{rng.randint(1000, 99999):05d}", "LOCATION_ZIP")


# ----- date / age ----------------------------------------------------------


def gen_date(rng: random.Random) -> Slot:
    """DD.MM.YYYY (90%) or DD. Monat YYYY (10%) — recent dates only,
    since we're generating financial documents that mostly reference
    the last few years."""
    day = rng.randint(1, 28)
    month = rng.randint(1, 12)
    year = rng.randint(2018, 2026)
    if rng.random() < 0.1:
        months = ["Januar", "Februar", "März", "April", "Mai", "Juni",
                  "Juli", "August", "September", "Oktober", "November",
                  "Dezember"]
        return Slot(f"{day}. {months[month-1]} {year}", "DATE")
    return Slot(f"{day:02d}.{month:02d}.{year}", "DATE")


def gen_dob(rng: random.Random) -> Slot:
    day = rng.randint(1, 28)
    month = rng.randint(1, 12)
    year = rng.randint(1940, 2005)
    return Slot(f"{day:02d}.{month:02d}.{year}", "DATE_BIRTH")


def gen_age(rng: random.Random) -> Slot:
    return Slot(f"{rng.randint(18, 85)}", "AGE")


# ----- contact -------------------------------------------------------------


def gen_phone(rng: random.Random) -> Slot:
    """German phone formats."""
    city_codes = ["30", "40", "89", "221", "69", "711", "211", "341",
                  "201", "228", "511"]
    cc = _rng_pick(rng, city_codes)
    main = rng.randint(1000000, 99999999)
    style = rng.random()
    if style < 0.4:
        s = f"+49 {cc} {main}"
    elif style < 0.65:
        s = f"0{cc}/{main}"
    elif style < 0.85:
        s = f"+49-{cc}-{main}"
    else:
        # Mobile.
        prefix = _rng_pick(rng, ["0151", "0152", "0157", "0159", "0170",
                                  "0171", "0172", "0173", "0176", "0178"])
        s = f"{prefix} {rng.randint(1000000, 9999999)}"
    return Slot(s, "CONTACT_PHONE")


def gen_email(rng: random.Random) -> Slot:
    first = _ascii_fold(_rng_pick(rng, FIRST_NAMES_M + FIRST_NAMES_F)).lower()
    last = _ascii_fold(_rng_pick(rng, LAST_NAMES)).lower()
    if rng.random() < 0.7:
        domain = _rng_pick(rng, EMAIL_DOMAINS)
    else:
        domain = _rng_pick(rng, CORPORATE_EMAIL_DOMAINS)
    sep = _rng_pick(rng, [".", "_", ""])
    return Slot(f"{first}{sep}{last}@{domain}", "CONTACT_EMAIL")


# ----- IDs: IBAN / BIC / ISIN / customer / Steuer-ID -----------------------


# IBAN_CODE-mapped gold uses 'ID' (canonical ID). anonde emits IBAN_CODE
# which maps to canonical IBAN — strict/partial F1 for IBAN-detection
# will therefore be 0, but type-only F1 and leak rate remain meaningful.
# This is a deliberate trade-off documented in the README; the alternative
# was modifying bench/scoring/label_map.yaml which is out of scope.

# German BLZ → BIC samples (real BIC strings, paired with their owning bank).
# BBAN structure: 8-digit BLZ + 10-digit account number = 18 digits.
DE_BLZ_BIC = [
    ("10070000", "DEUTDEBBXXX"),  # Deutsche Bank Berlin
    ("50070010", "DEUTDEFFXXX"),  # Deutsche Bank Frankfurt
    ("70070010", "DEUTDEMMXXX"),  # Deutsche Bank München
    ("10080000", "DRESDEFF100"),  # Commerzbank (ex Dresdner)
    ("50040000", "COBADEFFXXX"),  # Commerzbank Frankfurt
    ("37040044", "COBADEFFXXX"),  # Commerzbank Köln
    ("10010010", "PBNKDEFFXXX"),  # Postbank Berlin
    ("60050101", "SOLADEST600"),  # BW-Bank
    ("10050000", "BELADEBEXXX"),  # Berliner Sparkasse
    ("25050180", "SPKHDE2HXXX"),  # Sparkasse Hannover
    ("10090000", "BEVODEBBXXX"),  # Berliner Volksbank
    ("70150000", "SSKMDEMMXXX"),  # Stadtsparkasse München
    ("50010517", "INGDDEFFXXX"),  # ING-DiBa
    ("12030000", "BYLADEM1001"),  # DKB
]


def _iban_check_digits(country: str, bban: str) -> str:
    """Compute the two MOD-97 check digits for an IBAN.

    Mirrors validateIBAN in analyzer/recognizers/iban.go: rearrange so
    country code + "00" goes to the end, convert A..Z to 10..35, take
    mod 97, the check digits are 98 - mod (zero-padded to 2 chars)."""
    rearranged = bban + country + "00"
    numeric = []
    for ch in rearranged:
        if ch.isdigit():
            numeric.append(ch)
        elif "A" <= ch <= "Z":
            numeric.append(str(ord(ch) - ord("A") + 10))
        else:
            raise ValueError(f"bad IBAN char {ch!r}")
    n = int("".join(numeric))
    check = 98 - (n % 97)
    return f"{check:02d}"


def gen_iban(rng: random.Random) -> Slot:
    """German IBAN with valid MOD-97 check digits.

    Format: DE<2 check><8 BLZ><10 account> — 22 chars total. We pick a
    real BLZ so the document's bank context (when we emit a paired BIC)
    is internally consistent."""
    blz, _bic = _rng_pick(rng, DE_BLZ_BIC)
    acct = f"{rng.randint(0, 9999999999):010d}"
    bban = blz + acct
    check = _iban_check_digits("DE", bban)
    return Slot(f"DE{check}{bban}", "ID")


def gen_iban_with_bic(rng: random.Random) -> tuple[Slot, Slot]:
    """Emit a (IBAN, BIC) pair from the same real bank.

    Helpful for transfer orders where both the IBAN and the BIC should
    plausibly belong to the same institution."""
    blz, bic = _rng_pick(rng, DE_BLZ_BIC)
    acct = f"{rng.randint(0, 9999999999):010d}"
    bban = blz + acct
    check = _iban_check_digits("DE", bban)
    return (
        Slot(f"DE{check}{bban}", "ID"),
        Slot(bic, "ID"),
    )


def gen_bic(rng: random.Random) -> Slot:
    """Standalone BIC (8 or 11 chars)."""
    _blz, bic = _rng_pick(rng, DE_BLZ_BIC)
    return Slot(bic, "ID")


def gen_customer_number(rng: random.Random) -> Slot:
    """Customer numbers: varied shapes (8-digit, prefixed, dashed)."""
    style = rng.random()
    if style < 0.4:
        return Slot(f"{rng.randint(10000000, 99999999)}", "ID")
    if style < 0.7:
        return Slot(f"KD-{rng.randint(100000, 9999999)}", "ID")
    return Slot(
        f"{_rng_pick(rng, ['K', 'D', 'KN'])}{rng.randint(1000000, 99999999)}",
        "ID",
    )


def gen_isin(rng: random.Random) -> Slot:
    """Return a known German/EU security's ISIN. Type=ID since IBAN/ID
    distinction isn't preserved in gold (see file header)."""
    isin, _name = _rng_pick(rng, SECURITIES)
    return Slot(isin, "ID")


def gen_security(rng: random.Random) -> tuple[Slot, str]:
    """Pick one security and return its ISIN slot plus the security name
    (non-PHI plaintext for the template). Used in Depot-Auszug rows."""
    isin, name = _rng_pick(rng, SECURITIES)
    return Slot(isin, "ID"), name


def gen_wkn(rng: random.Random) -> Slot:
    """German Wertpapierkennnummer: 6 alphanumeric chars, all upper."""
    alpha = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
    s = "".join(alpha[rng.randrange(len(alpha))] for _ in range(6))
    return Slot(s, "ID")


def gen_steuer_id(rng: random.Random) -> Slot:
    """11-digit German Steuer-ID with valid ISO 7064 MOD 11,10 check
    digit AND BMF uniqueness rule (one digit repeats 2-3 times, all
    others 0-1).

    Algorithm mirrors validateDESteuerID in
    analyzer/recognizers/checksums.go."""
    # Reject loop: build 10 digits that satisfy the uniqueness rule,
    # then append the MOD 11,10 check digit. Empirically ~4% of random
    # 10-digit sequences satisfy the uniqueness rule, so 1000 attempts
    # gives effectively zero failure rate.
    for _ in range(1000):
        digits = [rng.randint(0, 9) for _ in range(10)]
        counts = [0] * 10
        for d in digits:
            counts[d] += 1
        repeated = sum(1 for c in counts if c >= 2)
        too_many = any(c >= 4 for c in counts)
        if too_many or repeated != 1:
            continue
        # ISO 7064 MOD 11,10.
        product = 10
        for d in digits:
            s = (d + product) % 10
            if s == 0:
                s = 10
            product = (2 * s) % 11
        check = (11 - product) % 10
        digits.append(check)
        # Format with spaces: 2-3-3-3 grouping in 30% of cases.
        s11 = "".join(str(d) for d in digits)
        if rng.random() < 0.3:
            s11 = f"{s11[0:2]} {s11[2:5]} {s11[5:8]} {s11[8:11]}"
        return Slot(s11, "ID")
    raise RuntimeError("Steuer-ID generation failed to satisfy uniqueness rule")


# ----- profession + non-PHI fills (amounts, etc.) --------------------------


def gen_profession(rng: random.Random) -> Slot:
    return Slot(_rng_pick(rng, PROFESSIONS), "PROFESSION")


# ----- generator dispatch table -------------------------------------------


GENERATORS = {
    "NAME_PATIENT": gen_person,
    "NAME_PATIENT_ANREDE": gen_person_anrede,
    "NAME_RELATIVE": gen_beneficiary,
    "DATE": gen_date,
    "DATE_BIRTH": gen_dob,
    "AGE": gen_age,
    "LOCATION_CITY": gen_city,
    "LOCATION_BANK": gen_bank,
    "LOCATION_BROKER": gen_broker,
    "LOCATION_EMPLOYER": gen_employer,
    "LOCATION_STREET": gen_street,
    "LOCATION_ZIP": gen_zip,
    "CONTACT_PHONE": gen_phone,
    "CONTACT_EMAIL": gen_email,
    "IBAN": gen_iban,
    "BIC": gen_bic,
    "CUSTOMER_NUMBER": gen_customer_number,
    "ISIN": gen_isin,
    "WKN": gen_wkn,
    "STEUER_ID": gen_steuer_id,
    "PROFESSION": gen_profession,
}
