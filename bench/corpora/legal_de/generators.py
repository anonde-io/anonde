#!/usr/bin/env python3
"""PII slot generators with bundled German legal-domain vocab.

Each generator returns a Slot(surface, type) tuple. The canonical type
strings follow the same GraSCCo-style conventions used by
synth_clinical/ — they all appear as keys in
bench/scoring/label_map.yaml::gold, so the existing label_map maps them
to the canonical vocabulary (PERSON, ORGANIZATION, LOCATION, ADDRESS,
DATE, ID, PHONE, EMAIL, IBAN, PROFESSION, AGE) without modification.

Type-name choices:

  NAME_DOCTOR      — used for attorneys, judges, notaries (canonical: PERSON).
                     Picked over NAME_PATIENT because in legal docs the
                     attorney/judge role is the closest analogue to the
                     "doctor" role in clinical: trained professional with
                     a title. NAME_PATIENT is reserved for natural-person
                     parties (plaintiffs, defendants, clients) and
                     NAME_RELATIVE for witnesses.
  LOCATION_HOSPITAL — used for courts and law-firm names. canonical:
                     ORGANIZATION. Re-using rather than inventing new
                     keys keeps label_map.yaml untouched per spec.
  LOCATION_ORGANIZATION — used for counterparty corporate names.
  LOCATION_CITY    — court seat city, party residence city.
  LOCATION_STREET  — street + number (German format "Hauptstraße 8").
  LOCATION_ZIP     — 5-digit German postcode.
  DATE / DATE_BIRTH — case/judgment/deadline dates and DOBs.
  CONTACT_PHONE / CONTACT_EMAIL — attorney + client contact.
  ID               — Aktenzeichen, Personalausweis, Steuer-ID, lawyer
                     registration number. All collapse to canonical ID.
  PROFESSION       — party occupation (in Vollmacht, Anwaltsschreiben).
  AGE              — DOB-derived age (occasional, family-law contexts).

  IBAN_CODE        — emitted as the anonde-side label; no gold-section
                     entry exists in label_map.yaml. Scored as OTHER on
                     the gold side, which is acceptable: the user
                     explicitly forbade extending label_map.yaml, and
                     IBAN appears infrequently (Anwaltsschreiben only).
"""

from __future__ import annotations

import random
import string
from dataclasses import dataclass

# ----- bundled German legal vocab ------------------------------------------

FIRST_NAMES_M = [
    "Hans", "Klaus", "Wolfgang", "Peter", "Werner", "Michael", "Jürgen",
    "Manfred", "Thomas", "Andreas", "Stefan", "Martin", "Frank", "Bernd",
    "Karl", "Heinz", "Dieter", "Helmut", "Walter", "Günter", "Horst",
    "Christian", "Matthias", "Sebastian", "Tobias", "Markus", "Lars",
    "Joachim", "Friedrich", "Maximilian", "Alexander", "Philipp", "Florian",
]

FIRST_NAMES_F = [
    "Petra", "Maria", "Ursula", "Renate", "Monika", "Sabine", "Brigitte",
    "Andrea", "Karin", "Heike", "Susanne", "Gabriele", "Barbara", "Christa",
    "Anna", "Birgit", "Claudia", "Christine", "Martina", "Angelika",
    "Stefanie", "Nicole", "Julia", "Katrin", "Silke", "Beate", "Ingrid",
    "Helga", "Doris", "Annette", "Charlotte", "Elisabeth", "Friederike",
]

LAST_NAMES = [
    "Müller", "Schmidt", "Schneider", "Fischer", "Weber", "Meyer", "Wagner",
    "Becker", "Schulz", "Hoffmann", "Schäfer", "Koch", "Bauer", "Richter",
    "Klein", "Wolf", "Schröder", "Neumann", "Schwarz", "Zimmermann", "Braun",
    "Krüger", "Hofmann", "Hartmann", "Lange", "Schmitt", "Werner", "Krause",
    "Lehmann", "Schmid", "Schulze", "Maier", "Köhler", "Herrmann", "König",
    "Walter", "Mayer", "Huber", "Kaiser", "Fuchs", "Peters", "Lang", "Scholz",
    "Vogel", "Stein", "Jäger", "Otto", "Sommer", "Winter", "Lorenz", "Roth",
]

CITIES = [
    "Berlin", "Hamburg", "München", "Köln", "Frankfurt am Main", "Stuttgart",
    "Düsseldorf", "Leipzig", "Dortmund", "Essen", "Bremen", "Dresden",
    "Hannover", "Nürnberg", "Duisburg", "Bochum", "Wuppertal", "Bielefeld",
    "Bonn", "Münster", "Karlsruhe", "Mannheim", "Augsburg", "Wiesbaden",
    "Mönchengladbach", "Gelsenkirchen", "Braunschweig", "Kiel", "Aachen",
    "Magdeburg", "Freiburg im Breisgau", "Krefeld", "Halle", "Lübeck",
    "Oldenburg", "Erfurt", "Mainz", "Rostock", "Kassel", "Hagen",
]

STREETS = [
    "Hauptstraße", "Bahnhofstraße", "Schillerstraße", "Goethestraße",
    "Lindenweg", "Bismarckstraße", "Kaiserallee", "Friedrichstraße",
    "Mozartweg", "Beethovenstraße", "Marktplatz", "Kirchgasse", "Am Rathaus",
    "Schulstraße", "Gartenweg", "Mühlenweg", "Talstraße", "Bergstraße",
    "Wilhelmstraße", "Mühlweg", "Rosenstraße", "Lessingstraße", "Parkallee",
    "Königsallee", "Theaterstraße", "Domplatz", "Marienplatz", "Heinrichstraße",
]

# Realistic invented court names: Amtsgericht/Landgericht/Oberlandesgericht
# + city. Bundesgerichtshof is single-instance (Karlsruhe).
COURT_PREFIXES = [
    "Amtsgericht", "Landgericht", "Oberlandesgericht",
    "Verwaltungsgericht", "Sozialgericht", "Arbeitsgericht",
]

# Standalone high-courts — used occasionally to add realism.
HIGH_COURTS = [
    "Bundesgerichtshof",
    "Bundesverwaltungsgericht",
    "Bundesarbeitsgericht",
    "Bundessozialgericht",
    "Bundesverfassungsgericht",
]

# Invented law-firm name fragments. Keep clearly fictional.
KANZLEI_SUFFIX = ["Rechtsanwälte", "und Partner", "Rechtsanwaltsgesellschaft",
                  "Partnerschaft mbB", "& Kollegen"]

# Invented company names for counterparties (Anwaltsschreiben).
COMPANY_BASES = [
    "Nordtech", "Süddeutsche Handels", "Rheinland Industrie", "Westfalen Bau",
    "Alpenland Logistik", "Mainland Software", "Hanse Werft", "Donau Energie",
    "Elbschloss Möbel", "Sachsenforst", "Schwarzwald Wein",
]
COMPANY_SUFFIX = ["GmbH", "AG", "GmbH & Co. KG", "SE", "UG (haftungsbeschränkt)"]

# Professions seen in legal docs — broader than synth_clinical's clinical
# subset.
PROFESSIONS = [
    "Rentner", "Rentnerin", "Lehrer", "Lehrerin", "Ingenieur", "Ingenieurin",
    "Krankenschwester", "Schreiner", "Bäcker", "Kaufmann", "Kauffrau",
    "Polizist", "Architekt", "Bankangestellter", "Verkäuferin",
    "Maschinenbautechniker", "Selbstständig", "Hausfrau", "Student", "Studentin",
    "Geschäftsführer", "Geschäftsführerin", "Steuerberater", "Wirtschaftsprüfer",
    "Beamter", "Beamtin", "Journalist", "Journalistin", "Unternehmer",
]

ATTORNEY_TITLES = [
    "Rechtsanwalt", "Rechtsanwältin", "Fachanwalt für Familienrecht",
    "Fachanwältin für Arbeitsrecht", "Fachanwalt für Strafrecht",
    "Fachanwältin für Verkehrsrecht", "Notar", "Notarin",
]

JUDGE_TITLES = [
    "Richter am Amtsgericht", "Richterin am Amtsgericht",
    "Vorsitzender Richter am Landgericht",
    "Vorsitzende Richterin am Landgericht",
    "Richter am Oberlandesgericht", "Richterin am Oberlandesgericht",
]

ROMAN_NUMERALS = ["I", "II", "III", "IV", "V", "VI", "VII", "VIII", "IX", "X",
                  "XI", "XII", "C", "O", "F", "S", "Ca", "Ds", "Ls", "Js"]


# ----- generator helpers ---------------------------------------------------


@dataclass
class Slot:
    """One slot fill: surface string + PHI type (gold-section key)."""

    text: str
    type: str


def _rng_pick(rng: random.Random, xs: list[str]) -> str:
    return xs[rng.randrange(len(xs))]


# ----- person generators ---------------------------------------------------


def gen_party_name(rng: random.Random) -> Slot:
    """Natural-person party (plaintiff, defendant, client, grantor)."""
    if rng.random() < 0.5:
        first = _rng_pick(rng, FIRST_NAMES_M)
    else:
        first = _rng_pick(rng, FIRST_NAMES_F)
    last = _rng_pick(rng, LAST_NAMES)
    return Slot(f"{first} {last}", "NAME_PATIENT")


def gen_witness_name(rng: random.Random) -> Slot:
    """Witness / third-party named in the doc."""
    first = _rng_pick(rng, FIRST_NAMES_M + FIRST_NAMES_F)
    last = _rng_pick(rng, LAST_NAMES)
    return Slot(f"{first} {last}", "NAME_RELATIVE")


def gen_attorney_name(rng: random.Random) -> Slot:
    """Rechtsanwalt, Anwältin, Notar — emitted with German legal title."""
    title = _rng_pick(rng, ATTORNEY_TITLES)
    first = _rng_pick(rng, FIRST_NAMES_M + FIRST_NAMES_F)
    last = _rng_pick(rng, LAST_NAMES)
    return Slot(f"{title} {first} {last}", "NAME_DOCTOR")


def gen_judge_name(rng: random.Random) -> Slot:
    title = _rng_pick(rng, JUDGE_TITLES)
    first = _rng_pick(rng, FIRST_NAMES_M + FIRST_NAMES_F)
    last = _rng_pick(rng, LAST_NAMES)
    return Slot(f"{title} {first} {last}", "NAME_DOCTOR")


def gen_notary_name(rng: random.Random) -> Slot:
    first = _rng_pick(rng, FIRST_NAMES_M + FIRST_NAMES_F)
    last = _rng_pick(rng, LAST_NAMES)
    return Slot(f"Notar {first} {last}", "NAME_DOCTOR")


# ----- date / age generators ------------------------------------------------


def gen_date(rng: random.Random) -> Slot:
    """German-format date — most case dates within 2018..2026."""
    day = rng.randint(1, 28)
    month = rng.randint(1, 12)
    year = rng.randint(2018, 2026)
    if rng.random() < 0.1:
        months = ["Januar", "Februar", "März", "April", "Mai", "Juni",
                  "Juli", "August", "September", "Oktober", "November", "Dezember"]
        return Slot(f"{day}. {months[month-1]} {year}", "DATE")
    return Slot(f"{day:02d}.{month:02d}.{year}", "DATE")


def gen_deadline(rng: random.Random) -> Slot:
    """Future deadline date — usually within 30 days of a notional today."""
    day = rng.randint(1, 28)
    month = rng.randint(1, 12)
    year = rng.randint(2025, 2027)
    return Slot(f"{day:02d}.{month:02d}.{year}", "DATE")


def gen_birthdate(rng: random.Random) -> Slot:
    day = rng.randint(1, 28)
    month = rng.randint(1, 12)
    year = rng.randint(1940, 2010)
    return Slot(f"{day:02d}.{month:02d}.{year}", "DATE_BIRTH")


def gen_age(rng: random.Random) -> Slot:
    return Slot(f"{rng.randint(18, 92)}", "AGE")


# ----- location generators --------------------------------------------------


def gen_city(rng: random.Random) -> Slot:
    return Slot(_rng_pick(rng, CITIES), "LOCATION_CITY")


def gen_court(rng: random.Random) -> Slot:
    """Either prefix + city ('Amtsgericht München') or a high-court."""
    if rng.random() < 0.1:
        return Slot(_rng_pick(rng, HIGH_COURTS), "LOCATION_HOSPITAL")
    prefix = _rng_pick(rng, COURT_PREFIXES)
    city = _rng_pick(rng, CITIES)
    return Slot(f"{prefix} {city}", "LOCATION_HOSPITAL")


def gen_kanzlei(rng: random.Random) -> Slot:
    """Law-firm name: '<LastName> & <LastName> Rechtsanwälte' style."""
    style = rng.random()
    if style < 0.5:
        a, b = _rng_pick(rng, LAST_NAMES), _rng_pick(rng, LAST_NAMES)
        while a == b:
            b = _rng_pick(rng, LAST_NAMES)
        suffix = _rng_pick(rng, KANZLEI_SUFFIX)
        return Slot(f"Kanzlei {a} & {b} {suffix}", "LOCATION_HOSPITAL")
    a = _rng_pick(rng, LAST_NAMES)
    suffix = _rng_pick(rng, KANZLEI_SUFFIX)
    return Slot(f"Kanzlei {a} {suffix}", "LOCATION_HOSPITAL")


def gen_company(rng: random.Random) -> Slot:
    base = _rng_pick(rng, COMPANY_BASES)
    suffix = _rng_pick(rng, COMPANY_SUFFIX)
    return Slot(f"{base} {suffix}", "LOCATION_ORGANIZATION")


def gen_street(rng: random.Random) -> Slot:
    s = _rng_pick(rng, STREETS)
    n = rng.randint(1, 250)
    return Slot(f"{s} {n}", "LOCATION_STREET")


def gen_zip(rng: random.Random) -> Slot:
    return Slot(f"{rng.randint(10000, 99999)}", "LOCATION_ZIP")


def gen_kammerbezirk(rng: random.Random) -> Slot:
    """Bezirk of a notarial chamber — emitted as a city-like LOCATION."""
    return Slot(_rng_pick(rng, CITIES), "LOCATION_CITY")


# ----- contact generators ---------------------------------------------------


def gen_phone(rng: random.Random) -> Slot:
    city_codes = ["30", "40", "89", "221", "69", "711", "211", "341",
                  "228", "351", "511", "911"]
    cc = _rng_pick(rng, city_codes)
    main = rng.randint(1000000, 99999999)
    style = rng.random()
    if style < 0.4:
        s = f"+49 {cc} {main}"
    elif style < 0.7:
        s = f"0{cc}/{main}"
    else:
        s = f"+49-{cc}-{main}"
    return Slot(s, "CONTACT_PHONE")


def gen_email(rng: random.Random) -> Slot:
    first = _rng_pick(rng, FIRST_NAMES_M + FIRST_NAMES_F).lower()
    last = _rng_pick(rng, LAST_NAMES).lower() \
        .replace("ü", "ue").replace("ö", "oe") \
        .replace("ä", "ae").replace("ß", "ss")
    domains = ["kanzlei.de", "rae.de", "anwalt.de", "law-de.de",
               "ra-online.de", "mail.de", "web.de"]
    return Slot(f"{first}.{last}@{_rng_pick(rng, domains)}", "CONTACT_EMAIL")


# ----- ID generators (Aktenzeichen, Personalausweis, etc.) ------------------


def gen_aktenzeichen(rng: random.Random) -> Slot:
    """Aktenzeichen format: '<digits> <Roman>/<yy>' — e.g. '4 O 217/23'.

    Production uses a small pool of plausible Senat / department Roman
    suffixes (O = Zivilkammer ordinary, C = Familiensachen,
    F = Familienrecht, Ca = Arbeitsgerichts-Klage, etc.). Years
    constrained to a 2-digit form 18..26.
    """
    senat = rng.randint(1, 39)
    suffix = _rng_pick(rng, ROMAN_NUMERALS)
    nr = rng.randint(1, 999)
    yr = rng.randint(18, 26)
    return Slot(f"{senat} {suffix} {nr}/{yr:02d}", "ID")


def gen_personalausweis(rng: random.Random) -> Slot:
    """Personalausweis number — 10 chars, alnum, German style."""
    chars = string.ascii_uppercase + string.digits
    return Slot("".join(rng.choice(chars) for _ in range(10)), "ID")


def gen_steuer_id(rng: random.Random) -> Slot:
    """11-digit Steuer-ID, grouped 2-3-3-3."""
    digits = [str(rng.randint(0, 9)) for _ in range(11)]
    s = f"{''.join(digits[0:2])} {''.join(digits[2:5])} {''.join(digits[5:8])} {''.join(digits[8:11])}"
    return Slot(s, "ID")


def gen_lawyer_reg(rng: random.Random) -> Slot:
    """Anwaltsregister registration number: RA-NR followed by 6-7 digits."""
    n = rng.randint(100000, 9999999)
    return Slot(f"RA-NR {n}", "ID")


# ----- IBAN with MOD-97 validation ------------------------------------------


def _iban_mod97_check(country: str, bank_account: str) -> str:
    """Compute the 2-digit IBAN checksum so the resulting IBAN is MOD-97 valid.

    Implements the standard ISO 13616 algorithm: rearrange (BBAN + country
    + '00'), convert letters to digits (A=10..Z=35), reduce mod 97, then
    checksum = 98 - remainder.
    """
    rearranged = bank_account + country + "00"
    converted = ""
    for ch in rearranged:
        if ch.isalpha():
            converted += str(ord(ch.upper()) - ord("A") + 10)
        else:
            converted += ch
    # Standard mod-97 by chunks (avoids huge ints, but Python handles it fine).
    rem = int(converted) % 97
    checksum = 98 - rem
    return f"{checksum:02d}"


def gen_iban(rng: random.Random) -> Slot:
    """German IBAN, MOD-97-validated. Format: DE<cs> 8-digit-blz 10-digit-acct.

    Surface stored without spaces — anonde recognisers commonly accept
    both, and the unspaced form keeps offset accounting simple.
    """
    blz = "".join(str(rng.randint(0, 9)) for _ in range(8))
    acct = "".join(str(rng.randint(0, 9)) for _ in range(10))
    bban = blz + acct
    cs = _iban_mod97_check("DE", bban)
    return Slot(f"DE{cs}{bban}", "IBAN_CODE")


# ----- profession ----------------------------------------------------------


def gen_profession(rng: random.Random) -> Slot:
    return Slot(_rng_pick(rng, PROFESSIONS), "PROFESSION")


GENERATORS = {
    # PERSON-canonical
    "NAME_PARTY": gen_party_name,
    "NAME_WITNESS": gen_witness_name,
    "NAME_ATTORNEY": gen_attorney_name,
    "NAME_JUDGE": gen_judge_name,
    "NAME_NOTARY": gen_notary_name,
    # DATE-canonical
    "DATE": gen_date,
    "DATE_DEADLINE": gen_deadline,
    "DATE_BIRTH": gen_birthdate,
    "AGE": gen_age,
    # LOCATION-canonical
    "CITY": gen_city,
    "COURT": gen_court,
    "KANZLEI": gen_kanzlei,
    "COMPANY": gen_company,
    "STREET": gen_street,
    "ZIP": gen_zip,
    "KAMMERBEZIRK": gen_kammerbezirk,
    # contact-canonical
    "PHONE": gen_phone,
    "EMAIL": gen_email,
    # ID-canonical
    "AKTENZEICHEN": gen_aktenzeichen,
    "PERSONALAUSWEIS": gen_personalausweis,
    "STEUER_ID": gen_steuer_id,
    "LAWYER_REG": gen_lawyer_reg,
    # IBAN
    "IBAN": gen_iban,
    # profession
    "PROFESSION": gen_profession,
}
