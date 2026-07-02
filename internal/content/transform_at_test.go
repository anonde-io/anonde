package content

import (
	"strings"
	"testing"
)

// The *At transforms must (1) report a byte base that locates each string
// leaf / line inside the source document and (2) produce output byte-for-byte
// identical to their non-offset siblings.

func TestTransformJSONStringLeavesAt_BaseLocatesLeafInSource(t *testing.T) {
	t.Parallel()
	src := `{"a":"contact alice@example.com","b":"then bob@example.com"}`

	type leaf struct {
		value string
		base  int
	}
	var leaves []leaf
	out, err := TransformJSONStringLeavesAt(src, func(value string, base int) (string, error) {
		leaves = append(leaves, leaf{value, base})
		// For escape-free leaves the base must land exactly on the value.
		if got := src[base : base+len(value)]; got != value {
			t.Errorf("base %d does not locate %q, got %q", base, value, got)
		}
		return value, nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(leaves) != 2 {
		t.Fatalf("expected 2 string leaves, got %d: %+v", len(leaves), leaves)
	}
	// Visited in source order, so the second leaf's base is strictly greater.
	if leaves[1].base <= leaves[0].base {
		t.Fatalf("expected increasing bases, got %d then %d", leaves[0].base, leaves[1].base)
	}
	// Identity transform must round-trip to a compacted re-serialisation.
	if !strings.Contains(out, "alice@example.com") || !strings.Contains(out, "bob@example.com") {
		t.Fatalf("unexpected output: %s", out)
	}
}

func TestTransformJSONStringLeavesAt_OutputParity(t *testing.T) {
	t.Parallel()
	cases := []string{
		`{"name":"Alice","city":"New York"}`,
		`{"person":{"name":"Bob","age":30},"tags":["x","y"]}`,
		`["alpha","beta","gamma"]`,
		`{"n":42,"f":1.5,"b":true,"nil":null,"s":"keep"}`,
		`"bare string"`,
		`{"dupe":"first","dupe":"second"}`,
		`{}`,
		`[]`,
	}
	upper := func(s string) (string, error) { return strings.ToUpper(s), nil }
	upperAt := func(s string, _ int) (string, error) { return strings.ToUpper(s), nil }
	for _, in := range cases {
		want, wantErr := TransformJSONStringLeaves(in, upper)
		got, gotErr := TransformJSONStringLeavesAt(in, upperAt)
		if (wantErr == nil) != (gotErr == nil) {
			t.Fatalf("input %q: err mismatch want=%v got=%v", in, wantErr, gotErr)
		}
		if got != want {
			t.Fatalf("input %q: output mismatch\n got %q\nwant %q", in, got, want)
		}
	}
}

func TestTransformJSONStringLeavesAt_InvalidJSON(t *testing.T) {
	t.Parallel()
	for _, bad := range []string{"{broken", "", "{}{}", `{"a":1} trailing`} {
		if _, err := TransformJSONStringLeavesAt(bad, func(s string, _ int) (string, error) {
			return s, nil
		}); err == nil {
			t.Errorf("expected error for %q, got nil", bad)
		}
	}
}

func TestTransformJSONStringLeavesAt_PropagatesCallbackError(t *testing.T) {
	t.Parallel()
	// A callback error must surface unwrapped (not relabelled a JSON parse
	// failure), so callers can distinguish an analyzer error from bad input.
	sentinel := "analyzer boom"
	_, err := TransformJSONStringLeavesAt(`{"a":"x"}`, func(string, int) (string, error) {
		return "", errString(sentinel)
	})
	if err == nil || !strings.Contains(err.Error(), sentinel) {
		t.Fatalf("expected callback error to propagate, got %v", err)
	}
	if strings.Contains(err.Error(), "parse json content") {
		t.Fatalf("callback error must not be relabelled as a parse error, got %v", err)
	}
}

type errString string

func (e errString) Error() string { return string(e) }

func TestTransformLinesAt_BaseLocatesLineInSource(t *testing.T) {
	t.Parallel()
	src := "alpha\nbeta line\ngamma\n"
	var bases []int
	textFn := func(line string, base int) (string, error) {
		bases = append(bases, base)
		if got := src[base : base+len(line)]; got != line {
			t.Errorf("base %d does not locate line %q, got %q", base, line, got)
		}
		return line, nil
	}
	jsonFn := func(line string, base int) (string, error) { return line, nil }
	out, err := TransformLinesAt(src, false, jsonFn, textFn)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != src {
		t.Fatalf("identity transform changed content: got %q want %q", out, src)
	}
	want := []int{0, len("alpha\n"), len("alpha\nbeta line\n")}
	if len(bases) != len(want) {
		t.Fatalf("expected %d line bases, got %d: %v", len(want), len(bases), bases)
	}
	for i := range want {
		if bases[i] != want[i] {
			t.Fatalf("line %d base = %d, want %d", i, bases[i], want[i])
		}
	}
}

func TestTransformLinesAt_OutputParityWithTransformLines(t *testing.T) {
	t.Parallel()
	cases := []string{
		"alpha\nbeta\n",
		"{\"a\":1}\n{\"b\":2}\n",
		"plain\n{\"j\":true}\nmore\n",
		"\n\n",
		"no trailing newline",
		"",
	}
	textFn := func(s string) (string, error) { return "T[" + strings.ToUpper(s) + "]", nil }
	jsonFn := func(s string) (string, error) { return "J[" + s + "]", nil }
	textFnAt := func(s string, _ int) (string, error) { return "T[" + strings.ToUpper(s) + "]", nil }
	jsonFnAt := func(s string, _ int) (string, error) { return "J[" + s + "]", nil }
	for _, in := range cases {
		want, wantErr := TransformLines(in, false, jsonFn, textFn)
		got, gotErr := TransformLinesAt(in, false, jsonFnAt, textFnAt)
		if (wantErr == nil) != (gotErr == nil) {
			t.Fatalf("input %q: err mismatch want=%v got=%v", in, wantErr, gotErr)
		}
		if got != want {
			t.Fatalf("input %q: output mismatch got=%q want=%q", in, got, want)
		}
	}
}
