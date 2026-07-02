package recognizers

import (
	"context"
	"testing"
)

func TestUsernameRecognizer_ContextGated(t *testing.T) {
	r := NewUsernameRecognizer()

	cases := []struct {
		name string
		text string
		want []string
	}{
		{
			name: "username stem digit",
			text: "username: kottmann1989",
			want: []string{"kottmann1989"},
		},
		{
			name: "handle dotted",
			text: "handle bercem.luini should be treated as an account owner",
			want: []string{"bercem.luini"},
		},
		{
			name: "unicode login",
			text: "login schlöter01 was used for the profile",
			want: []string{"schlöter01"},
		},
		{
			name: "github account",
			text: "GitHub account xmrlcpvcqejqc8071 opened the issue",
			want: []string{"xmrlcpvcqejqc8071"},
		},
		{
			name: "protocol dotted identifier no context",
			text: "The request contains assistant.message and response.output_text fields.",
		},
		{
			name: "model or version slug no context",
			text: "Use model claudeopus2026 with schema version message2026.",
		},
		{
			name: "json role text no context",
			text: `{"role":"user","content":"set default_profile2026 in tool.config"}`,
		},
		// Self-corroboration guard: a dotted id whose OWN segment is a cue word
		// ("github", "profile") must not authorize itself — context must be
		// external to the span.
		{
			name: "github dotted self-cue no external context",
			text: "workflow uses github.actions",
		},
		{
			name: "profile dotted self-cue no external context",
			text: "profile.default was updated",
		},
		{
			name: "dotted handle external github cue",
			text: "GitHub account bercem.luini",
			want: []string{"bercem.luini"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := r.Analyze(context.Background(), tc.text, nil, "en")
			if err != nil {
				t.Fatalf("Analyze: %v", err)
			}
			if len(got) != len(tc.want) {
				spans := make([]string, 0, len(got))
				for _, res := range got {
					spans = append(spans, tc.text[res.Start:res.End])
				}
				t.Fatalf("got %d spans %v, want %d %v", len(got), spans, len(tc.want), tc.want)
			}
			for i, res := range got {
				span := tc.text[res.Start:res.End]
				if span != tc.want[i] {
					t.Fatalf("span %d = %q, want %q", i, span, tc.want[i])
				}
				if res.EntityType != "PERSON" || res.RecognizerName != "UsernameRecognizer" || res.Score != 0.50 {
					t.Fatalf("unexpected result metadata: %+v", res)
				}
			}
		})
	}
}
