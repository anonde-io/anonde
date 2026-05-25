package recognizers

import "regexp"

// Romanian-specific pattern recognizers. Complement GLiNER NER on the
// surfaces it consistently misses on Romanian government / legal /
// banking forms — landline phones (county-prefix format, GLiNER trained
// on US-shape phones misses these), the 13-digit CNP personal-numeric
// code, HH:MM:SS times the model classifies as plain dates, and
// vehicle-registration tags ("R20 0700288") common in police-fine
// notices.

var (
	// Romanian landline & mobile: leading 0, 3-digit area code, 6-7 more
	// digits. Allows '-', '.', or space between groups. Examples:
	// "0232-740646", "021 3120030", "0744123456".
	roPhoneRE = regexp.MustCompile(
		`\b0\d{3}[-.\s]?\d{6,7}\b`,
	)

	// CNP — Romanian personal numeric code, 13 digits. First digit is
	// 1-8 (sex + century-of-birth tag); the rest is birthdate + county
	// + sequence + checksum. Strict shape — long digit strings outside
	// this prefix range are not CNPs.
	roCNPRE = regexp.MustCompile(
		`\b[1-8]\d{12}\b`,
	)

	// HH:MM[:SS] time of day. Trailing :SS is optional so "13:27" and
	// "13:27:50" both match. The hour is bounded at 23 to rule out
	// "29:90:90"-style numeric noise that surfaces on tables of dates.
	roTimeRE = regexp.MustCompile(
		`\b(?:[01]?\d|2[0-3]):[0-5]\d(?::[0-5]\d)?\b`,
	)

	// Vehicle-registration / fine-record tag: "R20 0700288",
	// "R190524925", "R18 R18 0697785" (the OCR sometimes doubles).
	// Strict shape — single uppercase R, 2 digits (year prefix), optional
	// space, 6-9 digits. Score below the generic ID pattern.
	roVehicleRegRE = regexp.MustCompile(
		`\bR\d{2}[-.\s]?\d{6,9}\b`,
	)

	// Romanian money in "1.234,56 lei" or "1234,56 RON" form. Thousands
	// separator '.' is optional (Romanian writes "1.234" but OCR often
	// drops the dot). Currency word required to avoid matching arbitrary
	// decimals.
	roMoneyRE = regexp.MustCompile(
		`\b\d{1,3}(?:\.\d{3})*(?:,\d{1,2})?\s*(?:lei|LEI|RON|EUR|USD|euro)\b`,
	)

	// Romanian Treasury (Trezoreria) account references — the "TREZ"
	// prefix on bank account numbers. The full IBAN form is
	// "RO<dd>TREZ<dd>..." but OCR commonly fragments this in tables
	// (loses the "RO<dd>" prefix on subsequent rows, leaving bare
	// "TREZ..." strings) or jitters digits to letters ("RO57" → "ROS7"
	// where '5' is misread as 'S'). The international IBAN regex
	// requires the country code AND strict digit positions so it
	// misses these. Matching TREZ + 10-30 alphanumerics is strict
	// enough to avoid arbitrary code/word collisions in clinical or
	// legal text — and importantly, the TREZ token is preserved by
	// OCR even when surrounding digits jitter.
	roTreasuryAccountRE = regexp.MustCompile(
		// No leading \b on purpose: tesseract sometimes glues a stray
		// alphanumeric prefix to the IBAN ("AROSTTREZ..." for what
		// should be "RO88TREZ..."), or other table-cell content runs
		// into it ("250,00|RO57TREZ..."). The TREZ + 10-30 alphanums
		// shape is specific enough that the substring match is safe.
		`(?:RO[A-Z0-9]{1,3})?TREZ[A-Z0-9]{10,30}\b`,
	)

	// Romanian street addresses. The anchors are the Romanian
	// street-type prefixes (STRADA / STR. / SOSEAUA / SOS. / BD. /
	// BULEVARDUL / ALEEA / CALEA / PIATA / SPLAIUL / INTRAREA),
	// followed by 1-6 capitalised tokens, an optional ", NR. <digits>"
	// suffix. Case-insensitive on the prefix so "Strada" / "STRADA"
	// both match.
	roStreetRE = regexp.MustCompile(
		`(?i)\b(?:strada|str\.|soseaua|sos\.|bd\.|bulevardul|aleea|calea|piata|piaţa|splaiul|intrarea)[\s.]+` +
			`[A-Za-zĂÂÎȘȚăâîșțŞŢşţ][A-Za-zĂÂÎȘȚăâîșțŞŢşţ.'-]{1,40}` +
			`(?:[\s,]+[A-Za-zĂÂÎȘȚăâîșțŞŢşţ.'-]{1,40}){0,5}` +
			`(?:[\s,]+(?:nr\.?|no\.?)[\s.]?\d{1,5}[A-Za-z]?)?`,
	)

	// Observation / annotation fields on Romanian government and
	// police forms. Format is "Obs:<TEXT>" — usually a brief
	// uppercase reason code like "LIPSA ROVINETA" (missing road
	// tax) or "FARA ROVINIETA" (without). Standalone these are
	// public violation codes; alongside a named debtor in the same
	// row they're sensitive enough to redact (matches Private AI /
	// Limina behaviour, which classifies them as NAME).
	// Stops at the next non-capital token / punctuation / digit.
	roObservationRE = regexp.MustCompile(
		`\bObs[:.]\s*[A-ZĂÂÎȘȚŞŢ][A-ZĂÂÎȘȚŞŢ\s]{2,80}`,
	)
)

// NewRomanianPhoneRecognizer detects Romanian landline + mobile numbers.
func NewRomanianPhoneRecognizer() *PatternRecognizer {
	return NewPatternRecognizerWithContext(
		"RomanianPhoneRecognizer",
		[]string{"PHONE_NUMBER"},
		[]string{"*"},
		[]namedPattern{{re: roPhoneRE, score: 0.80}},
		[]string{"tel", "telefon", "fax", "mobil", "celular", "contact"},
	)
}

// NewRomanianCNPRecognizer detects Romanian CNP personal-identification
// codes (13 digits, leading 1-8).
func NewRomanianCNPRecognizer() *PatternRecognizer {
	return NewPatternRecognizerWithContext(
		"RomanianCNPRecognizer",
		[]string{"ID"},
		[]string{"*"},
		[]namedPattern{{re: roCNPRE, score: 0.85}},
		[]string{"CNP", "cod numeric", "C.I.F", "identificare"},
	)
}

// NewTimeOfDayRecognizer detects HH:MM[:SS] surfaces — language-agnostic.
func NewTimeOfDayRecognizer() *PatternRecognizer {
	return NewPatternRecognizerWithContext(
		"TimeOfDayRecognizer",
		[]string{"DATE_TIME"},
		[]string{"*"},
		[]namedPattern{{re: roTimeRE, score: 0.70}},
		[]string{"ora", "time", "h", "AM", "PM"},
	)
}

// NewRomanianVehicleRegRecognizer detects vehicle / fine tags ("R20
// 0700288"). Common on police-fine and garnishment notices.
func NewRomanianVehicleRegRecognizer() *PatternRecognizer {
	return NewPatternRecognizerWithContext(
		"RomanianVehicleRegRecognizer",
		[]string{"ID"},
		[]string{"*"},
		[]namedPattern{{re: roVehicleRegRE, score: 0.75}},
		[]string{"amenda", "amenzi", "circulatie", "BORD", "rovineta"},
	)
}

// NewRomanianMoneyRecognizer detects monetary amounts with explicit
// currency suffix (lei / RON / EUR / USD).
func NewRomanianMoneyRecognizer() *PatternRecognizer {
	return NewPatternRecognizerWithContext(
		"RomanianMoneyRecognizer",
		[]string{"MONEY"},
		[]string{"*"},
		[]namedPattern{{re: roMoneyRE, score: 0.80}},
		[]string{"suma", "total", "amount", "cuantum", "datorat"},
	)
}

// NewRomanianTreasuryAccountRecognizer detects Trezoreria (Romanian
// Treasury) bank-account references — TREZ-prefixed bare account
// fragments that OCR drops the country code from. Complements the
// generic IBAN recognizer which requires the full RO<dd> prefix.
func NewRomanianTreasuryAccountRecognizer() *PatternRecognizer {
	return NewPatternRecognizerWithContext(
		"RomanianTreasuryAccountRecognizer",
		[]string{"IBAN_CODE"},
		[]string{"*"},
		[]namedPattern{{re: roTreasuryAccountRE, score: 0.85}},
		[]string{"cont", "trezorerie", "TREZ", "IBAN"},
	)
}

// NewRomanianStreetRecognizer detects Romanian street addresses by
// their characteristic prefix tokens (STRADA / SOSEAUA / BULEVARDUL
// etc.) plus a capitalised street name and optional house-number
// suffix. Complements GLiNER's STREET_ADDRESS for cases where the
// open-set model has lower recall on dense form fields.
func NewRomanianStreetRecognizer() *PatternRecognizer {
	return NewPatternRecognizerWithContext(
		"RomanianStreetRecognizer",
		[]string{"STREET_ADDRESS"},
		[]string{"*"},
		[]namedPattern{{re: roStreetRE, score: 0.80}},
		[]string{"adresa", "domiciliu", "sediu", "address"},
	)
}

// NewRomanianObservationRecognizer detects "Obs:<TEXT>" annotation
// fields on Romanian government / police forms (e.g. "Obs:LIPSA
// ROVINETA", "Obs:FARA ROVINIETA"). Treated as PII-by-association
// since the reason code is paired with a named debtor in the same
// table row.
func NewRomanianObservationRecognizer() *PatternRecognizer {
	return NewPatternRecognizerWithContext(
		"RomanianObservationRecognizer",
		[]string{"PERSON"},
		[]string{"*"},
		[]namedPattern{{re: roObservationRE, score: 0.80}},
		[]string{"Obs", "observatie", "observaţie"},
	)
}
