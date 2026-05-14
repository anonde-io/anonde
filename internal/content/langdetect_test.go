package content

import "testing"

func TestDetectLanguage(t *testing.T) {
	cases := []struct {
		name string
		text string
		want string
	}{
		{
			"German clinical letter",
			"Patient: Anna Schmidt, geb. 15.03.1985. Die Diagnose wurde " +
				"durch die Untersuchung am Universitätsklinikum bestätigt. " +
				"Der behandelnde Arzt empfiehlt eine weitere Kontrolle.",
			"de",
		},
		{
			"English clinical letter",
			"Patient: John Doe, born 03/15/1985. The diagnosis was confirmed " +
				"at the university hospital. The treating physician recommends " +
				"a follow-up.",
			"en",
		},
		{
			"German with English drug names (code-switched, DE dominant)",
			"Der Patient wurde mit Aspirin und Ibuprofen behandelt. Eine " +
				"Allergie ist nicht bekannt.",
			"de",
		},
		{
			"Empty input returns unknown",
			"",
			"",
		},
		{
			"Bare phone returns unknown — no function words",
			"+49 89 1234567",
			"",
		},
		{
			"GraSCCo-style fragment is German",
			"Aufnahmegrund: Anschlussheilbehandlung nach operativer Versorgung " +
				"im Hause Universitätsklinikum Heidelberg.",
			"de",
		},
		{
			"German clinical metadata header (no function words, has umlauts)",
			"Patient: Anna Schmidt, geb. 15.03.1985\nPatient-ID: PAT-456789\n" +
				"Beruf: Lehrerin\nTelefon: +49 89 1234567\n" +
				"Klinik: Universitätsklinikum Heidelberg",
			"de",
		},
		{
			"English log line",
			"The user logged in from the browser at the device.",
			"en",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := DetectLanguage(tc.text)
			if got != tc.want {
				t.Fatalf("DetectLanguage(%q) = %q, want %q", tc.text, got, tc.want)
			}
		})
	}
}
