#!/usr/bin/env python3
"""Locale-parametrised templates for synthetic clinical documents.

Four clinical sublanguages per language: ED triage notes, OP reports,
radiology findings, rehab discharge letters. Each sublanguage has a
header pool, a body pool (paragraphs sampled without replacement), and a
footer pool.

A document is assembled by picking 1 header, N body paragraphs, and 1
footer, then filling slots of the form `{SLOT_NAME}` with values from
generators.py. Slot names match GraSCCo PHI conventions so the existing
label_map.yaml gold: section maps them with no changes.

TEMPLATES is keyed by language code (de/en/fr/it); each value is a dict
of sublanguage -> (headers, body, footers). There is deliberately NO es
key — Spanish clinical PHI is covered by the real-gold MEDDOCAN corpus
(bench/corpora/meddocan_es), not by this generator.

REGRESSION CONTRACT: the de templates below are copied byte-for-byte
from the pre-refactor single-language templates.py. With `--language de`
(the default) the generator walks exactly these strings, so a fixed
seed reproduces the historical synth_clinical corpus.
"""

from __future__ import annotations

# ===========================================================================
# German — verbatim copy of the pre-refactor templates.
# ===========================================================================

_DE_ED_TRIAGE_HEADERS = [
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

_DE_ED_TRIAGE_BODY = [
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

_DE_ED_TRIAGE_FOOTERS = [
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

_DE_OP_HEADERS = [
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

_DE_OP_BODY = [
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

_DE_OP_FOOTERS = [
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

_DE_RADIO_HEADERS = [
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

_DE_RADIO_BODY = [
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

_DE_RADIO_FOOTERS = [
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

_DE_REHAB_HEADERS = [
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

_DE_REHAB_BODY = [
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

_DE_REHAB_FOOTERS = [
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

_DE = {
    "ed_triage": (_DE_ED_TRIAGE_HEADERS, _DE_ED_TRIAGE_BODY, _DE_ED_TRIAGE_FOOTERS),
    "op_report": (_DE_OP_HEADERS, _DE_OP_BODY, _DE_OP_FOOTERS),
    "radiology": (_DE_RADIO_HEADERS, _DE_RADIO_BODY, _DE_RADIO_FOOTERS),
    "rehab_discharge": (_DE_REHAB_HEADERS, _DE_REHAB_BODY, _DE_REHAB_FOOTERS),
}


# ===========================================================================
# English
# ===========================================================================

_EN_ED_TRIAGE_HEADERS = [
    """{LOCATION_HOSPITAL}
Emergency Department — Triage Note

Patient: {NAME_PATIENT}, DOB {DATE_BIRTH}
Arrival: {DATE}, {TIME}
Patient ID: {ID}
Triage category: {TRIAGE_LEVEL}
""",
    """Emergency Department First Contact — {LOCATION_HOSPITAL}

Presenting: {NAME_PATIENT} ({AGE} years, DOB {DATE_BIRTH})
Date/time: {DATE} {TIME}
Accompanied by: {NAME_RELATIVE}
ID: {ID}
""",
]

_EN_ED_TRIAGE_BODY = [
    "Chief complaint: acute chest pain since the morning of {DATE}, radiating to the left arm.",
    "Brought in by ambulance, handed over by the dispatch desk at {TIME}.",
    "History per patient: fell from a bicycle, impact with helmet on; brief loss of consciousness.",
    "Vital signs: BP 145/90 mmHg, HR 102/min, SpO2 96% on room air, temperature 37.8 °C.",
    "Allergies: no known drug allergies.",
    "Regular medication per patient: ramipril 5 mg, aspirin 100 mg, metformin 850 mg.",
    "Examination: alert patient, oriented in all spheres, no neck stiffness.",
    "Accompanied by a relative ({NAME_RELATIVE}) who is present.",
    "GP informed in advance: {NAME_DOCTOR}, practice in {LOCATION_CITY}.",
    "Insurance status: NHS, membership ID not available.",
    "Triage decision by {NAME_DOCTOR}: inpatient admission required.",
    "Results discussion scheduled with {NAME_DOCTOR} from {TIME}.",
    "Patient lives alone at {LOCATION_STREET}, {LOCATION_CITY} {LOCATION_ZIP}.",
    "Next-of-kin contact number: {CONTACT_PHONE}.",
]

_EN_ED_TRIAGE_FOOTERS = [
    """
Examined by: {NAME_DOCTOR}
Nursing: {NAME_DOCTOR}
Queries: {CONTACT_PHONE} or {CONTACT_EMAIL}
""",
    """
Triage completed on {DATE} at {TIME}.
Responsible clinician: {NAME_DOCTOR}
Emergency Department contact: {CONTACT_PHONE}
""",
]

_EN_OP_HEADERS = [
    """{LOCATION_HOSPITAL}
Department of General and Visceral Surgery

Operative Report

Patient: {NAME_PATIENT}, DOB {DATE_BIRTH}, Patient ID {ID}
Date of surgery: {DATE}
Surgeon: {NAME_DOCTOR}
First assistant: {NAME_DOCTOR}
Anaesthetist: {NAME_DOCTOR}
""",
    """Operative Report — {LOCATION_HOSPITAL}

Date of surgery: {DATE}
Start time: {TIME}
Patient: {NAME_PATIENT} ({AGE} years)
ID: {ID}
Surgeon: {NAME_DOCTOR}
Assistant: {NAME_DOCTOR}
""",
]

_EN_OP_BODY = [
    "Indication: symptomatic cholecystolithiasis with recurrent colic.",
    "Preoperative diagnosis: right inguinal hernia, painful since {DATE}.",
    "Prior records from {LOCATION_HOSPITAL} are on file.",
    "Consent: detailed written consent obtained the day before by {NAME_DOCTOR}.",
    "Positioning: supine, both arms tucked.",
    "Skin disinfection and sterile draping in the usual fashion.",
    "Incision: median upper-abdominal incision, approximately 12 cm long.",
    "Entry into the abdomen in anatomical layers without complication.",
    "Inspection: no ascites, smooth liver surface.",
    "Dissection of the gallbladder with exposure of the cystic duct and cystic artery.",
    "Clip ligation and division of both structures.",
    "Retrieval of the gallbladder in a specimen bag.",
    "Histology: tissue sent to Pathology at {LOCATION_HOSPITAL} (order {ID}).",
    "Layered wound closure, intracutaneous skin suture with Monocryl 3-0.",
    "End of surgery: {TIME}; patient transferred to recovery in a stable condition.",
    "Postoperative orders discussed with ward doctor {NAME_DOCTOR}.",
]

_EN_OP_FOOTERS = [
    """
{LOCATION_CITY}, {DATE}

___________________
{NAME_DOCTOR}, Surgeon
""",
    """
Surgeon: {NAME_DOCTOR}
Dictated: {NAME_DOCTOR} on {DATE}
Report no.: {ID}
""",
]

_EN_RADIO_HEADERS = [
    """{LOCATION_HOSPITAL}
Department of Diagnostic and Interventional Radiology

Report

Patient: {NAME_PATIENT}, DOB {DATE_BIRTH}
Patient ID: {ID}
Examination: CT chest with contrast
Date of examination: {DATE}, {TIME}
Referring clinician: {NAME_DOCTOR}
""",
    """Radiology Report — {LOCATION_HOSPITAL}

Patient: {NAME_PATIENT}
Date of birth: {DATE_BIRTH}
ID: {ID}
Modality: MRI head, unenhanced + contrast
Date: {DATE}
Reporting clinician: {NAME_DOCTOR}
""",
]

_EN_RADIO_BODY = [
    "Clinical details from the referring clinician {NAME_DOCTOR}: query central pulmonary embolism.",
    "Prior study from {DATE} at {LOCATION_HOSPITAL} is available for comparison.",
    "Technique: multidetector CT, 80 ml of iomeprol 350 IV, slice thickness 1 mm.",
    "Lung findings: no space-occupying lesions, no pleural effusions bilaterally.",
    "Mediastinum: unremarkable configuration, no pathologically enlarged lymph nodes.",
    "Heart: normal size, no pericardial effusion.",
    "Skeleton: degenerative changes of the thoracic spine, otherwise age-appropriate.",
    "Head: normal grey-white matter differentiation, no intracranial haemorrhage.",
    "Impression: no evidence of central pulmonary embolism.",
    "Recommendation: follow-up imaging in 6 months or sooner if clinically indicated.",
    "Findings discussed with {NAME_DOCTOR} on {DATE} at {TIME}.",
]

_EN_RADIO_FOOTERS = [
    """
{LOCATION_CITY}, {DATE}

{NAME_DOCTOR}
Consultant Radiologist

Queries: {CONTACT_PHONE}, {CONTACT_EMAIL}
""",
    """
Report dictated by: {NAME_DOCTOR}
Transcribed by: {NAME_DOCTOR}
Report finalised on {DATE} at {TIME}
""",
]

_EN_REHAB_HEADERS = [
    """{LOCATION_HOSPITAL}
Department of Orthopaedic Rehabilitation

Discharge Summary

Patient: {NAME_PATIENT}, DOB {DATE_BIRTH}
Occupation: {PROFESSION}
Resident at: {LOCATION_STREET}, {LOCATION_CITY} {LOCATION_ZIP}
Patient ID: {ID}
Admission: {DATE}
Discharge: {DATE}
Ward doctor: {NAME_DOCTOR}
""",
    """Rehabilitation Discharge Summary

Hospital: {LOCATION_HOSPITAL}
Patient: {NAME_PATIENT} ({AGE} years)
ID no.: {ID}
Main diagnosis: status post right total hip replacement
Admission: {DATE} — Discharge: {DATE}
Treating clinician: {NAME_DOCTOR}
""",
]

_EN_REHAB_BODY = [
    "Reason for admission: post-acute rehabilitation following surgery at {LOCATION_HOSPITAL}.",
    "History: the {AGE}-year-old patient was an inpatient at {LOCATION_HOSPITAL} until {DATE}.",
    "Occupational history: worked as a {PROFESSION} until the onset of illness.",
    "Family history: father died of myocardial infarction. Spouse: {NAME_RELATIVE}.",
    "Social history: lives with a spouse in {LOCATION_CITY}, stairs with no lift.",
    "Prior records: operative report from {NAME_DOCTOR} dated {DATE} is on file.",
    "Treatment plan: physiotherapy twice daily, occupational therapy as required.",
    "Course: steady mobilisation progress under the guidance of {NAME_DOCTOR}.",
    "Weight-bearing: pain-adapted full weight-bearing possible from {DATE}.",
    "Social work: appointment with the hospital social services on {DATE} at {TIME}.",
    "Recommendation: outpatient follow-up with the GP {NAME_DOCTOR} in {LOCATION_CITY}.",
    "Medication on discharge: aspirin 100 mg in the morning, pantoprazole 20 mg in the morning.",
    "Aids: forearm crutches for a further 4 weeks.",
    "Patient contact details for follow-up: {CONTACT_PHONE}, {CONTACT_EMAIL}.",
]

_EN_REHAB_FOOTERS = [
    """
Yours sincerely

{NAME_DOCTOR}
Lead Consultant
{LOCATION_HOSPITAL}
Tel.: {CONTACT_PHONE}
""",
    """
{LOCATION_CITY}, {DATE}

{NAME_DOCTOR}              {NAME_DOCTOR}
Ward Doctor               Consultant

Queries: {CONTACT_PHONE} / {CONTACT_EMAIL}
""",
]

_EN = {
    "ed_triage": (_EN_ED_TRIAGE_HEADERS, _EN_ED_TRIAGE_BODY, _EN_ED_TRIAGE_FOOTERS),
    "op_report": (_EN_OP_HEADERS, _EN_OP_BODY, _EN_OP_FOOTERS),
    "radiology": (_EN_RADIO_HEADERS, _EN_RADIO_BODY, _EN_RADIO_FOOTERS),
    "rehab_discharge": (_EN_REHAB_HEADERS, _EN_REHAB_BODY, _EN_REHAB_FOOTERS),
}


# ===========================================================================
# French
# ===========================================================================

_FR_ED_TRIAGE_HEADERS = [
    """{LOCATION_HOSPITAL}
Service des Urgences — Note de triage

Patient : {NAME_PATIENT}, né(e) le {DATE_BIRTH}
Arrivée : {DATE}, {TIME}
Identifiant patient : {ID}
Niveau de triage : {TRIAGE_LEVEL}
""",
    """Premier contact aux Urgences — {LOCATION_HOSPITAL}

Présenté(e) : {NAME_PATIENT} ({AGE} ans, né(e) le {DATE_BIRTH})
Date/heure : {DATE} {TIME}
Accompagnant : {NAME_RELATIVE}
Identifiant : {ID}
""",
]

_FR_ED_TRIAGE_BODY = [
    "Motif principal : douleur thoracique aiguë depuis le matin du {DATE}, irradiant vers le bras gauche.",
    "Amené(e) par les services de secours, pris(e) en charge par la régulation à {TIME}.",
    "Anamnèse selon le patient : chute de vélo, choc avec casque ; brève perte de connaissance.",
    "Constantes : TA 145/90 mmHg, FC 102/min, SpO2 96 % en air ambiant, température 37,8 °C.",
    "Allergies : aucune allergie médicamenteuse connue.",
    "Traitement habituel selon le patient : ramipril 5 mg, aspirine 100 mg, metformine 850 mg.",
    "Examen : patient éveillé, orienté dans toutes les sphères, pas de raideur de nuque.",
    "Accompagné d'un proche ({NAME_RELATIVE}) présent sur place.",
    "Médecin traitant informé au préalable : {NAME_DOCTOR}, cabinet à {LOCATION_CITY}.",
    "Statut d'assurance : Assurance Maladie, numéro d'adhérent non disponible.",
    "Décision de triage par {NAME_DOCTOR} : hospitalisation nécessaire.",
    "Entretien de restitution prévu avec {NAME_DOCTOR} à partir de {TIME}.",
    "Le patient vit seul au {LOCATION_STREET}, {LOCATION_ZIP} {LOCATION_CITY}.",
    "Numéro de contact du proche : {CONTACT_PHONE}.",
]

_FR_ED_TRIAGE_FOOTERS = [
    """
Examiné par : {NAME_DOCTOR}
Soins infirmiers : {NAME_DOCTOR}
Questions : {CONTACT_PHONE} ou {CONTACT_EMAIL}
""",
    """
Triage terminé le {DATE} à {TIME}.
Responsable : {NAME_DOCTOR}
Contact des Urgences : {CONTACT_PHONE}
""",
]

_FR_OP_HEADERS = [
    """{LOCATION_HOSPITAL}
Service de Chirurgie Générale et Viscérale

Compte rendu opératoire

Patient : {NAME_PATIENT}, né(e) le {DATE_BIRTH}, Identifiant {ID}
Date de l'intervention : {DATE}
Chirurgien : {NAME_DOCTOR}
Premier assistant : {NAME_DOCTOR}
Anesthésiste : {NAME_DOCTOR}
""",
    """Compte rendu opératoire — {LOCATION_HOSPITAL}

Date de l'intervention : {DATE}
Heure de début : {TIME}
Patient : {NAME_PATIENT} ({AGE} ans)
Identifiant : {ID}
Chirurgien : {NAME_DOCTOR}
Assistant : {NAME_DOCTOR}
""",
]

_FR_OP_BODY = [
    "Indication : cholécystolithiase symptomatique avec coliques récidivantes.",
    "Diagnostic préopératoire : hernie inguinale droite, douloureuse depuis le {DATE}.",
    "Antécédents de l'établissement {LOCATION_HOSPITAL} disponibles au dossier.",
    "Information : consentement écrit détaillé recueilli la veille par {NAME_DOCTOR}.",
    "Installation : décubitus dorsal, les deux bras le long du corps.",
    "Désinfection cutanée et champage stérile selon les usages.",
    "Incision : laparotomie médiane sus-ombilicale, environ 12 cm de longueur.",
    "Ouverture de l'abdomen plan par plan sans complication.",
    "Inspection : pas d'ascite, surface hépatique lisse.",
    "Dissection de la vésicule biliaire avec exposition du canal cystique et de l'artère cystique.",
    "Ligature par clips et section des deux structures.",
    "Extraction de la vésicule dans un sac de prélèvement.",
    "Histologie : tissu adressé au service d'Anatomopathologie de {LOCATION_HOSPITAL} (demande {ID}).",
    "Fermeture pariétale plan par plan, suture cutanée intradermique au Monocryl 3-0.",
    "Fin de l'intervention : {TIME} ; patient transféré stable en salle de réveil.",
    "Prescriptions postopératoires discutées avec le médecin de l'unité {NAME_DOCTOR}.",
]

_FR_OP_FOOTERS = [
    """
{LOCATION_CITY}, le {DATE}

___________________
{NAME_DOCTOR}, Chirurgien
""",
    """
Chirurgien : {NAME_DOCTOR}
Dicté : {NAME_DOCTOR} le {DATE}
N° de compte rendu : {ID}
""",
]

_FR_RADIO_HEADERS = [
    """{LOCATION_HOSPITAL}
Service de Radiologie Diagnostique et Interventionnelle

Compte rendu

Patient : {NAME_PATIENT}, né(e) le {DATE_BIRTH}
Identifiant patient : {ID}
Examen : scanner thoracique avec produit de contraste
Date de l'examen : {DATE}, {TIME}
Médecin prescripteur : {NAME_DOCTOR}
""",
    """Compte rendu de radiologie — {LOCATION_HOSPITAL}

Patient : {NAME_PATIENT}
Date de naissance : {DATE_BIRTH}
Identifiant : {ID}
Modalité : IRM cérébrale sans puis avec injection
Date : {DATE}
Médecin radiologue : {NAME_DOCTOR}
""",
]

_FR_RADIO_BODY = [
    "Renseignements cliniques du médecin prescripteur {NAME_DOCTOR} : suspicion d'embolie pulmonaire centrale.",
    "Examen antérieur du {DATE} réalisé à {LOCATION_HOSPITAL} disponible pour comparaison.",
    "Technique : scanner multidétecteur, 80 ml d'ioméprol 350 IV, épaisseur de coupe 1 mm.",
    "Champ pulmonaire : pas de processus expansif, pas d'épanchement pleural bilatéral.",
    "Médiastin : configuration sans particularité, pas d'adénopathie pathologique.",
    "Cœur : taille normale, pas d'épanchement péricardique.",
    "Squelette : remaniements dégénératifs du rachis dorsal, sinon en rapport avec l'âge.",
    "Encéphale : différenciation substance grise/blanche normale, pas d'hémorragie intracrânienne.",
    "Conclusion : pas d'argument pour une embolie pulmonaire centrale.",
    "Recommandation : contrôle d'imagerie à 6 mois ou plus tôt si indication clinique.",
    "Résultats discutés avec {NAME_DOCTOR} le {DATE} à {TIME}.",
]

_FR_RADIO_FOOTERS = [
    """
{LOCATION_CITY}, le {DATE}

{NAME_DOCTOR}
Radiologue

Questions : {CONTACT_PHONE}, {CONTACT_EMAIL}
""",
    """
Compte rendu dicté par : {NAME_DOCTOR}
Transcrit par : {NAME_DOCTOR}
Compte rendu finalisé le {DATE} à {TIME}
""",
]

_FR_REHAB_HEADERS = [
    """{LOCATION_HOSPITAL}
Service de Rééducation Orthopédique

Compte rendu de sortie

Patient : {NAME_PATIENT}, né(e) le {DATE_BIRTH}
Profession : {PROFESSION}
Domicile : {LOCATION_STREET}, {LOCATION_ZIP} {LOCATION_CITY}
Identifiant patient : {ID}
Admission : {DATE}
Sortie : {DATE}
Médecin de l'unité : {NAME_DOCTOR}
""",
    """Compte rendu de sortie de rééducation

Établissement : {LOCATION_HOSPITAL}
Patient : {NAME_PATIENT} ({AGE} ans)
N° d'identifiant : {ID}
Diagnostic principal : suites de prothèse totale de hanche droite
Admission : {DATE} — Sortie : {DATE}
Médecin référent : {NAME_DOCTOR}
""",
]

_FR_REHAB_BODY = [
    "Motif d'admission : soins de suite après intervention chirurgicale à {LOCATION_HOSPITAL}.",
    "Anamnèse : le patient de {AGE} ans a été hospitalisé à {LOCATION_HOSPITAL} jusqu'au {DATE}.",
    "Antécédents professionnels : a exercé en tant que {PROFESSION} jusqu'au début de la maladie.",
    "Antécédents familiaux : père décédé d'un infarctus du myocarde. Conjoint : {NAME_RELATIVE}.",
    "Situation sociale : vit avec son conjoint à {LOCATION_CITY}, escaliers sans ascenseur.",
    "Antécédents : compte rendu opératoire du {NAME_DOCTOR} daté du {DATE} au dossier.",
    "Plan de traitement : kinésithérapie deux fois par jour, ergothérapie selon les besoins.",
    "Évolution : progrès réguliers de la mobilisation sous la supervision du {NAME_DOCTOR}.",
    "Mise en charge : appui complet adapté à la douleur possible à partir du {DATE}.",
    "Accompagnement social : rendez-vous avec le service social de l'établissement le {DATE} à {TIME}.",
    "Recommandation : suivi ambulatoire avec le médecin traitant {NAME_DOCTOR} à {LOCATION_CITY}.",
    "Traitement à la sortie : aspirine 100 mg le matin, pantoprazole 20 mg le matin.",
    "Aides techniques : cannes anglaises pour 4 semaines supplémentaires.",
    "Coordonnées du patient pour le suivi : {CONTACT_PHONE}, {CONTACT_EMAIL}.",
]

_FR_REHAB_FOOTERS = [
    """
Confraternellement

{NAME_DOCTOR}
Médecin chef de service
{LOCATION_HOSPITAL}
Tél. : {CONTACT_PHONE}
""",
    """
{LOCATION_CITY}, le {DATE}

{NAME_DOCTOR}              {NAME_DOCTOR}
Médecin de l'unité        Chef de service

Questions : {CONTACT_PHONE} / {CONTACT_EMAIL}
""",
]

_FR = {
    "ed_triage": (_FR_ED_TRIAGE_HEADERS, _FR_ED_TRIAGE_BODY, _FR_ED_TRIAGE_FOOTERS),
    "op_report": (_FR_OP_HEADERS, _FR_OP_BODY, _FR_OP_FOOTERS),
    "radiology": (_FR_RADIO_HEADERS, _FR_RADIO_BODY, _FR_RADIO_FOOTERS),
    "rehab_discharge": (_FR_REHAB_HEADERS, _FR_REHAB_BODY, _FR_REHAB_FOOTERS),
}


# ===========================================================================
# Italian
# ===========================================================================

_IT_ED_TRIAGE_HEADERS = [
    """{LOCATION_HOSPITAL}
Pronto Soccorso — Nota di triage

Paziente: {NAME_PATIENT}, nato/a il {DATE_BIRTH}
Arrivo: {DATE}, {TIME}
Identificativo paziente: {ID}
Codice di triage: {TRIAGE_LEVEL}
""",
    """Primo contatto in Pronto Soccorso — {LOCATION_HOSPITAL}

Presentato/a: {NAME_PATIENT} ({AGE} anni, nato/a il {DATE_BIRTH})
Data/ora: {DATE} {TIME}
Accompagnatore: {NAME_RELATIVE}
Identificativo: {ID}
""",
]

_IT_ED_TRIAGE_BODY = [
    "Motivo principale: dolore toracico acuto dalla mattina del {DATE}, irradiato al braccio sinistro.",
    "Trasportato dal servizio di emergenza, preso in carico dalla centrale operativa alle {TIME}.",
    "Anamnesi riferita dal paziente: caduta dalla bicicletta, impatto con casco; breve perdita di coscienza.",
    "Parametri vitali: PA 145/90 mmHg, FC 102/min, SpO2 96% in aria ambiente, temperatura 37,8 °C.",
    "Allergie: nessuna allergia farmacologica nota.",
    "Terapia abituale riferita dal paziente: ramipril 5 mg, acido acetilsalicilico 100 mg, metformina 850 mg.",
    "Esame obiettivo: paziente vigile, orientato nelle varie sfere, assenza di rigidità nucale.",
    "Accompagnato da un familiare ({NAME_RELATIVE}) presente sul posto.",
    "Medico di base preavvisato: {NAME_DOCTOR}, ambulatorio a {LOCATION_CITY}.",
    "Stato assicurativo: SSN, codice di iscrizione non disponibile.",
    "Decisione di triage da parte di {NAME_DOCTOR}: necessario ricovero ospedaliero.",
    "Colloquio sui risultati programmato con {NAME_DOCTOR} a partire dalle {TIME}.",
    "Il paziente vive da solo in {LOCATION_STREET}, {LOCATION_ZIP} {LOCATION_CITY}.",
    "Recapito telefonico del familiare: {CONTACT_PHONE}.",
]

_IT_ED_TRIAGE_FOOTERS = [
    """
Esaminato da: {NAME_DOCTOR}
Assistenza infermieristica: {NAME_DOCTOR}
Per informazioni: {CONTACT_PHONE} oppure {CONTACT_EMAIL}
""",
    """
Triage completato il {DATE} alle {TIME}.
Responsabile: {NAME_DOCTOR}
Contatto Pronto Soccorso: {CONTACT_PHONE}
""",
]

_IT_OP_HEADERS = [
    """{LOCATION_HOSPITAL}
Unità di Chirurgia Generale e Viscerale

Verbale operatorio

Paziente: {NAME_PATIENT}, nato/a il {DATE_BIRTH}, Identificativo {ID}
Data dell'intervento: {DATE}
Chirurgo: {NAME_DOCTOR}
Primo assistente: {NAME_DOCTOR}
Anestesista: {NAME_DOCTOR}
""",
    """Verbale operatorio — {LOCATION_HOSPITAL}

Data dell'intervento: {DATE}
Ora di inizio: {TIME}
Paziente: {NAME_PATIENT} ({AGE} anni)
Identificativo: {ID}
Chirurgo: {NAME_DOCTOR}
Assistente: {NAME_DOCTOR}
""",
]

_IT_OP_BODY = [
    "Indicazione: colecistolitiasi sintomatica con coliche ricorrenti.",
    "Diagnosi preoperatoria: ernia inguinale destra, dolente dal {DATE}.",
    "Documentazione precedente della struttura {LOCATION_HOSPITAL} disponibile in cartella.",
    "Informazione: consenso scritto dettagliato acquisito il giorno precedente da {NAME_DOCTOR}.",
    "Posizionamento: decubito supino, entrambe le braccia lungo il corpo.",
    "Disinfezione cutanea e telatura sterile secondo prassi.",
    "Incisione: laparotomia mediana sovraombelicale, lunghezza circa 12 cm.",
    "Ingresso in addome per piani anatomici senza complicanze.",
    "Ispezione: assenza di ascite, superficie epatica liscia.",
    "Dissezione della colecisti con esposizione del dotto cistico e dell'arteria cistica.",
    "Legatura con clip e sezione di entrambe le strutture.",
    "Estrazione della colecisti in apposito sacchetto.",
    "Istologia: tessuto inviato all'Anatomia Patologica di {LOCATION_HOSPITAL} (richiesta {ID}).",
    "Chiusura della parete per piani, sutura cutanea intradermica con Monocryl 3-0.",
    "Fine dell'intervento: {TIME}; paziente trasferito stabile in sala risveglio.",
    "Prescrizioni postoperatorie discusse con il medico di reparto {NAME_DOCTOR}.",
]

_IT_OP_FOOTERS = [
    """
{LOCATION_CITY}, {DATE}

___________________
{NAME_DOCTOR}, Chirurgo
""",
    """
Chirurgo: {NAME_DOCTOR}
Dettato: {NAME_DOCTOR} il {DATE}
N. verbale: {ID}
""",
]

_IT_RADIO_HEADERS = [
    """{LOCATION_HOSPITAL}
Unità di Radiologia Diagnostica e Interventistica

Referto

Paziente: {NAME_PATIENT}, nato/a il {DATE_BIRTH}
Identificativo paziente: {ID}
Esame: TC torace con mezzo di contrasto
Data dell'esame: {DATE}, {TIME}
Medico richiedente: {NAME_DOCTOR}
""",
    """Referto radiologico — {LOCATION_HOSPITAL}

Paziente: {NAME_PATIENT}
Data di nascita: {DATE_BIRTH}
Identificativo: {ID}
Metodica: RM encefalo senza e con mezzo di contrasto
Data: {DATE}
Medico radiologo: {NAME_DOCTOR}
""",
]

_IT_RADIO_BODY = [
    "Quesito clinico del medico richiedente {NAME_DOCTOR}: sospetta embolia polmonare centrale.",
    "Esame precedente del {DATE} eseguito presso {LOCATION_HOSPITAL} disponibile per confronto.",
    "Tecnica: TC multidetettore, 80 ml di iomeprolo 350 e.v., spessore di strato 1 mm.",
    "Reperto polmonare: assenza di processi espansivi, assenza di versamento pleurico bilaterale.",
    "Mediastino: configurazione nella norma, assenza di adenopatie patologiche.",
    "Cuore: dimensioni regolari, assenza di versamento pericardico.",
    "Scheletro: alterazioni degenerative del rachide dorsale, per il resto in rapporto all'età.",
    "Encefalo: differenziazione sostanza grigia/bianca regolare, assenza di emorragie intracraniche.",
    "Conclusioni: nessun elemento per embolia polmonare centrale.",
    "Raccomandazione: controllo radiologico a 6 mesi o prima se clinicamente indicato.",
    "Reperti discussi con {NAME_DOCTOR} il {DATE} alle {TIME}.",
]

_IT_RADIO_FOOTERS = [
    """
{LOCATION_CITY}, {DATE}

{NAME_DOCTOR}
Medico Radiologo

Per informazioni: {CONTACT_PHONE}, {CONTACT_EMAIL}
""",
    """
Referto dettato da: {NAME_DOCTOR}
Trascritto da: {NAME_DOCTOR}
Referto finalizzato il {DATE} alle {TIME}
""",
]

_IT_REHAB_HEADERS = [
    """{LOCATION_HOSPITAL}
Unità di Riabilitazione Ortopedica

Lettera di dimissione

Paziente: {NAME_PATIENT}, nato/a il {DATE_BIRTH}
Professione: {PROFESSION}
Residente in: {LOCATION_STREET}, {LOCATION_ZIP} {LOCATION_CITY}
Identificativo paziente: {ID}
Ricovero: {DATE}
Dimissione: {DATE}
Medico di reparto: {NAME_DOCTOR}
""",
    """Lettera di dimissione riabilitativa

Struttura: {LOCATION_HOSPITAL}
Paziente: {NAME_PATIENT} ({AGE} anni)
N. identificativo: {ID}
Diagnosi principale: esiti di protesi totale d'anca destra
Ricovero: {DATE} — Dimissione: {DATE}
Medico curante: {NAME_DOCTOR}
""",
]

_IT_REHAB_BODY = [
    "Motivo del ricovero: riabilitazione post-acuta dopo intervento chirurgico presso {LOCATION_HOSPITAL}.",
    "Anamnesi: il paziente di {AGE} anni è stato ricoverato presso {LOCATION_HOSPITAL} fino al {DATE}.",
    "Anamnesi lavorativa: ha lavorato come {PROFESSION} fino all'esordio della malattia.",
    "Anamnesi familiare: padre deceduto per infarto del miocardio. Coniuge: {NAME_RELATIVE}.",
    "Anamnesi sociale: vive con il coniuge a {LOCATION_CITY}, scale prive di ascensore.",
    "Documentazione precedente: verbale operatorio del {NAME_DOCTOR} datato {DATE} in cartella.",
    "Piano di trattamento: fisioterapia due volte al giorno, terapia occupazionale secondo necessità.",
    "Decorso: progressi costanti della mobilizzazione sotto la guida del {NAME_DOCTOR}.",
    "Carico: carico completo adattato al dolore possibile a partire dal {DATE}.",
    "Supporto sociale: appuntamento con il servizio sociale della struttura il {DATE} alle {TIME}.",
    "Raccomandazione: proseguimento ambulatoriale con il medico di base {NAME_DOCTOR} a {LOCATION_CITY}.",
    "Terapia alla dimissione: acido acetilsalicilico 100 mg al mattino, pantoprazolo 20 mg al mattino.",
    "Ausili: stampelle per ulteriori 4 settimane.",
    "Recapiti del paziente per il follow-up: {CONTACT_PHONE}, {CONTACT_EMAIL}.",
]

_IT_REHAB_FOOTERS = [
    """
Cordiali saluti

{NAME_DOCTOR}
Direttore dell'Unità
{LOCATION_HOSPITAL}
Tel.: {CONTACT_PHONE}
""",
    """
{LOCATION_CITY}, {DATE}

{NAME_DOCTOR}              {NAME_DOCTOR}
Medico di reparto         Primario

Per informazioni: {CONTACT_PHONE} / {CONTACT_EMAIL}
""",
]

_IT = {
    "ed_triage": (_IT_ED_TRIAGE_HEADERS, _IT_ED_TRIAGE_BODY, _IT_ED_TRIAGE_FOOTERS),
    "op_report": (_IT_OP_HEADERS, _IT_OP_BODY, _IT_OP_FOOTERS),
    "radiology": (_IT_RADIO_HEADERS, _IT_RADIO_BODY, _IT_RADIO_FOOTERS),
    "rehab_discharge": (_IT_REHAB_HEADERS, _IT_REHAB_BODY, _IT_REHAB_FOOTERS),
}


# Public dispatch — keyed by --language code. de/en/fr/it only; Spanish
# clinical is covered by bench/corpora/meddocan_es (real gold).
TEMPLATES = {
    "de": _DE,
    "en": _EN,
    "fr": _FR,
    "it": _IT,
}

# Sublanguage order — fixed across all languages so doc ids stay stable.
SUBLANGUAGES = ("ed_triage", "op_report", "radiology", "rehab_discharge")
