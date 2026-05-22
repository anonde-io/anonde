#!/usr/bin/env python3
"""Locale-parametrised templates for synthetic financial documents.

Four doc types per language: invoice, bank statement, KYC / onboarding
record, transaction confirmation. Each doc type has a header pool, a body
pool (paragraphs sampled without replacement), and a footer pool.

Slots are `{SLOT_NAME}` markers filled by GENERATORS in generators.py.
The bank-statement doc type also carries dynamic transaction rows via a
`{ROWS_TX}` marker expanded by generate.py.

Non-PII placeholders (filled with realistic values but NOT gold-tagged):

  * {AMOUNT}      — monetary amount in locale formatting
  * {AMOUNT_NEG}  — negative amount (debit)
  * {REF}         — free-text payment reference
  * {INVNO}       — invoice number (a document identifier, not a person
                    identifier — left non-gold to keep the gold set to
                    the brief's PII slot list; see README)

TEMPLATES is keyed by language code, each value a dict of doc-type ->
(headers, body, footers, row_template_or_None, row_marker_or_None,
 (row_lo, row_hi), (body_lo, body_hi)).
"""

from __future__ import annotations


# ===========================================================================
# English
# ===========================================================================

_EN = {
    "invoice": (
        # headers
        [
            """{ORG_EMPLOYER}
INVOICE

Invoice number: {INVNO}
Invoice date: {DATE}
Due date: {DATE}

Bill to:
  {PERSON}
  {ADDRESS}
Customer reference: {ACCOUNT_ID}
""",
            """INVOICE — {ORG_EMPLOYER}

Issued: {DATE}
Customer: {PERSON}
Billing address: {ADDRESS}
Account: {ACCOUNT_ID}
Contact email: {EMAIL}
""",
        ],
        # body
        [
            "Description: professional services rendered during the period. Amount {AMOUNT}.",
            "Description: annual software subscription renewal. Amount {AMOUNT}.",
            "Subtotal {AMOUNT}, VAT at 20% {AMOUNT}, total due {AMOUNT}.",
            "Please remit payment to account {IBAN} quoting reference {REF}.",
            "Payment is due within 30 days of the invoice date {DATE}.",
            "Queries about this invoice should be directed to {EMAIL} or {PHONE}.",
            "A late-payment charge applies to balances unpaid after {DATE}.",
            "Card payments are accepted; the card ending {CREDIT_CARD} on file will be charged.",
            "This account is managed by your representative {PERSON}.",
        ],
        # footers
        [
            """
Remit to: {IBAN}
Questions: {EMAIL}
{ORG_EMPLOYER}
""",
            """
Thank you for your business.
Accounts receivable — {PERSON}, {PHONE}
""",
        ],
        None, None, (0, 0), (3, 6),
    ),
    "statement": (
        [
            """{ORG_BANK}

ACCOUNT STATEMENT

Account holder: {PERSON}
Address: {ADDRESS}
IBAN: {IBAN}
Statement date: {DATE}
Customer number: {ACCOUNT_ID}
""",
            """{ORG_BANK} — Online Banking

Statement for {PERSON}
IBAN: {IBAN}
Period: {DATE} to {DATE}
""",
        ],
        [
            "Opening balance carried forward as of {DATE}.",
            "For queries about these transactions, call {PHONE}.",
            "Online banking support is available at {EMAIL}.",
            "This account is held jointly with {PERSON}.",
            "Please review all entries within 60 days of the statement date.",
            "Your next statement will be issued on {DATE}.",
        ],
        [
            """
{ROWS_TX}

Closing balance as of {DATE}.

{ORG_BANK}
""",
            """
{ROWS_TX}

Prepared by relationship manager {PERSON}.
Contact: {PHONE}, {EMAIL}
""",
        ],
        "{DATE}  {PERSON}  {IBAN}  {REF}  {AMOUNT_NEG}",
        "ROWS_TX", (4, 8), (3, 5),
    ),
    "kyc": (
        [
            """{ORG_BANK}
Know Your Customer — Onboarding Record

Date received: {DATE}
Applicant: {PERSON}
Date of birth: {DATE}
Residential address: {ADDRESS}
""",
            """KYC Self-Certification — {ORG_BANK}

Processing date: {DATE}
Customer: {PERSON}
Customer number: {ACCOUNT_ID}
""",
        ],
        [
            "Residential address: {ADDRESS}.",
            "Contact telephone number: {PHONE}.",
            "Correspondence email address: {EMAIL}.",
            "Occupation / profession: {PROFESSION}.",
            "Current employer: {ORG_EMPLOYER}.",
            "Primary account IBAN: {IBAN}.",
            "A debit card ending {CREDIT_CARD} is linked to this account.",
            "Politically exposed person (PEP): no.",
            "Source of funds: salary from {ORG_EMPLOYER}.",
            "Tax residency: declared and verified on {DATE}.",
            "Relationship manager assigned: {PERSON}.",
            "Customer reference for this case: {ACCOUNT_ID}.",
        ],
        [
            """
I confirm the information above is accurate.

Signed: {PERSON}
Date: {DATE}
""",
            """
KYC review completed on {DATE}.
Case officer: {PERSON}
Queries: {PHONE} / {EMAIL}
""",
        ],
        None, None, (0, 0), (5, 9),
    ),
    "confirmation": (
        [
            """{ORG_BANK}

TRANSACTION CONFIRMATION

Reference: {ACCOUNT_ID}
Date: {DATE}
Account holder: {PERSON}
""",
            """Payment Confirmation — {ORG_BANK}

Confirmed: {DATE}
Payer: {PERSON}
""",
        ],
        [
            "A payment of {AMOUNT} has been processed from IBAN {IBAN}.",
            "Beneficiary: {PERSON}, IBAN {IBAN}.",
            "Payment reference: {REF}.",
            "The transaction will appear on the statement dated {DATE}.",
            "Card ending {CREDIT_CARD} was used to authorise this payment.",
            "A confirmation copy was emailed to {EMAIL}.",
            "For disputes, contact us on {PHONE} within 14 days.",
            "Processed by {PERSON} at the {ORG_BANK} settlement desk.",
        ],
        [
            """
Confirmation sent to {EMAIL}.
{ORG_BANK}
""",
            """
Authorised by {PERSON}.
Support: {PHONE}
""",
        ],
        None, None, (0, 0), (4, 7),
    ),
}


# ===========================================================================
# German
# ===========================================================================

_DE = {
    "invoice": (
        [
            """{ORG_EMPLOYER}
RECHNUNG

Rechnungsnummer: {INVNO}
Rechnungsdatum: {DATE}
Fälligkeitsdatum: {DATE}

Rechnungsempfänger:
  {PERSON}
  {ADDRESS}
Kundenreferenz: {ACCOUNT_ID}
""",
            """RECHNUNG — {ORG_EMPLOYER}

Ausgestellt: {DATE}
Kunde: {PERSON}
Rechnungsanschrift: {ADDRESS}
Konto: {ACCOUNT_ID}
Kontakt-E-Mail: {EMAIL}
""",
        ],
        [
            "Leistungsbeschreibung: erbrachte Dienstleistungen im Abrechnungszeitraum. Betrag {AMOUNT}.",
            "Leistungsbeschreibung: jährliche Software-Lizenzverlängerung. Betrag {AMOUNT}.",
            "Zwischensumme {AMOUNT}, MwSt. 19% {AMOUNT}, Gesamtbetrag {AMOUNT}.",
            "Bitte überweisen Sie den Betrag auf das Konto {IBAN} unter Angabe der Referenz {REF}.",
            "Der Betrag ist innerhalb von 30 Tagen ab Rechnungsdatum {DATE} fällig.",
            "Rückfragen zu dieser Rechnung richten Sie bitte an {EMAIL} oder {PHONE}.",
            "Bei Zahlungsverzug nach dem {DATE} fällt eine Mahngebühr an.",
            "Die hinterlegte Karte mit Endung {CREDIT_CARD} wird belastet.",
            "Dieses Konto wird von Ihrem Betreuer {PERSON} verwaltet.",
        ],
        [
            """
Zahlbar an: {IBAN}
Rückfragen: {EMAIL}
{ORG_EMPLOYER}
""",
            """
Vielen Dank für Ihren Auftrag.
Debitorenbuchhaltung — {PERSON}, {PHONE}
""",
        ],
        None, None, (0, 0), (3, 6),
    ),
    "statement": (
        [
            """{ORG_BANK}

KONTOAUSZUG

Kontoinhaber: {PERSON}
Anschrift: {ADDRESS}
IBAN: {IBAN}
Auszugsdatum: {DATE}
Kundennummer: {ACCOUNT_ID}
""",
            """{ORG_BANK} — Online-Banking

Auszug für {PERSON}
IBAN: {IBAN}
Zeitraum: {DATE} bis {DATE}
""",
        ],
        [
            "Saldovortrag zum {DATE}.",
            "Bei Rückfragen zu diesen Umsätzen erreichen Sie uns unter {PHONE}.",
            "Online-Banking-Support erhalten Sie unter {EMAIL}.",
            "Dieses Konto wird gemeinschaftlich mit {PERSON} geführt.",
            "Bitte prüfen Sie alle Buchungen innerhalb von 60 Tagen.",
            "Ihr nächster Auszug wird am {DATE} erstellt.",
        ],
        [
            """
{ROWS_TX}

Endsaldo zum {DATE}.

{ORG_BANK}
""",
            """
{ROWS_TX}

Erstellt durch Kundenberater {PERSON}.
Kontakt: {PHONE}, {EMAIL}
""",
        ],
        "{DATE}  {PERSON}  {IBAN}  {REF}  {AMOUNT_NEG}",
        "ROWS_TX", (4, 8), (3, 5),
    ),
    "kyc": (
        [
            """{ORG_BANK}
Know Your Customer — Onboarding-Datensatz

Eingangsdatum: {DATE}
Antragsteller: {PERSON}
Geburtsdatum: {DATE}
Wohnanschrift: {ADDRESS}
""",
            """KYC-Selbstauskunft — {ORG_BANK}

Bearbeitungsdatum: {DATE}
Kunde: {PERSON}
Kundennummer: {ACCOUNT_ID}
""",
        ],
        [
            "Wohnanschrift: {ADDRESS}.",
            "Telefonnummer: {PHONE}.",
            "E-Mail-Adresse für Korrespondenz: {EMAIL}.",
            "Beruf / Tätigkeit: {PROFESSION}.",
            "Aktueller Arbeitgeber: {ORG_EMPLOYER}.",
            "Haupt-Konto-IBAN: {IBAN}.",
            "Eine Debitkarte mit Endung {CREDIT_CARD} ist mit diesem Konto verknüpft.",
            "Politisch exponierte Person (PEP): nein.",
            "Mittelherkunft: Gehalt von {ORG_EMPLOYER}.",
            "Steuerlicher Wohnsitz: festgestellt und geprüft am {DATE}.",
            "Zugewiesener Kundenberater: {PERSON}.",
            "Fallreferenz: {ACCOUNT_ID}.",
        ],
        [
            """
Ich bestätige die Richtigkeit der obigen Angaben.

Unterschrift: {PERSON}
Datum: {DATE}
""",
            """
KYC-Prüfung abgeschlossen am {DATE}.
Sachbearbeiter: {PERSON}
Rückfragen: {PHONE} / {EMAIL}
""",
        ],
        None, None, (0, 0), (5, 9),
    ),
    "confirmation": (
        [
            """{ORG_BANK}

TRANSAKTIONSBESTÄTIGUNG

Referenz: {ACCOUNT_ID}
Datum: {DATE}
Kontoinhaber: {PERSON}
""",
            """Zahlungsbestätigung — {ORG_BANK}

Bestätigt: {DATE}
Zahler: {PERSON}
""",
        ],
        [
            "Eine Zahlung über {AMOUNT} wurde vom IBAN {IBAN} ausgeführt.",
            "Empfänger: {PERSON}, IBAN {IBAN}.",
            "Verwendungszweck: {REF}.",
            "Die Transaktion erscheint auf dem Auszug vom {DATE}.",
            "Karte mit Endung {CREDIT_CARD} wurde zur Autorisierung verwendet.",
            "Eine Bestätigungskopie wurde an {EMAIL} versendet.",
            "Bei Reklamationen erreichen Sie uns unter {PHONE} innerhalb von 14 Tagen.",
            "Bearbeitet durch {PERSON} am Abwicklungsschalter der {ORG_BANK}.",
        ],
        [
            """
Bestätigung gesendet an {EMAIL}.
{ORG_BANK}
""",
            """
Autorisiert durch {PERSON}.
Support: {PHONE}
""",
        ],
        None, None, (0, 0), (4, 7),
    ),
}


# ===========================================================================
# Spanish
# ===========================================================================

_ES = {
    "invoice": (
        [
            """{ORG_EMPLOYER}
FACTURA

Número de factura: {INVNO}
Fecha de factura: {DATE}
Fecha de vencimiento: {DATE}

Facturar a:
  {PERSON}
  {ADDRESS}
Referencia de cliente: {ACCOUNT_ID}
""",
            """FACTURA — {ORG_EMPLOYER}

Emitida: {DATE}
Cliente: {PERSON}
Dirección de facturación: {ADDRESS}
Cuenta: {ACCOUNT_ID}
Correo de contacto: {EMAIL}
""",
        ],
        [
            "Descripción: servicios profesionales prestados durante el período. Importe {AMOUNT}.",
            "Descripción: renovación anual de la suscripción de software. Importe {AMOUNT}.",
            "Base imponible {AMOUNT}, IVA 21% {AMOUNT}, total a pagar {AMOUNT}.",
            "Realice el pago a la cuenta {IBAN} indicando la referencia {REF}.",
            "El importe vence en un plazo de 30 días desde la fecha de factura {DATE}.",
            "Para consultas sobre esta factura, escriba a {EMAIL} o llame al {PHONE}.",
            "Se aplicará un recargo por demora a los saldos impagados después del {DATE}.",
            "Se cargará la tarjeta registrada terminada en {CREDIT_CARD}.",
            "Esta cuenta está gestionada por su representante {PERSON}.",
        ],
        [
            """
Pagar a: {IBAN}
Consultas: {EMAIL}
{ORG_EMPLOYER}
""",
            """
Gracias por su confianza.
Cuentas por cobrar — {PERSON}, {PHONE}
""",
        ],
        None, None, (0, 0), (3, 6),
    ),
    "statement": (
        [
            """{ORG_BANK}

EXTRACTO DE CUENTA

Titular de la cuenta: {PERSON}
Dirección: {ADDRESS}
IBAN: {IBAN}
Fecha del extracto: {DATE}
Número de cliente: {ACCOUNT_ID}
""",
            """{ORG_BANK} — Banca en línea

Extracto para {PERSON}
IBAN: {IBAN}
Período: {DATE} a {DATE}
""",
        ],
        [
            "Saldo anterior arrastrado a fecha de {DATE}.",
            "Para consultas sobre estos movimientos, llame al {PHONE}.",
            "El soporte de banca en línea está disponible en {EMAIL}.",
            "Esta cuenta es de titularidad compartida con {PERSON}.",
            "Revise todos los apuntes dentro de los 60 días siguientes a la fecha del extracto.",
            "Su próximo extracto se emitirá el {DATE}.",
        ],
        [
            """
{ROWS_TX}

Saldo final a fecha de {DATE}.

{ORG_BANK}
""",
            """
{ROWS_TX}

Preparado por el gestor {PERSON}.
Contacto: {PHONE}, {EMAIL}
""",
        ],
        "{DATE}  {PERSON}  {IBAN}  {REF}  {AMOUNT_NEG}",
        "ROWS_TX", (4, 8), (3, 5),
    ),
    "kyc": (
        [
            """{ORG_BANK}
Conozca a su cliente — Registro de alta

Fecha de recepción: {DATE}
Solicitante: {PERSON}
Fecha de nacimiento: {DATE}
Domicilio: {ADDRESS}
""",
            """Autocertificación KYC — {ORG_BANK}

Fecha de tramitación: {DATE}
Cliente: {PERSON}
Número de cliente: {ACCOUNT_ID}
""",
        ],
        [
            "Domicilio: {ADDRESS}.",
            "Número de teléfono de contacto: {PHONE}.",
            "Correo electrónico para correspondencia: {EMAIL}.",
            "Ocupación / profesión: {PROFESSION}.",
            "Empleador actual: {ORG_EMPLOYER}.",
            "IBAN de la cuenta principal: {IBAN}.",
            "Hay una tarjeta de débito terminada en {CREDIT_CARD} vinculada a esta cuenta.",
            "Persona del medio político (PEP): no.",
            "Origen de los fondos: salario de {ORG_EMPLOYER}.",
            "Residencia fiscal: declarada y verificada el {DATE}.",
            "Gestor asignado: {PERSON}.",
            "Referencia del expediente: {ACCOUNT_ID}.",
        ],
        [
            """
Confirmo que la información anterior es correcta.

Firmado: {PERSON}
Fecha: {DATE}
""",
            """
Revisión KYC completada el {DATE}.
Responsable del caso: {PERSON}
Consultas: {PHONE} / {EMAIL}
""",
        ],
        None, None, (0, 0), (5, 9),
    ),
    "confirmation": (
        [
            """{ORG_BANK}

CONFIRMACIÓN DE TRANSACCIÓN

Referencia: {ACCOUNT_ID}
Fecha: {DATE}
Titular de la cuenta: {PERSON}
""",
            """Confirmación de pago — {ORG_BANK}

Confirmado: {DATE}
Ordenante: {PERSON}
""",
        ],
        [
            "Se ha procesado un pago de {AMOUNT} desde el IBAN {IBAN}.",
            "Beneficiario: {PERSON}, IBAN {IBAN}.",
            "Concepto del pago: {REF}.",
            "La operación aparecerá en el extracto con fecha {DATE}.",
            "Se utilizó la tarjeta terminada en {CREDIT_CARD} para autorizar este pago.",
            "Se envió una copia de confirmación a {EMAIL}.",
            "Para reclamaciones, contáctenos en el {PHONE} dentro de 14 días.",
            "Procesado por {PERSON} en la mesa de liquidación de {ORG_BANK}.",
        ],
        [
            """
Confirmación enviada a {EMAIL}.
{ORG_BANK}
""",
            """
Autorizado por {PERSON}.
Soporte: {PHONE}
""",
        ],
        None, None, (0, 0), (4, 7),
    ),
}


# ===========================================================================
# French
# ===========================================================================

_FR = {
    "invoice": (
        [
            """{ORG_EMPLOYER}
FACTURE

Numéro de facture : {INVNO}
Date de facture : {DATE}
Date d'échéance : {DATE}

Facturer à :
  {PERSON}
  {ADDRESS}
Référence client : {ACCOUNT_ID}
""",
            """FACTURE — {ORG_EMPLOYER}

Émise le : {DATE}
Client : {PERSON}
Adresse de facturation : {ADDRESS}
Compte : {ACCOUNT_ID}
Courriel de contact : {EMAIL}
""",
        ],
        [
            "Description : prestations professionnelles réalisées sur la période. Montant {AMOUNT}.",
            "Description : renouvellement annuel de l'abonnement logiciel. Montant {AMOUNT}.",
            "Sous-total {AMOUNT}, TVA 20% {AMOUNT}, total à payer {AMOUNT}.",
            "Veuillez régler le montant sur le compte {IBAN} en indiquant la référence {REF}.",
            "Le montant est exigible dans les 30 jours suivant la date de facture {DATE}.",
            "Pour toute question sur cette facture, écrivez à {EMAIL} ou appelez le {PHONE}.",
            "Des pénalités de retard s'appliquent aux soldes impayés après le {DATE}.",
            "La carte enregistrée se terminant par {CREDIT_CARD} sera débitée.",
            "Ce compte est géré par votre interlocuteur {PERSON}.",
        ],
        [
            """
Régler à : {IBAN}
Questions : {EMAIL}
{ORG_EMPLOYER}
""",
            """
Merci de votre confiance.
Comptabilité clients — {PERSON}, {PHONE}
""",
        ],
        None, None, (0, 0), (3, 6),
    ),
    "statement": (
        [
            """{ORG_BANK}

RELEVÉ DE COMPTE

Titulaire du compte : {PERSON}
Adresse : {ADDRESS}
IBAN : {IBAN}
Date du relevé : {DATE}
Numéro de client : {ACCOUNT_ID}
""",
            """{ORG_BANK} — Banque en ligne

Relevé pour {PERSON}
IBAN : {IBAN}
Période : {DATE} au {DATE}
""",
        ],
        [
            "Solde antérieur reporté à la date du {DATE}.",
            "Pour toute question sur ces opérations, appelez le {PHONE}.",
            "L'assistance de la banque en ligne est disponible à {EMAIL}.",
            "Ce compte est détenu conjointement avec {PERSON}.",
            "Veuillez vérifier toutes les écritures dans les 60 jours suivant la date du relevé.",
            "Votre prochain relevé sera émis le {DATE}.",
        ],
        [
            """
{ROWS_TX}

Solde de clôture à la date du {DATE}.

{ORG_BANK}
""",
            """
{ROWS_TX}

Préparé par le conseiller {PERSON}.
Contact : {PHONE}, {EMAIL}
""",
        ],
        "{DATE}  {PERSON}  {IBAN}  {REF}  {AMOUNT_NEG}",
        "ROWS_TX", (4, 8), (3, 5),
    ),
    "kyc": (
        [
            """{ORG_BANK}
Connaissance du client — Dossier d'entrée en relation

Date de réception : {DATE}
Demandeur : {PERSON}
Date de naissance : {DATE}
Adresse de résidence : {ADDRESS}
""",
            """Auto-certification KYC — {ORG_BANK}

Date de traitement : {DATE}
Client : {PERSON}
Numéro de client : {ACCOUNT_ID}
""",
        ],
        [
            "Adresse de résidence : {ADDRESS}.",
            "Numéro de téléphone de contact : {PHONE}.",
            "Adresse électronique de correspondance : {EMAIL}.",
            "Profession / activité : {PROFESSION}.",
            "Employeur actuel : {ORG_EMPLOYER}.",
            "IBAN du compte principal : {IBAN}.",
            "Une carte de débit se terminant par {CREDIT_CARD} est liée à ce compte.",
            "Personne politiquement exposée (PPE) : non.",
            "Origine des fonds : salaire de {ORG_EMPLOYER}.",
            "Résidence fiscale : déclarée et vérifiée le {DATE}.",
            "Conseiller attribué : {PERSON}.",
            "Référence du dossier : {ACCOUNT_ID}.",
        ],
        [
            """
Je certifie que les informations ci-dessus sont exactes.

Signé : {PERSON}
Date : {DATE}
""",
            """
Examen KYC terminé le {DATE}.
Chargé de dossier : {PERSON}
Questions : {PHONE} / {EMAIL}
""",
        ],
        None, None, (0, 0), (5, 9),
    ),
    "confirmation": (
        [
            """{ORG_BANK}

CONFIRMATION DE TRANSACTION

Référence : {ACCOUNT_ID}
Date : {DATE}
Titulaire du compte : {PERSON}
""",
            """Confirmation de paiement — {ORG_BANK}

Confirmé le : {DATE}
Donneur d'ordre : {PERSON}
""",
        ],
        [
            "Un paiement de {AMOUNT} a été traité depuis l'IBAN {IBAN}.",
            "Bénéficiaire : {PERSON}, IBAN {IBAN}.",
            "Référence du paiement : {REF}.",
            "L'opération apparaîtra sur le relevé daté du {DATE}.",
            "La carte se terminant par {CREDIT_CARD} a été utilisée pour autoriser ce paiement.",
            "Une copie de confirmation a été envoyée à {EMAIL}.",
            "En cas de litige, contactez-nous au {PHONE} sous 14 jours.",
            "Traité par {PERSON} au desk de règlement de {ORG_BANK}.",
        ],
        [
            """
Confirmation envoyée à {EMAIL}.
{ORG_BANK}
""",
            """
Autorisé par {PERSON}.
Assistance : {PHONE}
""",
        ],
        None, None, (0, 0), (4, 7),
    ),
}


# ===========================================================================
# Italian
# ===========================================================================

_IT = {
    "invoice": (
        [
            """{ORG_EMPLOYER}
FATTURA

Numero fattura: {INVNO}
Data fattura: {DATE}
Data di scadenza: {DATE}

Intestare a:
  {PERSON}
  {ADDRESS}
Riferimento cliente: {ACCOUNT_ID}
""",
            """FATTURA — {ORG_EMPLOYER}

Emessa: {DATE}
Cliente: {PERSON}
Indirizzo di fatturazione: {ADDRESS}
Conto: {ACCOUNT_ID}
Email di contatto: {EMAIL}
""",
        ],
        [
            "Descrizione: prestazioni professionali rese nel periodo. Importo {AMOUNT}.",
            "Descrizione: rinnovo annuale dell'abbonamento software. Importo {AMOUNT}.",
            "Imponibile {AMOUNT}, IVA 22% {AMOUNT}, totale dovuto {AMOUNT}.",
            "Si prega di effettuare il pagamento sul conto {IBAN} indicando il riferimento {REF}.",
            "L'importo è dovuto entro 30 giorni dalla data della fattura {DATE}.",
            "Per domande su questa fattura, scrivere a {EMAIL} o chiamare il {PHONE}.",
            "Una penale per ritardato pagamento si applica ai saldi insoluti dopo il {DATE}.",
            "La carta registrata che termina con {CREDIT_CARD} verrà addebitata.",
            "Questo conto è gestito dal suo referente {PERSON}.",
        ],
        [
            """
Pagare a: {IBAN}
Domande: {EMAIL}
{ORG_EMPLOYER}
""",
            """
Grazie per la fiducia accordata.
Contabilità clienti — {PERSON}, {PHONE}
""",
        ],
        None, None, (0, 0), (3, 6),
    ),
    "statement": (
        [
            """{ORG_BANK}

ESTRATTO CONTO

Intestatario del conto: {PERSON}
Indirizzo: {ADDRESS}
IBAN: {IBAN}
Data dell'estratto: {DATE}
Numero cliente: {ACCOUNT_ID}
""",
            """{ORG_BANK} — Banca online

Estratto per {PERSON}
IBAN: {IBAN}
Periodo: dal {DATE} al {DATE}
""",
        ],
        [
            "Saldo precedente riportato alla data del {DATE}.",
            "Per domande su queste operazioni, chiamare il {PHONE}.",
            "L'assistenza per la banca online è disponibile all'indirizzo {EMAIL}.",
            "Questo conto è cointestato con {PERSON}.",
            "Si prega di verificare tutte le registrazioni entro 60 giorni dalla data dell'estratto.",
            "Il prossimo estratto sarà emesso il {DATE}.",
        ],
        [
            """
{ROWS_TX}

Saldo finale alla data del {DATE}.

{ORG_BANK}
""",
            """
{ROWS_TX}

Preparato dal gestore {PERSON}.
Contatto: {PHONE}, {EMAIL}
""",
        ],
        "{DATE}  {PERSON}  {IBAN}  {REF}  {AMOUNT_NEG}",
        "ROWS_TX", (4, 8), (3, 5),
    ),
    "kyc": (
        [
            """{ORG_BANK}
Adeguata verifica della clientela — Scheda di apertura

Data di ricezione: {DATE}
Richiedente: {PERSON}
Data di nascita: {DATE}
Indirizzo di residenza: {ADDRESS}
""",
            """Autocertificazione KYC — {ORG_BANK}

Data di elaborazione: {DATE}
Cliente: {PERSON}
Numero cliente: {ACCOUNT_ID}
""",
        ],
        [
            "Indirizzo di residenza: {ADDRESS}.",
            "Numero di telefono di contatto: {PHONE}.",
            "Indirizzo email per la corrispondenza: {EMAIL}.",
            "Occupazione / professione: {PROFESSION}.",
            "Datore di lavoro attuale: {ORG_EMPLOYER}.",
            "IBAN del conto principale: {IBAN}.",
            "Una carta di debito che termina con {CREDIT_CARD} è collegata a questo conto.",
            "Persona politicamente esposta (PEP): no.",
            "Origine dei fondi: stipendio da {ORG_EMPLOYER}.",
            "Residenza fiscale: dichiarata e verificata il {DATE}.",
            "Gestore assegnato: {PERSON}.",
            "Riferimento della pratica: {ACCOUNT_ID}.",
        ],
        [
            """
Confermo che le informazioni di cui sopra sono corrette.

Firmato: {PERSON}
Data: {DATE}
""",
            """
Verifica KYC completata il {DATE}.
Responsabile della pratica: {PERSON}
Domande: {PHONE} / {EMAIL}
""",
        ],
        None, None, (0, 0), (5, 9),
    ),
    "confirmation": (
        [
            """{ORG_BANK}

CONFERMA DI TRANSAZIONE

Riferimento: {ACCOUNT_ID}
Data: {DATE}
Intestatario del conto: {PERSON}
""",
            """Conferma di pagamento — {ORG_BANK}

Confermato: {DATE}
Ordinante: {PERSON}
""",
        ],
        [
            "Un pagamento di {AMOUNT} è stato elaborato dall'IBAN {IBAN}.",
            "Beneficiario: {PERSON}, IBAN {IBAN}.",
            "Causale del pagamento: {REF}.",
            "L'operazione comparirà sull'estratto conto con data {DATE}.",
            "La carta che termina con {CREDIT_CARD} è stata usata per autorizzare questo pagamento.",
            "Una copia di conferma è stata inviata a {EMAIL}.",
            "Per contestazioni, contattateci al {PHONE} entro 14 giorni.",
            "Elaborato da {PERSON} presso lo sportello di regolamento di {ORG_BANK}.",
        ],
        [
            """
Conferma inviata a {EMAIL}.
{ORG_BANK}
""",
            """
Autorizzato da {PERSON}.
Assistenza: {PHONE}
""",
        ],
        None, None, (0, 0), (4, 7),
    ),
}


# Master table: language -> doc-type -> template tuple.
TEMPLATES = {
    "en": _EN,
    "de": _DE,
    "es": _ES,
    "fr": _FR,
    "it": _IT,
}

# Document types, in a fixed order so doc ids are deterministic.
DOCTYPES = ["invoice", "statement", "kyc", "confirmation"]
