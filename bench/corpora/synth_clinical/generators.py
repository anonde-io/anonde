#!/usr/bin/env python3
"""Locale-parametrised PII slot generators for synthetic clinical text.

This is the SINGLE shared generator for all four synth_clinical_{de,en,
fr,it} corpora — mirrors the synth_finance / ai4privacy / mapa
shared-loader pattern. The three sibling corpora
(synth_clinical_{en,fr,it}) are thin Makefile wrappers that invoke this
module via ../synth_clinical/generate.py with a different --language.
There is no per-language copy of the generator.

(There is deliberately NO synth_clinical_es: Spanish clinical PHI is
covered by the real-gold MEDDOCAN corpus, bench/corpora/meddocan_es.)

Each generator returns a Slot(text, type): the surface string that will
be spliced into a template, plus the canonical PHI type for
offset-tracking. Gold types follow the GraSCCo PHI conventions
(NAME_PATIENT, LOCATION_HOSPITAL, …) so the existing label_map.yaml
gold: section maps them with no changes.

Regression contract — the German behaviour MUST stay byte-identical to
the pre-refactor single-language generator. `--language de` (the
default) draws from the same German vocab in the same order with the
same number of rng calls per generator, so a fixed seed reproduces the
historical synth_clinical corpus exactly. The de vocab blocks below are
copied verbatim from the original generators.py; the en/fr/it blocks are
additive.

Vocab is intentionally small per locale: variety comes from
combination, not exhaustive listings.
"""

from __future__ import annotations

import random
from dataclasses import dataclass

# ===========================================================================
# Per-locale vocab — keyed by --language code.
# ===========================================================================

# Male given names.
FIRST_NAMES_M = {
    "de": [
        "Hans", "Klaus", "Wolfgang", "Peter", "Werner", "Michael", "Jürgen",
        "Manfred", "Thomas", "Andreas", "Stefan", "Martin", "Frank", "Bernd",
        "Karl", "Heinz", "Dieter", "Helmut", "Walter", "Günter", "Horst",
        "Christian", "Matthias", "Sebastian", "Tobias", "Markus", "Lars",
    ],
    "en": [
        "James", "Robert", "John", "Michael", "David", "William", "Richard",
        "Joseph", "Thomas", "Charles", "Daniel", "Matthew", "Anthony", "Mark",
        "Donald", "Steven", "Andrew", "Paul", "Joshua", "Kenneth", "Kevin",
        "Brian", "George", "Edward", "Ronald", "Timothy", "Jason",
    ],
    "fr": [
        "Jean", "Pierre", "Michel", "Alain", "Philippe", "Nicolas",
        "Christophe", "Laurent", "Stéphane", "David", "Julien", "Sébastien",
        "Olivier", "Antoine", "Mathieu", "Vincent", "Thomas", "Patrick",
        "Bernard", "Daniel", "Frédéric", "Gérard", "Pascal", "Bruno",
        "Henri", "Guillaume", "Maxime",
    ],
    "it": [
        "Giuseppe", "Antonio", "Giovanni", "Mario", "Luigi", "Francesco",
        "Marco", "Andrea", "Roberto", "Stefano", "Paolo", "Alessandro",
        "Luca", "Matteo", "Davide", "Simone", "Lorenzo", "Salvatore",
        "Vincenzo", "Carlo", "Sergio", "Bruno", "Angelo", "Fabio",
        "Riccardo", "Federico", "Pietro",
    ],
}

# Female given names.
FIRST_NAMES_F = {
    "de": [
        "Petra", "Maria", "Ursula", "Renate", "Monika", "Sabine", "Brigitte",
        "Andrea", "Karin", "Heike", "Susanne", "Gabriele", "Barbara", "Christa",
        "Anna", "Birgit", "Claudia", "Christine", "Martina", "Angelika",
        "Stefanie", "Nicole", "Julia", "Katrin", "Silke", "Beate", "Ingrid",
    ],
    "en": [
        "Mary", "Patricia", "Jennifer", "Linda", "Elizabeth", "Barbara",
        "Susan", "Jessica", "Sarah", "Karen", "Nancy", "Lisa", "Margaret",
        "Sandra", "Ashley", "Emily", "Olivia", "Dorothy", "Carol", "Amanda",
        "Melissa", "Deborah", "Rebecca", "Laura", "Helen", "Sharon",
        "Cynthia",
    ],
    "fr": [
        "Marie", "Nathalie", "Isabelle", "Sylvie", "Catherine", "Françoise",
        "Martine", "Christine", "Monique", "Sandrine", "Céline", "Valérie",
        "Sophie", "Aurélie", "Émilie", "Camille", "Julie", "Nicole",
        "Brigitte", "Anne", "Hélène", "Chantal", "Laurence", "Patricia",
        "Caroline", "Delphine", "Manon",
    ],
    "it": [
        "Maria", "Anna", "Giulia", "Rosa", "Angela", "Giovanna", "Teresa",
        "Lucia", "Carmela", "Caterina", "Francesca", "Elena", "Sara",
        "Chiara", "Valentina", "Federica", "Martina", "Paola", "Laura",
        "Silvia", "Daniela", "Marta", "Simona", "Elisabetta", "Cristina",
        "Alessandra", "Roberta",
    ],
}

# Surnames.
LAST_NAMES = {
    "de": [
        "Müller", "Schmidt", "Schneider", "Fischer", "Weber", "Meyer", "Wagner",
        "Becker", "Schulz", "Hoffmann", "Schäfer", "Koch", "Bauer", "Richter",
        "Klein", "Wolf", "Schröder", "Neumann", "Schwarz", "Zimmermann", "Braun",
        "Krüger", "Hofmann", "Hartmann", "Lange", "Schmitt", "Werner", "Krause",
        "Lehmann", "Schmid", "Schulze", "Maier", "Köhler", "Herrmann", "König",
        "Walter", "Mayer", "Huber", "Kaiser", "Fuchs", "Peters", "Lang", "Scholz",
    ],
    "en": [
        "Smith", "Johnson", "Williams", "Brown", "Jones", "Miller", "Davis",
        "Wilson", "Anderson", "Taylor", "Thomas", "Moore", "Jackson", "Martin",
        "Lee", "Thompson", "White", "Harris", "Clark", "Lewis", "Robinson",
        "Walker", "Young", "Allen", "King", "Wright", "Scott", "Hill", "Green",
        "Adams", "Baker", "Nelson", "Carter", "Mitchell", "Roberts", "Turner",
        "Phillips", "Campbell", "Parker", "Evans", "Edwards", "Collins",
        "Stewart", "Morris",
    ],
    "fr": [
        "Martin", "Bernard", "Dubois", "Thomas", "Robert", "Richard", "Petit",
        "Durand", "Leroy", "Moreau", "Simon", "Laurent", "Lefebvre", "Michel",
        "Garcia", "David", "Bertrand", "Roux", "Vincent", "Fournier", "Morel",
        "Girard", "André", "Lefèvre", "Mercier", "Dupont", "Lambert", "Bonnet",
        "François", "Martinez", "Legrand", "Garnier", "Faure", "Rousseau",
        "Blanc", "Guerin", "Muller", "Henry", "Roussel", "Nicolas", "Perrin",
        "Morin", "Mathieu", "Clement",
    ],
    "it": [
        "Rossi", "Russo", "Ferrari", "Esposito", "Bianchi", "Romano",
        "Colombo", "Ricci", "Marino", "Greco", "Bruno", "Gallo", "Conti",
        "De Luca", "Mancini", "Costa", "Giordano", "Rizzo", "Lombardi",
        "Moretti", "Barbieri", "Fontana", "Santoro", "Mariani", "Rinaldi",
        "Caruso", "Ferrara", "Galli", "Martini", "Leone", "Longo", "Gentile",
        "Martinelli", "Vitale", "Lombardo", "Serra", "Coppola", "De Santis",
        "D'Angelo", "Marchetti", "Parisi", "Villa", "Conte", "Ferro",
    ],
}

# Cities.
CITIES = {
    "de": [
        "Berlin", "Hamburg", "München", "Köln", "Frankfurt am Main", "Stuttgart",
        "Düsseldorf", "Leipzig", "Dortmund", "Essen", "Bremen", "Dresden",
        "Hannover", "Nürnberg", "Duisburg", "Bochum", "Wuppertal", "Bielefeld",
        "Bonn", "Münster", "Karlsruhe", "Mannheim", "Augsburg", "Wiesbaden",
        "Mönchengladbach", "Gelsenkirchen", "Braunschweig", "Kiel", "Aachen",
        "Magdeburg", "Freiburg im Breisgau", "Krefeld", "Halle", "Lübeck",
    ],
    "en": [
        "London", "Manchester", "Birmingham", "Leeds", "Glasgow", "Liverpool",
        "Bristol", "Sheffield", "Edinburgh", "Cardiff", "Leicester",
        "Nottingham", "Newcastle", "Brighton", "Coventry", "Bradford",
        "Plymouth", "Reading", "Oxford", "Cambridge", "Portsmouth", "Norwich",
        "Southampton", "Aberdeen", "Belfast", "Exeter", "York", "Bath",
    ],
    "fr": [
        "Paris", "Marseille", "Lyon", "Toulouse", "Nice", "Nantes",
        "Strasbourg", "Montpellier", "Bordeaux", "Lille", "Rennes", "Reims",
        "Toulon", "Grenoble", "Dijon", "Angers", "Nîmes", "Le Havre",
        "Clermont-Ferrand", "Tours", "Amiens", "Limoges", "Metz", "Besançon",
        "Caen", "Orléans", "Rouen", "Nancy", "Poitiers", "Avignon",
    ],
    "it": [
        "Roma", "Milano", "Napoli", "Torino", "Palermo", "Genova", "Bologna",
        "Firenze", "Bari", "Catania", "Venezia", "Verona", "Messina",
        "Padova", "Trieste", "Brescia", "Parma", "Modena", "Reggio Calabria",
        "Perugia", "Livorno", "Cagliari", "Foggia", "Salerno", "Ferrara",
        "Latina", "Monza", "Bergamo", "Pescara", "Vicenza",
    ],
}

# Street templates use a {n} placeholder for the house number; the
# generator renders the number after the street name for de/it,
# before it for en/fr — locale-correct address ordering.
STREETS = {
    "de": [
        "Hauptstraße", "Bahnhofstraße", "Schillerstraße", "Goethestraße",
        "Lindenweg", "Bismarckstraße", "Kaiserallee", "Friedrichstraße",
        "Mozartweg", "Beethovenstraße", "Marktplatz", "Kirchgasse", "Am Rathaus",
        "Schulstraße", "Gartenweg", "Mühlenweg", "Talstraße", "Bergstraße",
        "Wilhelmstraße", "Mühlweg", "Rosenstraße", "Lessingstraße", "Parkallee",
    ],
    "en": [
        "High Street", "Station Road", "Church Lane", "Park Avenue",
        "Victoria Road", "Mill Lane", "Queen Street", "King Street",
        "Main Street", "Oxford Road", "Maple Drive", "Elm Close",
        "London Road", "The Green", "Manor Road", "School Lane",
        "West Street", "North Road", "Springfield Road", "Grange Road",
    ],
    "fr": [
        "rue de la République", "avenue des Champs", "rue Victor Hugo",
        "boulevard Voltaire", "rue de Paris", "rue de la Gare",
        "place de la Mairie", "rue Nationale", "rue Jean Jaurès",
        "avenue de la Liberté", "rue du Moulin", "rue des Écoles",
        "rue Pasteur", "boulevard Gambetta", "rue de l'Église",
        "avenue du Général de Gaulle", "rue Saint-Martin", "impasse des Lilas",
    ],
    "it": [
        "Via Roma", "Via Garibaldi", "Corso Italia", "Via Dante", "Via Mazzini",
        "Piazza del Duomo", "Via Marconi", "Via Verdi", "Via Nazionale",
        "Via San Martino", "Corso Vittorio Emanuele", "Via Veneto",
        "Via Cavour", "Via Manzoni", "Via XX Settembre", "Largo Europa",
        "Via della Libertà", "Via Trento", "Via Milano", "Via Torino",
    ],
}

# Hospital / clinic names — become LOCATION_HOSPITAL gold spans.
HOSPITALS = {
    "de": [
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
    ],
    "en": [
        "St Thomas' Hospital",
        "Royal London Hospital",
        "Addenbrooke's Hospital",
        "John Radcliffe Hospital",
        "Manchester Royal Infirmary",
        "Queen Elizabeth Hospital Birmingham",
        "Leeds General Infirmary",
        "Royal Victoria Infirmary",
        "Bristol Royal Infirmary",
        "Guy's Hospital",
        "King's College Hospital",
        "Southampton General Hospital",
        "Nottingham City Hospital",
        "Royal Free Hospital",
        "University College Hospital",
    ],
    "fr": [
        "Hôpital de la Pitié-Salpêtrière",
        "Hôpital Cochin",
        "Hôpital Necker-Enfants Malades",
        "Hôpital Européen Georges-Pompidou",
        "CHU de Bordeaux",
        "CHU de Lille",
        "Hôpital Édouard-Herriot",
        "CHU de Toulouse",
        "Hôpital de la Timone",
        "CHU de Nantes",
        "Hôpital Saint-Louis",
        "Hôpital Bichat-Claude-Bernard",
        "CHU de Grenoble",
        "Hôpital de Hautepierre",
        "CHU de Rennes",
    ],
    "it": [
        "Policlinico Gemelli",
        "Ospedale San Raffaele",
        "Azienda Ospedaliera di Padova",
        "Ospedale Niguarda",
        "Policlinico Sant'Orsola-Malpighi",
        "Azienda Ospedaliero-Universitaria Careggi",
        "Ospedale San Giovanni Battista",
        "Policlinico Umberto I",
        "Ospedale Maggiore di Bologna",
        "Azienda Ospedaliera Universitaria di Verona",
        "Ospedale Civico di Palermo",
        "Policlinico San Matteo",
        "Ospedale Santa Maria Nuova",
        "Azienda Ospedaliera di Cosenza",
        "Ospedale Cervello",
    ],
}

# Doctor honorific/title prefixes.
DOCTOR_TITLES = {
    "de": ["Dr. med.", "Prof. Dr. med.", "PD Dr. med.", "Dr.", "Dr. med. univ."],
    "en": ["Dr", "Prof.", "Dr", "Mr", "Ms"],
    "fr": ["Dr", "Pr", "Dr", "Dr méd.", "Pr"],
    "it": ["Dott.", "Prof.", "Dott.ssa", "Dr.", "Prof."],
}

# Professions.
PROFESSIONS = {
    "de": [
        "Rentner", "Rentnerin", "Lehrer", "Lehrerin", "Ingenieur", "Ingenieurin",
        "Krankenschwester", "Schreiner", "Bäcker", "Kaufmann", "Kauffrau",
        "Polizist", "Architekt", "Bankangestellter", "Verkäuferin",
        "Maschinenbautechniker", "Selbstständig", "Hausfrau", "Student", "Studentin",
    ],
    "en": [
        "retired", "teacher", "engineer", "nurse", "carpenter", "baker",
        "shop assistant", "police officer", "architect", "bank clerk",
        "self-employed", "homemaker", "student", "accountant", "electrician",
        "lorry driver", "plumber", "civil servant", "cleaner", "chef",
    ],
    "fr": [
        "retraité", "retraitée", "enseignant", "enseignante", "ingénieur",
        "infirmière", "menuisier", "boulanger", "commerçant", "policier",
        "architecte", "employé de banque", "indépendant", "femme au foyer",
        "étudiant", "étudiante", "comptable", "électricien", "chauffeur",
        "cuisinier",
    ],
    "it": [
        "pensionato", "pensionata", "insegnante", "ingegnere", "infermiere",
        "falegname", "fornaio", "commerciante", "poliziotto", "architetto",
        "impiegato di banca", "lavoratore autonomo", "casalinga", "studente",
        "studentessa", "ragioniere", "elettricista", "autista", "cuoco",
        "operaio",
    ],
}

# Email free-mail / clinic domains, per locale.
EMAIL_DOMAINS = {
    "de": ["klinik.de", "uniklinik.de", "krankenhaus.de", "mail.de", "web.de"],
    "en": ["nhs.net", "hospital.org.uk", "clinic.co.uk", "gmail.com",
           "outlook.com"],
    "fr": ["chu.fr", "hopital.fr", "clinique.fr", "orange.fr", "gmail.com"],
    "it": ["ospedale.it", "asl.it", "policlinico.it", "libero.it", "gmail.com"],
}


# ===========================================================================
# Slot dataclass + helpers
# ===========================================================================


@dataclass
class Slot:
    """One slot fill: surface string + canonical PHI type."""

    text: str
    type: str


def _rng_pick(rng: random.Random, xs: list[str]) -> str:
    return xs[rng.randrange(len(xs))]


def _ascii_fold(s: str) -> str:
    """Fold accented letters to ASCII — used to build email local parts."""
    table = {
        "ü": "ue", "Ü": "Ue", "ö": "oe", "Ö": "Oe", "ä": "ae", "Ä": "Ae",
        "ß": "ss", "á": "a", "à": "a", "â": "a", "é": "e", "è": "e", "ê": "e",
        "ë": "e", "í": "i", "ì": "i", "î": "i", "ï": "i", "ó": "o", "ò": "o",
        "ô": "o", "ú": "u", "ù": "u", "û": "u", "ñ": "n", "ç": "c", "É": "E",
        "À": "A",
    }
    return "".join(table.get(ch, ch) for ch in s)


# ===========================================================================
# Generators — each takes (lang, rng) and returns Slot(text, canonical_type).
# ===========================================================================
#
# REGRESSION CONTRACT: for lang == "de" the rng-call order below is
# IDENTICAL to the pre-refactor single-language generators.py. The de
# vocab lists are copied verbatim. A fixed seed therefore reproduces the
# historical synth_clinical corpus byte-for-byte.


def gen_patient_name(lang: str, rng: random.Random) -> Slot:
    if rng.random() < 0.5:
        first = _rng_pick(rng, FIRST_NAMES_M[lang])
    else:
        first = _rng_pick(rng, FIRST_NAMES_F[lang])
    last = _rng_pick(rng, LAST_NAMES[lang])
    return Slot(f"{first} {last}", "NAME_PATIENT")


def gen_relative_name(lang: str, rng: random.Random) -> Slot:
    first = _rng_pick(rng, FIRST_NAMES_M[lang] + FIRST_NAMES_F[lang])
    last = _rng_pick(rng, LAST_NAMES[lang])
    return Slot(f"{first} {last}", "NAME_RELATIVE")


def gen_doctor_name(lang: str, rng: random.Random) -> Slot:
    title = _rng_pick(rng, DOCTOR_TITLES[lang])
    first = _rng_pick(rng, FIRST_NAMES_M[lang] + FIRST_NAMES_F[lang])
    last = _rng_pick(rng, LAST_NAMES[lang])
    return Slot(f"{title} {first} {last}", "NAME_DOCTOR")


def gen_date(lang: str, rng: random.Random) -> Slot:
    """Locale-formatted date. de/fr/it use DD.MM.YYYY (90%) or "D Monat
    YYYY" (10%); en uses DD/MM/YYYY (90%) or "D Month YYYY" (10%)."""
    day = rng.randint(1, 28)
    month = rng.randint(1, 12)
    year = rng.randint(1950, 2025)
    months = {
        "de": ["Januar", "Februar", "März", "April", "Mai", "Juni", "Juli",
               "August", "September", "Oktober", "November", "Dezember"],
        "en": ["January", "February", "March", "April", "May", "June", "July",
               "August", "September", "October", "November", "December"],
        "fr": ["janvier", "février", "mars", "avril", "mai", "juin", "juillet",
               "août", "septembre", "octobre", "novembre", "décembre"],
        "it": ["gennaio", "febbraio", "marzo", "aprile", "maggio", "giugno",
               "luglio", "agosto", "settembre", "ottobre", "novembre",
               "dicembre"],
    }
    if rng.random() < 0.1:
        return Slot(f"{day}. {months[lang][month-1]} {year}"
                    if lang == "de"
                    else f"{day} {months[lang][month-1]} {year}", "DATE")
    sep = "/" if lang == "en" else "."
    return Slot(f"{day:02d}{sep}{month:02d}{sep}{year}", "DATE")


def gen_birthdate(lang: str, rng: random.Random) -> Slot:
    day = rng.randint(1, 28)
    month = rng.randint(1, 12)
    year = rng.randint(1925, 2010)
    sep = "/" if lang == "en" else "."
    return Slot(f"{day:02d}{sep}{month:02d}{sep}{year}", "DATE_BIRTH")


def gen_time(lang: str, rng: random.Random) -> Slot:
    h = rng.randint(0, 23)
    m = rng.randint(0, 59)
    return Slot(f"{h:02d}:{m:02d}", "DATE")  # time-of-day stored as DATE


def gen_city(lang: str, rng: random.Random) -> Slot:
    return Slot(_rng_pick(rng, CITIES[lang]), "LOCATION_CITY")


def gen_hospital(lang: str, rng: random.Random) -> Slot:
    return Slot(_rng_pick(rng, HOSPITALS[lang]), "LOCATION_HOSPITAL")


def gen_street(lang: str, rng: random.Random) -> Slot:
    s = _rng_pick(rng, STREETS[lang])
    n = rng.randint(1, 250)
    # de/it: "Straße 12"; en/fr: "12 Street". The rng-call order
    # (_rng_pick then randint) is unchanged from the original, so de
    # output is byte-identical.
    if lang in ("en", "fr"):
        return Slot(f"{n} {s}", "LOCATION_STREET")
    return Slot(f"{s} {n}", "LOCATION_STREET")


def gen_zip(lang: str, rng: random.Random) -> Slot:
    """Postcode. de/fr/it are 5-digit numeric; en is a UK outward+inward
    code. The rng draw count differs per locale but each locale is
    internally deterministic; de keeps its single randint draw."""
    if lang == "en":
        out = (f"{_rng_pick(rng, ['SW', 'EC', 'M', 'B', 'LS', 'G'])}"
               f"{rng.randint(1, 99)}")
        inw = (f"{rng.randint(1, 9)}"
               f"{_rng_pick(rng, ['AA', 'BB', 'DE', 'XY'])}")
        return Slot(f"{out} {inw}", "LOCATION_ZIP")
    if lang == "it":
        return Slot(f"{rng.randint(10, 989) * 100:05d}", "LOCATION_ZIP")
    # de / fr — 5-digit numeric.
    return Slot(f"{rng.randint(1000, 99999):05d}", "LOCATION_ZIP")


def gen_phone(lang: str, rng: random.Random) -> Slot:
    """Locale-formatted phone number with international dialling code."""
    if lang == "en":
        return Slot(f"+44 {rng.randint(1000, 7999)} {rng.randint(100000, 999999)}",
                    "CONTACT_PHONE")
    if lang == "fr":
        return Slot(f"+33 {rng.randint(1, 9)} {rng.randint(10, 99)} "
                    f"{rng.randint(10, 99)} {rng.randint(10, 99)} "
                    f"{rng.randint(10, 99)}", "CONTACT_PHONE")
    if lang == "it":
        return Slot(f"+39 0{rng.randint(2, 99)} {rng.randint(1000, 9999999)}",
                    "CONTACT_PHONE")
    # de — +49 (city-code) main-number, mimicking common German formats.
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


def gen_email(lang: str, rng: random.Random) -> Slot:
    first = _rng_pick(rng, FIRST_NAMES_M[lang] + FIRST_NAMES_F[lang]).lower()
    last = _ascii_fold(_rng_pick(rng, LAST_NAMES[lang]).lower()).replace(" ", "")
    return Slot(f"{first}.{last}@{_rng_pick(rng, EMAIL_DOMAINS[lang])}",
                "CONTACT_EMAIL")


def gen_id(lang: str, rng: random.Random) -> Slot:
    """Patient ID — varied formats: numeric, alpha-prefix, dashed.
    Locale-agnostic (identifiers are not localised)."""
    style = rng.random()
    if style < 0.4:
        return Slot(f"{rng.randint(100000, 9999999)}", "ID")
    if style < 0.7:
        return Slot(f"PAT-{rng.randint(10000, 999999)}", "ID")
    return Slot(f"{rng.choice(['HN', 'KL', 'MR'])}{rng.randint(100000, 999999)}",
                "ID")


def gen_age(lang: str, rng: random.Random) -> Slot:
    return Slot(f"{rng.randint(1, 99)}", "AGE")


def gen_profession(lang: str, rng: random.Random) -> Slot:
    return Slot(_rng_pick(rng, PROFESSIONS[lang]), "PROFESSION")


# Dispatch table: slot name -> generator. Every generator takes
# (lang, rng); generate.py binds `lang` at render time.
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
