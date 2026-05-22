#!/usr/bin/env python3
"""Locale-parametrised PII slot generators for synthetic financial text.

This is the SINGLE shared generator for all five synth_finance_{en,de,
es,fr,it} corpora — mirrors the ai4privacy / mapa shared-loader pattern.
The four sibling corpora (synth_finance_{de,es,fr,it}) are thin Makefile
wrappers that invoke this module via ../synth_finance_en/generate.py with
a different --language. There is no per-language copy of the generator.

Design notes that matter for the bench:

  * IBANs are MOD-97 valid (the same algorithm anonde's IBANRecognizer
    uses, analyzer/recognizers/iban.go::validateIBAN). The country code
    matches the corpus locale so DE corpora emit DE IBANs, FR emits FR,
    etc. Real national BBAN lengths are used so the IBAN total length is
    correct per country.
  * Credit-card numbers are Luhn-valid (analyzer/recognizers/checksums.go
    Luhn path). Issued under realistic Visa/Mastercard/Amex prefixes.
  * Account / customer IDs are plain structured identifiers, no checksum.
  * Unlike synth_clinical / finance_de, this generator emits the
    CANONICAL label_map types directly (PERSON, IBAN, ID, ADDRESS,
    ORGANIZATION, DATE, EMAIL, PHONE). The gold: section of
    bench/scoring/label_map.yaml already lists every one as a
    pass-through, so no gold mapping changes are needed and IBAN is
    scored as canonical IBAN (finance_de could not — it reused GraSCCo
    NAME_* labels and had to fold IBAN into ID).
  * Monetary amounts appear in the text for realism but are NOT gold
    spans — anonde's no-monetary-PII rule.

Vocab is intentionally small per locale: variety comes from
combination, not exhaustive listings.
"""

from __future__ import annotations

import random
from dataclasses import dataclass


# ===========================================================================
# Per-locale vocab
# ===========================================================================

# First names, last names, cities, streets, banks, employers — one block
# per supported language. Keys are the --language values.

FIRST_NAMES = {
    "en": [
        "James", "Mary", "Robert", "Patricia", "John", "Jennifer", "Michael",
        "Linda", "David", "Elizabeth", "William", "Barbara", "Richard",
        "Susan", "Joseph", "Jessica", "Thomas", "Sarah", "Charles", "Karen",
        "Daniel", "Nancy", "Matthew", "Lisa", "Anthony", "Margaret", "Mark",
        "Sandra", "Steven", "Ashley", "Andrew", "Emily", "Joshua", "Olivia",
    ],
    "de": [
        "Hans", "Petra", "Klaus", "Maria", "Wolfgang", "Ursula", "Peter",
        "Renate", "Michael", "Sabine", "Thomas", "Andrea", "Andreas",
        "Karin", "Stefan", "Susanne", "Martin", "Gabriele", "Frank",
        "Barbara", "Christian", "Anna", "Sebastian", "Claudia", "Tobias",
        "Julia", "Markus", "Nicole", "Florian", "Stefanie", "Daniel",
        "Sandra", "Alexander", "Christina",
    ],
    "es": [
        "Antonio", "María", "Manuel", "Carmen", "José", "Josefa", "Francisco",
        "Isabel", "David", "Ana", "Juan", "Dolores", "Javier", "Pilar",
        "Daniel", "Teresa", "Carlos", "Rosa", "Jesús", "Cristina", "Alejandro",
        "Marta", "Miguel", "Lucía", "Pablo", "Laura", "Sergio", "Elena",
        "Diego", "Sara", "Pedro", "Paula", "Andrés", "Beatriz",
    ],
    "fr": [
        "Jean", "Marie", "Pierre", "Nathalie", "Michel", "Isabelle", "Alain",
        "Sylvie", "Philippe", "Catherine", "Nicolas", "Françoise", "Christophe",
        "Martine", "Laurent", "Christine", "Stéphane", "Monique", "David",
        "Sandrine", "Julien", "Céline", "Sébastien", "Valérie", "Thomas",
        "Sophie", "Olivier", "Aurélie", "Antoine", "Émilie", "Mathieu",
        "Camille", "Vincent", "Julie",
    ],
    "it": [
        "Giuseppe", "Maria", "Antonio", "Anna", "Giovanni", "Giulia", "Mario",
        "Rosa", "Luigi", "Angela", "Francesco", "Giovanna", "Marco", "Teresa",
        "Andrea", "Lucia", "Roberto", "Carmela", "Stefano", "Caterina",
        "Paolo", "Francesca", "Alessandro", "Elena", "Luca", "Sara",
        "Matteo", "Chiara", "Davide", "Valentina", "Simone", "Federica",
        "Lorenzo", "Martina",
    ],
}

LAST_NAMES = {
    "en": [
        "Smith", "Johnson", "Williams", "Brown", "Jones", "Garcia", "Miller",
        "Davis", "Rodriguez", "Martinez", "Hernandez", "Lopez", "Gonzalez",
        "Wilson", "Anderson", "Thomas", "Taylor", "Moore", "Jackson", "Martin",
        "Lee", "Perez", "Thompson", "White", "Harris", "Clark", "Lewis",
        "Robinson", "Walker", "Young", "Allen", "King", "Wright", "Scott",
    ],
    "de": [
        "Müller", "Schmidt", "Schneider", "Fischer", "Weber", "Meyer",
        "Wagner", "Becker", "Schulz", "Hoffmann", "Schäfer", "Koch", "Bauer",
        "Richter", "Klein", "Wolf", "Schröder", "Neumann", "Schwarz",
        "Zimmermann", "Braun", "Krüger", "Hofmann", "Hartmann", "Lange",
        "Schmitt", "Werner", "Krause", "Lehmann", "Schmid", "Schulze",
        "Maier", "Köhler", "Herrmann",
    ],
    "es": [
        "García", "Rodríguez", "González", "Fernández", "López", "Martínez",
        "Sánchez", "Pérez", "Gómez", "Martín", "Jiménez", "Ruiz", "Hernández",
        "Díaz", "Moreno", "Álvarez", "Muñoz", "Romero", "Alonso", "Gutiérrez",
        "Navarro", "Torres", "Domínguez", "Vázquez", "Ramos", "Gil", "Ramírez",
        "Serrano", "Blanco", "Molina", "Morales", "Suárez", "Ortega", "Castro",
    ],
    "fr": [
        "Martin", "Bernard", "Dubois", "Thomas", "Robert", "Richard",
        "Petit", "Durand", "Leroy", "Moreau", "Simon", "Laurent", "Lefebvre",
        "Michel", "Garcia", "David", "Bertrand", "Roux", "Vincent", "Fournier",
        "Morel", "Girard", "André", "Lefèvre", "Mercier", "Dupont", "Lambert",
        "Bonnet", "François", "Martinez", "Legrand", "Garnier", "Faure",
        "Rousseau",
    ],
    "it": [
        "Rossi", "Russo", "Ferrari", "Esposito", "Bianchi", "Romano",
        "Colombo", "Ricci", "Marino", "Greco", "Bruno", "Gallo", "Conti",
        "De Luca", "Mancini", "Costa", "Giordano", "Rizzo", "Lombardi",
        "Moretti", "Barbieri", "Fontana", "Santoro", "Mariani", "Rinaldi",
        "Caruso", "Ferrara", "Galli", "Martini", "Leone", "Longo", "Gentile",
        "Martinelli", "Vitale",
    ],
}

CITIES = {
    "en": [
        "London", "Manchester", "Birmingham", "Leeds", "Glasgow", "Liverpool",
        "Bristol", "Sheffield", "Edinburgh", "Cardiff", "New York", "Chicago",
        "Boston", "Seattle", "Austin", "Denver", "Portland", "Atlanta",
    ],
    "de": [
        "Berlin", "Hamburg", "München", "Köln", "Frankfurt am Main",
        "Stuttgart", "Düsseldorf", "Leipzig", "Dortmund", "Essen", "Bremen",
        "Dresden", "Hannover", "Nürnberg", "Bonn", "Münster", "Karlsruhe",
        "Mannheim",
    ],
    "es": [
        "Madrid", "Barcelona", "Valencia", "Sevilla", "Zaragoza", "Málaga",
        "Murcia", "Palma", "Bilbao", "Alicante", "Córdoba", "Valladolid",
        "Vigo", "Gijón", "Granada", "A Coruña", "Pamplona", "Santander",
    ],
    "fr": [
        "Paris", "Marseille", "Lyon", "Toulouse", "Nice", "Nantes",
        "Strasbourg", "Montpellier", "Bordeaux", "Lille", "Rennes", "Reims",
        "Toulon", "Grenoble", "Dijon", "Angers", "Nîmes", "Le Havre",
    ],
    "it": [
        "Roma", "Milano", "Napoli", "Torino", "Palermo", "Genova", "Bologna",
        "Firenze", "Bari", "Catania", "Venezia", "Verona", "Messina",
        "Padova", "Trieste", "Brescia", "Parma", "Modena",
    ],
}

# Street templates use a {n} placeholder for the house number.
STREETS = {
    "en": [
        "{n} High Street", "{n} Station Road", "{n} Church Lane",
        "{n} Park Avenue", "{n} Victoria Road", "{n} Mill Lane",
        "{n} Queen Street", "{n} King Street", "{n} Main Street",
        "{n} Oxford Road", "{n} Maple Drive", "{n} Elm Close",
    ],
    "de": [
        "Hauptstraße {n}", "Bahnhofstraße {n}", "Schillerstraße {n}",
        "Goethestraße {n}", "Lindenweg {n}", "Bismarckstraße {n}",
        "Friedrichstraße {n}", "Marktplatz {n}", "Schulstraße {n}",
        "Gartenweg {n}", "Bergstraße {n}", "Rosenstraße {n}",
    ],
    "es": [
        "Calle Mayor {n}", "Calle Real {n}", "Avenida de la Constitución {n}",
        "Calle del Sol {n}", "Gran Vía {n}", "Calle de Alcalá {n}",
        "Paseo de Gracia {n}", "Calle Nueva {n}", "Plaza España {n}",
        "Calle de la Iglesia {n}", "Avenida del Mar {n}", "Calle Sevilla {n}",
    ],
    "fr": [
        "{n} rue de la République", "{n} avenue des Champs",
        "{n} rue Victor Hugo", "{n} boulevard Voltaire", "{n} rue de Paris",
        "{n} rue de la Gare", "{n} place de la Mairie", "{n} rue Nationale",
        "{n} rue Jean Jaurès", "{n} avenue de la Liberté", "{n} rue du Moulin",
        "{n} rue des Écoles",
    ],
    "it": [
        "Via Roma {n}", "Via Garibaldi {n}", "Corso Italia {n}",
        "Via Dante {n}", "Via Mazzini {n}", "Piazza del Duomo {n}",
        "Via Marconi {n}", "Via Verdi {n}", "Via Nazionale {n}",
        "Via San Martino {n}", "Corso Vittorio Emanuele {n}", "Via Veneto {n}",
    ],
}

# Bank names — these become ORGANIZATION gold spans.
BANKS = {
    "en": [
        "Barclays Bank PLC", "HSBC UK Bank plc", "Lloyds Bank plc",
        "NatWest Group plc", "Santander UK plc", "Nationwide Building Society",
        "Standard Chartered Bank", "Metro Bank PLC", "Monzo Bank Ltd",
        "Starling Bank Limited",
    ],
    "de": [
        "Deutsche Bank AG", "Commerzbank AG", "DZ Bank AG", "ING-DiBa AG",
        "Postbank", "Berliner Sparkasse", "HypoVereinsbank", "Targobank",
        "Comdirect Bank", "N26 Bank GmbH",
    ],
    "es": [
        "Banco Santander S.A.", "Banco Bilbao Vizcaya Argentaria S.A.",
        "CaixaBank S.A.", "Banco Sabadell S.A.", "Bankinter S.A.",
        "Unicaja Banco S.A.", "Kutxabank S.A.", "Abanca Corporación Bancaria",
        "ING España", "Openbank S.A.",
    ],
    "fr": [
        "BNP Paribas S.A.", "Crédit Agricole S.A.", "Société Générale S.A.",
        "Groupe BPCE", "Crédit Mutuel", "La Banque Postale", "Boursorama Banque",
        "LCL Banque", "Caisse d'Épargne", "Hello bank!",
    ],
    "it": [
        "Intesa Sanpaolo S.p.A.", "UniCredit S.p.A.", "Banco BPM S.p.A.",
        "BPER Banca S.p.A.", "Banca Monte dei Paschi di Siena S.p.A.",
        "Mediobanca S.p.A.", "Credito Emiliano S.p.A.", "FinecoBank S.p.A.",
        "Banca Sella S.p.A.", "Banca Mediolanum S.p.A.",
    ],
}

# Employer / counterparty companies — also ORGANIZATION gold spans.
EMPLOYERS = {
    "en": [
        "Unilever plc", "BP plc", "Vodafone Group plc", "Tesco plc",
        "GlaxoSmithKline plc", "Rolls-Royce Holdings plc", "BT Group plc",
        "AstraZeneca plc", "Sage Group plc", "Diageo plc",
    ],
    "de": [
        "Siemens AG", "Bosch GmbH", "BMW AG", "Volkswagen AG", "SAP SE",
        "Bayer AG", "BASF SE", "Continental AG", "Henkel AG", "Adidas AG",
    ],
    "es": [
        "Telefónica S.A.", "Iberdrola S.A.", "Repsol S.A.", "Inditex S.A.",
        "Ferrovial S.A.", "ACS Actividades de Construcción", "Naturgy Energy Group",
        "Mapfre S.A.", "Endesa S.A.", "Cellnex Telecom S.A.",
    ],
    "fr": [
        "TotalEnergies SE", "L'Oréal S.A.", "Sanofi S.A.", "Airbus SE",
        "Renault S.A.", "Carrefour S.A.", "Orange S.A.", "Danone S.A.",
        "Capgemini SE", "Michelin S.A.",
    ],
    "it": [
        "Enel S.p.A.", "Eni S.p.A.", "Stellantis Italia S.p.A.",
        "Leonardo S.p.A.", "Telecom Italia S.p.A.", "Generali Assicurazioni",
        "Ferrari S.p.A.", "Pirelli & C. S.p.A.", "Prysmian S.p.A.",
        "Luxottica Group S.p.A.",
    ],
}

PROFESSIONS = {
    "en": [
        "teacher", "engineer", "nurse", "accountant", "software developer",
        "consultant", "architect", "sales manager", "retired", "self-employed",
        "lawyer", "project manager", "electrician", "student",
    ],
    "de": [
        "Lehrer", "Ingenieurin", "Krankenpfleger", "Steuerberater",
        "Softwareentwickler", "Unternehmensberaterin", "Architekt",
        "Vertriebsleiter", "Rentner", "Selbstständig", "Rechtsanwältin",
        "Projektleiter", "Elektriker", "Studentin",
    ],
    "es": [
        "profesor", "ingeniera", "enfermero", "contable",
        "desarrolladora de software", "consultor", "arquitecta",
        "jefe de ventas", "jubilado", "autónomo", "abogada",
        "jefe de proyecto", "electricista", "estudiante",
    ],
    "fr": [
        "enseignant", "ingénieure", "infirmier", "comptable",
        "développeuse logiciel", "consultant", "architecte",
        "directrice commerciale", "retraité", "indépendant", "avocate",
        "chef de projet", "électricien", "étudiante",
    ],
    "it": [
        "insegnante", "ingegnere", "infermiere", "commercialista",
        "sviluppatrice software", "consulente", "architetto",
        "responsabile vendite", "pensionato", "lavoratore autonomo",
        "avvocata", "project manager", "elettricista", "studentessa",
    ],
}

# Email domains — locale-flavoured free-mail plus a couple of corporate ones.
EMAIL_DOMAINS = {
    "en": ["gmail.com", "outlook.com", "yahoo.com", "btinternet.com",
           "icloud.com", "hotmail.co.uk"],
    "de": ["gmail.com", "web.de", "gmx.de", "t-online.de", "outlook.de",
           "posteo.de"],
    "es": ["gmail.com", "hotmail.es", "yahoo.es", "outlook.es", "telefonica.net",
           "icloud.com"],
    "fr": ["gmail.com", "orange.fr", "free.fr", "outlook.fr", "laposte.net",
           "sfr.fr"],
    "it": ["gmail.com", "libero.it", "virgilio.it", "alice.it", "outlook.it",
           "tiscali.it"],
}

# IBAN national specs: ISO 3166 country code -> BBAN length (digits only).
# We only emit numeric BBANs so the MOD-97 conversion stays simple; all
# five locales have national-significant numeric BBANs of these lengths.
#   DE 18, GB 18 (we use a numeric stand-in for the 4-letter bank code is
#   NOT valid — GB BBAN has 4 letters; to keep MOD-97 honest for the EN
#   corpus we issue an Irish IE IBAN instead, also 4 letters... so for EN
#   we use a numeric-friendly country. See _IBAN_SPEC notes below.
_IBAN_SPEC = {
    # locale -> (country_code, bban_numeric_length)
    # All chosen so the BBAN is purely numeric → MOD-97 stays exact.
    "de": ("DE", 18),   # 8 BLZ + 10 account
    "es": ("ES", 20),   # 4 bank + 4 branch + 2 check + 10 account
    "fr": ("FR", 23),   # 5 bank + 5 branch + 11 account + 2 RIB key
    "it": ("IT", 23),   # 1 CIN letter + 5 ABI + 5 CAB + 12 account
    # GB IBANs have a 4-LETTER bank code, which is fine for MOD-97 (letters
    # are converted too) but the EN corpus is "English-language", not
    # UK-specific. We issue Irish IE IBANs for EN: IE has a 4-letter bank
    # code + 14 digits. Handled specially in gen_iban.
    "en": ("IE", 0),
}

# Credit-card issuer prefixes (IIN ranges) and total length.
_CARD_SPEC = [
    ("4", 16),       # Visa
    ("51", 16),      # Mastercard
    ("52", 16),      # Mastercard
    ("53", 16),      # Mastercard
    ("55", 16),      # Mastercard
    ("34", 15),      # Amex
    ("37", 15),      # Amex
]


# ===========================================================================
# Slot dataclass + helpers
# ===========================================================================


@dataclass
class Slot:
    """One slot fill: surface string + canonical label_map gold type."""

    text: str
    type: str


def _pick(rng: random.Random, xs: list):
    return xs[rng.randrange(len(xs))]


def _ascii_fold(s: str) -> str:
    table = {
        "ü": "ue", "Ü": "Ue", "ö": "oe", "Ö": "Oe", "ä": "ae", "Ä": "Ae",
        "ß": "ss", "á": "a", "à": "a", "â": "a", "é": "e", "è": "e", "ê": "e",
        "í": "i", "ì": "i", "î": "i", "ó": "o", "ò": "o", "ô": "o", "ú": "u",
        "ù": "u", "û": "u", "ñ": "n", "ç": "c", "É": "E", "À": "A",
    }
    return "".join(table.get(ch, ch) for ch in s)


# ===========================================================================
# Checksum helpers — MOD-97 (IBAN) and Luhn (credit card)
# ===========================================================================


def _iban_check_digits(country: str, bban: str) -> str:
    """Two MOD-97 check digits for an IBAN.

    Mirrors validateIBAN in analyzer/recognizers/iban.go: move the
    country code + "00" to the end, map A-Z to 10-35, take mod 97, the
    check digits are 98 - mod (zero-padded to 2)."""
    rearranged = bban + country + "00"
    numeric: list[str] = []
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


def _luhn_check_digit(partial: str) -> str:
    """Compute the Luhn check digit that makes `partial` + digit valid.

    `partial` is the card number minus its final digit."""
    total = 0
    # The check digit will be appended, so the partial digits are at
    # positions that, counting the (future) check digit as position 1
    # from the right, double every second one starting from position 2.
    for i, ch in enumerate(reversed(partial)):
        d = int(ch)
        if i % 2 == 0:  # positions 2,4,... from the right of the full number
            d *= 2
            if d > 9:
                d -= 9
        total += d
    return str((10 - total % 10) % 10)


# ===========================================================================
# Generators — each returns Slot(text, canonical_type)
# ===========================================================================


def gen_person(lang: str, rng: random.Random) -> Slot:
    first = _pick(rng, FIRST_NAMES[lang])
    last = _pick(rng, LAST_NAMES[lang])
    return Slot(f"{first} {last}", "PERSON")


def gen_organization_bank(lang: str, rng: random.Random) -> Slot:
    return Slot(_pick(rng, BANKS[lang]), "ORGANIZATION")


def gen_organization_employer(lang: str, rng: random.Random) -> Slot:
    return Slot(_pick(rng, EMPLOYERS[lang]), "ORGANIZATION")


def gen_address(lang: str, rng: random.Random) -> Slot:
    """Full street address: street + house number + postcode + city, as
    one ADDRESS span. Postcode formats are locale-specific."""
    street = _pick(rng, STREETS[lang]).format(n=rng.randint(1, 250))
    city = _pick(rng, CITIES[lang])
    if lang == "en":
        # UK-style outward+inward postcode.
        pc = (f"{_pick(rng, ['SW', 'EC', 'M', 'B', 'LS', 'G'])}"
              f"{rng.randint(1, 99)} {rng.randint(1, 9)}"
              f"{_pick(rng, ['AA', 'BB', 'DE', 'XY'])}")
        return Slot(f"{street}, {city} {pc}", "ADDRESS")
    if lang == "de":
        pc = f"{rng.randint(1000, 99999):05d}"
        return Slot(f"{street}, {pc} {city}", "ADDRESS")
    if lang == "es":
        pc = f"{rng.randint(1000, 52999):05d}"
        return Slot(f"{street}, {pc} {city}", "ADDRESS")
    if lang == "fr":
        pc = f"{rng.randint(1000, 95999):05d}"
        return Slot(f"{street}, {pc} {city}", "ADDRESS")
    if lang == "it":
        pc = f"{rng.randint(10, 989) * 100:05d}"
        return Slot(f"{street}, {pc} {city}", "ADDRESS")
    raise ValueError(lang)


def gen_date(lang: str, rng: random.Random) -> Slot:
    """Locale-formatted date. EN uses YYYY-MM-DD or "D Month YYYY";
    DE/ES/FR/IT use DD.MM.YYYY or DD/MM/YYYY."""
    day = rng.randint(1, 28)
    month = rng.randint(1, 12)
    year = rng.randint(2018, 2026)
    months = {
        "en": ["January", "February", "March", "April", "May", "June", "July",
               "August", "September", "October", "November", "December"],
        "de": ["Januar", "Februar", "März", "April", "Mai", "Juni", "Juli",
               "August", "September", "Oktober", "November", "Dezember"],
        "es": ["enero", "febrero", "marzo", "abril", "mayo", "junio", "julio",
               "agosto", "septiembre", "octubre", "noviembre", "diciembre"],
        "fr": ["janvier", "février", "mars", "avril", "mai", "juin", "juillet",
               "août", "septembre", "octobre", "novembre", "décembre"],
        "it": ["gennaio", "febbraio", "marzo", "aprile", "maggio", "giugno",
               "luglio", "agosto", "settembre", "ottobre", "novembre",
               "dicembre"],
    }
    r = rng.random()
    if lang == "en":
        if r < 0.5:
            return Slot(f"{year}-{month:02d}-{day:02d}", "DATE")
        return Slot(f"{day} {months['en'][month-1]} {year}", "DATE")
    # de/es/fr/it
    if r < 0.5:
        return Slot(f"{day:02d}.{month:02d}.{year}", "DATE")
    if r < 0.8:
        return Slot(f"{day:02d}/{month:02d}/{year}", "DATE")
    if lang == "de":
        return Slot(f"{day}. {months['de'][month-1]} {year}", "DATE")
    if lang in ("es", "fr", "it"):
        de_word = {"es": "de", "fr": "", "it": ""}
        if lang == "es":
            return Slot(f"{day} de {months['es'][month-1]} de {year}", "DATE")
        return Slot(f"{day} {months[lang][month-1]} {year}", "DATE")
    raise ValueError(lang)


def gen_email(lang: str, rng: random.Random) -> Slot:
    first = _ascii_fold(_pick(rng, FIRST_NAMES[lang])).lower()
    last = _ascii_fold(_pick(rng, LAST_NAMES[lang])).lower().replace(" ", "")
    sep = _pick(rng, [".", "_", ""])
    domain = _pick(rng, EMAIL_DOMAINS[lang])
    return Slot(f"{first}{sep}{last}@{domain}", "EMAIL")


def gen_phone(lang: str, rng: random.Random) -> Slot:
    """Locale-formatted phone number with international dialling code."""
    if lang == "en":
        return Slot(f"+44 {rng.randint(1000, 7999)} {rng.randint(100000, 999999)}",
                    "PHONE")
    if lang == "de":
        cc = _pick(rng, ["30", "40", "89", "221", "69", "711"])
        return Slot(f"+49 {cc} {rng.randint(1000000, 99999999)}", "PHONE")
    if lang == "es":
        return Slot(f"+34 {rng.randint(600, 999)} {rng.randint(100, 999)} "
                    f"{rng.randint(100, 999)}", "PHONE")
    if lang == "fr":
        return Slot(f"+33 {rng.randint(1, 9)} {rng.randint(10, 99)} "
                    f"{rng.randint(10, 99)} {rng.randint(10, 99)} "
                    f"{rng.randint(10, 99)}", "PHONE")
    if lang == "it":
        return Slot(f"+39 0{rng.randint(2, 99)} {rng.randint(1000, 9999999)}",
                    "PHONE")
    raise ValueError(lang)


def gen_iban(lang: str, rng: random.Random) -> Slot:
    """Locale IBAN with valid MOD-97 check digits, emitted as canonical
    type IBAN.

    For EN we issue an Irish (IE) IBAN — IE has a 4-letter bank code and
    is the natural euro-area, English-language choice (the UK is not in
    SEPA-IBAN-by-default and GB IBANs would still be 4-letter-bank-code
    anyway). The 4 letters are MOD-97-converted so the checksum stays
    honest."""
    country, _bban_len = _IBAN_SPEC[lang]
    if country == "IE":
        # IE: 4-letter bank code + 6-digit sort code + 8-digit account.
        bank = "".join(chr(ord("A") + rng.randrange(26)) for _ in range(4))
        bban = bank + f"{rng.randint(0, 999999):06d}" + \
            f"{rng.randint(0, 99999999):08d}"
    else:
        _country, bban_len = country, _bban_len
        bban = "".join(str(rng.randrange(10)) for _ in range(bban_len))
    check = _iban_check_digits(country, bban)
    return Slot(f"{country}{check}{bban}", "IBAN")


def gen_credit_card(lang: str, rng: random.Random) -> Slot:
    """Luhn-valid credit-card number, canonical type ID.

    Formatted with spaces in groups of 4 (Amex 4-6-5) about half the
    time so both spaced and bare forms appear."""
    prefix, length = _pick(rng, _CARD_SPEC)
    body_len = length - len(prefix) - 1  # minus prefix, minus check digit
    body = "".join(str(rng.randrange(10)) for _ in range(body_len))
    partial = prefix + body
    digits = partial + _luhn_check_digit(partial)
    if rng.random() < 0.5:
        if length == 15:  # Amex 4-6-5
            spaced = f"{digits[:4]} {digits[4:10]} {digits[10:]}"
        else:             # 4-4-4-4
            spaced = " ".join(digits[i:i + 4] for i in range(0, 16, 4))
        return Slot(spaced, "ID")
    return Slot(digits, "ID")


def gen_account_id(lang: str, rng: random.Random) -> Slot:
    """Account / customer / reference identifier — no checksum, canonical
    type ID. Varied shapes per locale-agnostic style."""
    style = rng.random()
    if style < 0.35:
        return Slot(f"{rng.randint(10000000, 999999999)}", "ID")
    if style < 0.7:
        prefix = _pick(rng, ["ACC", "CUS", "REF", "KD", "CLT"])
        return Slot(f"{prefix}-{rng.randint(100000, 9999999)}", "ID")
    return Slot(f"{_pick(rng, ['A', 'C', 'X'])}"
                f"{rng.randint(1000000, 99999999)}", "ID")


def gen_profession(lang: str, rng: random.Random) -> Slot:
    return Slot(_pick(rng, PROFESSIONS[lang]), "PROFESSION")


# Dispatch table: slot name -> generator. Every generator takes
# (lang, rng). generate.py binds `lang` at render time.
GENERATORS = {
    "PERSON": gen_person,
    "ORG_BANK": gen_organization_bank,
    "ORG_EMPLOYER": gen_organization_employer,
    "ADDRESS": gen_address,
    "DATE": gen_date,
    "EMAIL": gen_email,
    "PHONE": gen_phone,
    "IBAN": gen_iban,
    "CREDIT_CARD": gen_credit_card,
    "ACCOUNT_ID": gen_account_id,
    "PROFESSION": gen_profession,
}
