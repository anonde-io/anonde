#!/usr/bin/env python3
"""PII slot generators with bundled German vocab.

Each generator returns a string that will be inserted into a template
along with the canonical PHI type for offset-tracking. The vocab is
intentionally small (a few hundred entries) — variety comes from
combination, not exhaustive listings. Bigger vocabs would just make the
bench harder to read.
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
]

FIRST_NAMES_F = [
    "Petra", "Maria", "Ursula", "Renate", "Monika", "Sabine", "Brigitte",
    "Andrea", "Karin", "Heike", "Susanne", "Gabriele", "Barbara", "Christa",
    "Anna", "Birgit", "Claudia", "Christine", "Martina", "Angelika",
    "Stefanie", "Nicole", "Julia", "Katrin", "Silke", "Beate", "Ingrid",
]

LAST_NAMES = [
    "Müller", "Schmidt", "Schneider", "Fischer", "Weber", "Meyer", "Wagner",
    "Becker", "Schulz", "Hoffmann", "Schäfer", "Koch", "Bauer", "Richter",
    "Klein", "Wolf", "Schröder", "Neumann", "Schwarz", "Zimmermann", "Braun",
    "Krüger", "Hofmann", "Hartmann", "Lange", "Schmitt", "Werner", "Krause",
    "Lehmann", "Schmid", "Schulze", "Maier", "Köhler", "Herrmann", "König",
    "Walter", "Mayer", "Huber", "Kaiser", "Fuchs", "Peters", "Lang", "Scholz",
]

CITIES = [
    "Berlin", "Hamburg", "München", "Köln", "Frankfurt am Main", "Stuttgart",
    "Düsseldorf", "Leipzig", "Dortmund", "Essen", "Bremen", "Dresden",
    "Hannover", "Nürnberg", "Duisburg", "Bochum", "Wuppertal", "Bielefeld",
    "Bonn", "Münster", "Karlsruhe", "Mannheim", "Augsburg", "Wiesbaden",
    "Mönchengladbach", "Gelsenkirchen", "Braunschweig", "Kiel", "Aachen",
    "Magdeburg", "Freiburg im Breisgau", "Krefeld", "Halle", "Lübeck",
]

STREETS = [
    "Hauptstraße", "Bahnhofstraße", "Schillerstraße", "Goethestraße",
    "Lindenweg", "Bismarckstraße", "Kaiserallee", "Friedrichstraße",
    "Mozartweg", "Beethovenstraße", "Marktplatz", "Kirchgasse", "Am Rathaus",
    "Schulstraße", "Gartenweg", "Mühlenweg", "Talstraße", "Bergstraße",
    "Wilhelmstraße", "Mühlweg", "Rosenstraße", "Lessingstraße", "Parkallee",
]

HOSPITALS = [
    "Charité Berlin",
    "Universitätsklinikum Hamburg-Eppendorf",
    "Klinikum der Universität München",
    "Universitätsklinikum Heidelberg",
    "Klinikum Bremen-Mitte",
    "Universitätsklinikum Düsseldorf",
    "Klinikum rechts der Isar",
    "Vivantes Klinikum im Friedrichshain",
    "Diakonissenkrankenhaus Stuttgart",
    "St. Marien-Krankenhaus Siegen",
    "Asklepios Klinik Barmbek",
    "Sana Klinikum Lichtenberg",
    "Helios Klinikum Erfurt",
    "Klinikum Großhadern",
    "Universitätsklinikum Tübingen",
]

DOCTOR_TITLES = [
    "Dr. med.", "Prof. Dr. med.", "PD Dr. med.", "Dr.", "Dr. med. univ.",
]

PROFESSIONS = [
    "Rentner", "Rentnerin", "Lehrer", "Lehrerin", "Ingenieur", "Ingenieurin",
    "Krankenschwester", "Schreiner", "Bäcker", "Kaufmann", "Kauffrau",
    "Polizist", "Architekt", "Bankangestellter", "Verkäuferin",
    "Maschinenbautechniker", "Selbstständig", "Hausfrau", "Student", "Studentin",
]


# ----- generator helpers ---------------------------------------------------


@dataclass
class Slot:
    """One slot fill: surface string + canonical PHI type."""

    text: str
    type: str


def _rng_pick(rng: random.Random, xs: list[str]) -> str:
    return xs[rng.randrange(len(xs))]


def gen_patient_name(rng: random.Random) -> Slot:
    if rng.random() < 0.5:
        first = _rng_pick(rng, FIRST_NAMES_M)
    else:
        first = _rng_pick(rng, FIRST_NAMES_F)
    last = _rng_pick(rng, LAST_NAMES)
    return Slot(f"{first} {last}", "NAME_PATIENT")


def gen_relative_name(rng: random.Random) -> Slot:
    first = _rng_pick(rng, FIRST_NAMES_M + FIRST_NAMES_F)
    last = _rng_pick(rng, LAST_NAMES)
    return Slot(f"{first} {last}", "NAME_RELATIVE")


def gen_doctor_name(rng: random.Random) -> Slot:
    title = _rng_pick(rng, DOCTOR_TITLES)
    first = _rng_pick(rng, FIRST_NAMES_M + FIRST_NAMES_F)
    last = _rng_pick(rng, LAST_NAMES)
    return Slot(f"{title} {first} {last}", "NAME_DOCTOR")


def gen_date(rng: random.Random) -> Slot:
    """German-format date: DD.MM.YYYY (90%) or DD. Monat YYYY (10%)."""
    day = rng.randint(1, 28)
    month = rng.randint(1, 12)
    year = rng.randint(1950, 2025)
    if rng.random() < 0.1:
        months = ["Januar", "Februar", "März", "April", "Mai", "Juni",
                  "Juli", "August", "September", "Oktober", "November", "Dezember"]
        return Slot(f"{day}. {months[month-1]} {year}", "DATE")
    return Slot(f"{day:02d}.{month:02d}.{year}", "DATE")


def gen_birthdate(rng: random.Random) -> Slot:
    day = rng.randint(1, 28)
    month = rng.randint(1, 12)
    year = rng.randint(1925, 2010)
    return Slot(f"{day:02d}.{month:02d}.{year}", "DATE_BIRTH")


def gen_time(rng: random.Random) -> Slot:
    h = rng.randint(0, 23)
    m = rng.randint(0, 59)
    return Slot(f"{h:02d}:{m:02d}", "DATE")  # time-of-day stored as DATE


def gen_city(rng: random.Random) -> Slot:
    return Slot(_rng_pick(rng, CITIES), "LOCATION_CITY")


def gen_hospital(rng: random.Random) -> Slot:
    return Slot(_rng_pick(rng, HOSPITALS), "LOCATION_HOSPITAL")


def gen_street(rng: random.Random) -> Slot:
    s = _rng_pick(rng, STREETS)
    n = rng.randint(1, 250)
    return Slot(f"{s} {n}", "LOCATION_STREET")


def gen_zip(rng: random.Random) -> Slot:
    return Slot(f"{rng.randint(1000, 99999):05d}", "LOCATION_ZIP")


def gen_phone(rng: random.Random) -> Slot:
    """+49 (city-code) main-number, mimicking common German formats."""
    city_codes = ["30", "40", "89", "221", "69", "711", "211", "341"]
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
    last = _rng_pick(rng, LAST_NAMES).lower().replace("ü", "ue").replace("ö", "oe").replace("ä", "ae").replace("ß", "ss")
    domains = ["klinik.de", "uniklinik.de", "krankenhaus.de", "mail.de", "web.de"]
    return Slot(f"{first}.{last}@{_rng_pick(rng, domains)}", "CONTACT_EMAIL")


def gen_id(rng: random.Random) -> Slot:
    """Patient ID — varied formats: numeric, alpha-prefix, dashed."""
    style = rng.random()
    if style < 0.4:
        return Slot(f"{rng.randint(100000, 9999999)}", "ID")
    if style < 0.7:
        return Slot(f"PAT-{rng.randint(10000, 999999)}", "ID")
    return Slot(f"{rng.choice(['HN', 'KL', 'MR'])}{rng.randint(100000, 999999)}", "ID")


def gen_age(rng: random.Random) -> Slot:
    return Slot(f"{rng.randint(1, 99)}", "AGE")


def gen_profession(rng: random.Random) -> Slot:
    return Slot(_rng_pick(rng, PROFESSIONS), "PROFESSION")


GENERATORS = {
    "NAME_PATIENT": gen_patient_name,
    "NAME_RELATIVE": gen_relative_name,
    "NAME_DOCTOR": gen_doctor_name,
    "DATE": gen_date,
    "DATE_BIRTH": gen_birthdate,
    "TIME": gen_time,
    "LOCATION_CITY": gen_city,
    "LOCATION_HOSPITAL": gen_hospital,
    "LOCATION_STREET": gen_street,
    "LOCATION_ZIP": gen_zip,
    "CONTACT_PHONE": gen_phone,
    "CONTACT_EMAIL": gen_email,
    "ID": gen_id,
    "AGE": gen_age,
    "PROFESSION": gen_profession,
}
