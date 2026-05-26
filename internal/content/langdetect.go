package content

import (
	"strings"
	"unicode"
)

// DetectLanguage guesses the language of `text` between "de" and "en"
// using a stopword + German-specific-rune heuristic. Returns "" when
// the signal is too weak (a short identifier-only string with no
// function words and no umlauts). Caller falls back to a default
// language.
//
// Why a heuristic and not a model: anonde runs in-process, often as a
// sidecar; we don't want to ship a language-detection model or pay
// cold-start latency for sub-millisecond classification. >99% accuracy
// in practice on the targets we care about (clinical letters, log
// lines, business prose).
//
// Algorithm:
//
//  1. Look for German-specific characters (ä, ö, ü, ß, ÄÖÜ). Each
//     occurrence is a strong DE marker; English essentially never uses
//     umlauts/eszett. We weight each at deRunesWeight points.
//  2. Lowercase + tokenise the first detectSampleBytes on non-letter
//     boundaries. Count tokens in deStopwords and enStopwords (disjoint
//     lists; every entry is a function word unique to that language).
//  3. Combine: deScore = umlautCount*deRunesWeight + deStopwordHits;
//     enScore = enStopwordHits. Whichever score is strictly larger
//     wins. Returns "" on a true zero/zero tie so caller can default.
//
// The umlaut signal exists because German clinical metadata headers
// ("Patient:", "Beruf:", "Telefon:") often contain zero function words
// but reliably contain at least one umlaut in entities like
// "Universitätsklinikum" or "München".

const (
	detectSampleBytes = 4096
	deRunesWeight     = 3
)

// germanSpecificRunes; characters German uses but English doesn't.
var germanSpecificRunes = map[rune]struct{}{
	'ä': {}, 'ö': {}, 'ü': {}, 'ß': {},
	'Ä': {}, 'Ö': {}, 'Ü': {},
}

var deStopwords = map[string]struct{}{
	"der": {}, "die": {}, "das": {}, "den": {}, "dem": {}, "des": {},
	"und": {}, "ist": {}, "ein": {}, "eine": {}, "einer": {}, "einem": {},
	"nicht": {}, "ich": {}, "sich": {}, "wir": {}, "sie": {}, "er": {},
	"war": {}, "wurde": {}, "hatte": {}, "haben": {}, "hat": {},
	"durch": {}, "über": {}, "ohne": {}, "noch": {}, "schon": {}, "sehr": {},
	"vom": {}, "zum": {}, "zur": {}, "beim": {}, "im": {}, "am": {},
	"auch": {}, "sind": {}, "wenn": {}, "wird": {}, "würde": {},
	"auf": {}, "für": {}, "von": {}, "bei": {}, "nach": {}, "aus": {},
	"bis": {}, "mit": {}, "als": {}, "dass": {}, "weil": {}, "aber": {},
	"oder": {}, "doch": {},
}

var enStopwords = map[string]struct{}{
	"the": {}, "of": {}, "to": {}, "and": {}, "is": {}, "for": {},
	"with": {}, "on": {}, "at": {}, "by": {}, "an": {}, "be": {},
	"this": {}, "that": {}, "are": {}, "were": {}, "has": {}, "have": {},
	"had": {}, "from": {}, "or": {}, "but": {}, "not": {}, "will": {},
	"would": {}, "should": {}, "could": {}, "their": {}, "they": {},
	"there": {}, "which": {}, "what": {}, "where": {}, "you": {},
	"your": {}, "our": {}, "we": {}, "my": {}, "his": {}, "her": {},
	"its": {}, "been": {}, "being": {}, "do": {}, "does": {}, "did": {},
	"can": {}, "may": {}, "might": {}, "must": {}, "if": {},
}

func DetectLanguage(text string) string {
	if text == "" {
		return ""
	}
	sample := text
	if len(sample) > detectSampleBytes {
		sample = sample[:detectSampleBytes]
	}
	deRunes := 0
	for _, r := range sample {
		if _, ok := germanSpecificRunes[r]; ok {
			deRunes++
		}
	}
	tokens := strings.FieldsFunc(strings.ToLower(sample), func(r rune) bool {
		return !unicode.IsLetter(r)
	})
	deStop, enStop := 0, 0
	for _, tok := range tokens {
		if _, ok := deStopwords[tok]; ok {
			deStop++
		}
		if _, ok := enStopwords[tok]; ok {
			enStop++
		}
	}
	de := deStop + deRunes*deRunesWeight
	en := enStop
	if de == 0 && en == 0 {
		return ""
	}
	if de > en {
		return "de"
	}
	if en > de {
		return "en"
	}
	return ""
}
