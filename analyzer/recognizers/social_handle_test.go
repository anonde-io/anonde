package recognizers

import (
	"context"
	"testing"
)

func TestSocialHandleRecognizer(t *testing.T) {
	r := NewSocialHandleRecognizer()

	type want struct {
		span       string
		entityType string
	}

	cases := []struct {
		name  string
		text  string
		wants []want
	}{
		// Explicit `@handle` — PERSON.
		{"plain @handle", "Follow @alice for updates.", []want{{"@alice", "PERSON"}}},
		{"@handle at start", "@alice ping me back.", []want{{"@alice", "PERSON"}}},
		{"@handle with underscore", "ping @user_name now.", []want{{"@user_name", "PERSON"}}},
		{"@handle with digits", "see @wnut_17 thread.", []want{{"@wnut_17", "PERSON"}}},
		{"wnut tokenised space after @", "RT @ beatfaceleah :", []want{{"@ beatfaceleah", "PERSON"}}},
		{"max length 30 chars after @", "RT @abcdefghijklmnopqrstuvwxyzABCD end.", []want{{"@abcdefghijklmnopqrstuvwxyzABCD", "PERSON"}}},

		// Hashtag — ORGANIZATION.
		{"plain #hashtag", "Loving #fitnessblender today.", []want{{"#fitnessblender", "ORGANIZATION"}}},
		{"#hashtag at start", "#nike just dropped a new shoe.", []want{{"#nike", "ORGANIZATION"}}},
		{"tokenised space after #", "Brand # fitnessblender rocks.", []want{{"# fitnessblender", "ORGANIZATION"}}},

		// Multiple matches across both patterns.
		{
			"mixed @ and #",
			"@alice mentioned #nike yesterday.",
			[]want{{"@alice", "PERSON"}, {"#nike", "ORGANIZATION"}},
		},

		// Must NOT match.
		{"email address", "Email user@domain.com today.", nil},
		{"too short @ab", "ping @ab now.", nil},
		{"starts with digit", "see @123abc here.", nil},
		{"@ followed by punct", "tag @!foo later.", nil},
		{"@ at end of text", "tag @", nil},
		{"hashtag in word", "foo#bar baz.", nil},
		{"empty string", "", nil},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res, err := r.Analyze(context.Background(), tc.text, nil, "en")
			if err != nil {
				t.Fatalf("Analyze: %v", err)
			}

			if len(tc.wants) == 0 {
				if len(res) > 0 {
					spans := make([]string, 0, len(res))
					for _, x := range res {
						spans = append(spans, tc.text[x.Start:x.End])
					}
					t.Fatalf("expected no match, got %v", spans)
				}
				return
			}

			if len(res) != len(tc.wants) {
				spans := make([]string, 0, len(res))
				for _, x := range res {
					spans = append(spans, tc.text[x.Start:x.End])
				}
				t.Fatalf("got %d matches %v, want %d %v", len(res), spans, len(tc.wants), tc.wants)
			}

			// Build a multiset of expected (span, entityType) and consume.
			remaining := make(map[want]int)
			for _, w := range tc.wants {
				remaining[w]++
			}
			for _, x := range res {
				got := want{span: tc.text[x.Start:x.End], entityType: x.EntityType}
				if remaining[got] == 0 {
					t.Fatalf("unexpected match %+v (score=%v, recognizer=%s)", got, x.Score, x.RecognizerName)
				}
				remaining[got]--

				// Score + recognizer name sanity.
				if x.RecognizerName != "SocialHandleRecognizer" {
					t.Fatalf("recognizer name %q, want SocialHandleRecognizer", x.RecognizerName)
				}
				switch x.EntityType {
				case "PERSON":
					if x.Score != 0.85 {
						t.Fatalf("PERSON score %v, want 0.85", x.Score)
					}
				case "ORGANIZATION":
					if x.Score != 0.78 {
						t.Fatalf("ORGANIZATION score %v, want 0.78", x.Score)
					}
				default:
					t.Fatalf("unexpected entity type %q", x.EntityType)
				}
			}
		})
	}
}

func TestSocialHandleRecognizer_Metadata(t *testing.T) {
	r := NewSocialHandleRecognizer()

	if name := r.Name(); name != "SocialHandleRecognizer" {
		t.Fatalf("Name() = %q, want SocialHandleRecognizer", name)
	}

	ents := r.SupportedEntities()
	wantEnts := map[string]bool{"PERSON": true, "ORGANIZATION": true}
	if len(ents) != len(wantEnts) {
		t.Fatalf("SupportedEntities() = %v, want %v", ents, wantEnts)
	}
	for _, e := range ents {
		if !wantEnts[e] {
			t.Fatalf("unexpected entity %q in SupportedEntities() = %v", e, ents)
		}
	}

	langs := r.SupportedLanguages()
	if len(langs) != 1 || langs[0] != "*" {
		t.Fatalf("SupportedLanguages() = %v, want [*]", langs)
	}
}
