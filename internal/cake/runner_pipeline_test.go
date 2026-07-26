package cake

import (
	"reflect"
	"testing"
)

// TestPipelineFromFixtures runs each stream fixture through a fake cake
// process and asserts the events delivered by Start match what parsing the
// same fixture directly produces. This is the regression guard for the whole
// chain: subprocess stdout → line reader → ParseLine → typed events.
func TestPipelineFromFixtures(t *testing.T) {
	fixtures := []string{
		"happy-path",
		"error-task",
		"minimal",
		"unknown-type",
		"malformed-midstream",
	}

	for _, name := range fixtures {
		t.Run(name, func(t *testing.T) {
			// Inline the fixture as a quoted heredoc rather than referencing
			// its path, so no checkout path has to survive shell quoting.
			bin := writeFakeCake(t, "cat <<'CAKE_FIXTURE_EOF'\n"+
				string(fixtureData(t, name))+"CAKE_FIXTURE_EOF\n")

			run, err := Start(Options{Bin: bin, Prompt: "hi"})
			if err != nil {
				t.Fatalf("Start: %v", err)
			}
			got, res := collect(t, run)

			if res.ExitCode != 0 || res.Canceled || res.Err != nil {
				t.Errorf("unexpected result: %+v", res)
			}

			want := parseFixture(t, name)
			if len(got) != len(want) {
				t.Fatalf("got %d events, want %d: %#v", len(got), len(want), got)
			}
			for i := range want {
				// ParseError carries an error value that will not compare
				// equal, so check its diagnostic line instead.
				if wantErr, ok := want[i].(ParseError); ok {
					gotErr, ok := got[i].(ParseError)
					if !ok {
						t.Errorf("event %d is %T, want ParseError", i, got[i])
					} else if gotErr.Line != wantErr.Line || gotErr.Err == nil {
						t.Errorf("event %d = %+v, want line %q and a non-nil error", i, gotErr, wantErr.Line)
					}
					continue
				}
				if !reflect.DeepEqual(got[i], want[i]) {
					t.Errorf("event %d = %#v, want %#v", i, got[i], want[i])
				}
			}
		})
	}
}
