package envreq

import (
	"bytes"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
)

// A variable first declared optional and then required is reported missing
// when unset: the stricter declaration is enforced, not the first one cached.
func TestALaterRequiredDeclarationIsEnforced(t *testing.T) {
	Reset()
	t.Cleanup(Reset)
	t.Setenv("ENVREQ_MERGE_REQUIRED", "")
	if first := Check(Requirement{Name: "ENVREQ_MERGE_REQUIRED", Optional: true, EmptyIsUnset: true}); first.Present {
		t.Fatalf("an empty variable read as present: %+v", first)
	}
	second := Check(Requirement{Name: "ENVREQ_MERGE_REQUIRED", Source: "second"})
	if second.Optional {
		t.Fatalf("the later required declaration was answered as optional: %+v", second)
	}
	var out bytes.Buffer
	if missing := Report(&out, CheckAll()); missing != 1 {
		t.Fatalf("Report counted %d missing, want the required variable counted:\n%s", missing, out.String())
	}
}

// A variable later declared Sensitive is redacted from then on, and a
// validator added by a later declaration runs.
func TestALaterSensitiveOrValidatedDeclarationIsEnforced(t *testing.T) {
	Reset()
	t.Cleanup(Reset)
	t.Setenv("ENVREQ_MERGE_SECRET", "hunter2-plaintext")
	t.Setenv("ENVREQ_SHOW_VALUES", "1")
	Check(Requirement{Name: "ENVREQ_MERGE_SECRET", Optional: true})
	if res := Check(Requirement{Name: "ENVREQ_MERGE_SECRET", Optional: true, Sensitive: true}); !res.Sensitive {
		t.Fatalf("the later Sensitive declaration was answered as plain: %+v", res.Requirement)
	}
	var out bytes.Buffer
	Report(&out, CheckAll())
	if strings.Contains(out.String(), "hunter2-plaintext") {
		t.Fatalf("a sensitive value was printed:\n%s", out.String())
	}
	refuse := errors.New("not allowed")
	if res := Check(Requirement{Name: "ENVREQ_MERGE_SECRET", Optional: true, Validate: func(string) error { return refuse }}); !errors.Is(res.Err, refuse) {
		t.Fatalf("a validator added later did not run: %+v", res)
	}
}

// A declaration that merges in while another Check of the same name is still
// judging is not lost: the slower Check's result, judged against the older
// requirement, does not replace the merged one.
func TestAConcurrentStricterDeclarationIsKept(t *testing.T) {
	Reset()
	t.Cleanup(Reset)
	t.Setenv("ENVREQ_MERGE_RACE", "value")
	entered, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	slow := func(string) error {
		if calls.Add(1) == 1 {
			close(entered)
			<-release
		}
		return nil
	}
	done := make(chan Result)
	go func() { done <- Check(Requirement{Name: "ENVREQ_MERGE_RACE", Optional: true, Validate: slow}) }()
	<-entered
	if res := Check(Requirement{Name: "ENVREQ_MERGE_RACE", Optional: true, Sensitive: true}); !res.Sensitive {
		t.Fatalf("the second declaration answered %+v", res.Requirement)
	}
	close(release)
	<-done
	for _, res := range CheckAll() {
		if res.Name == "ENVREQ_MERGE_RACE" && !res.Sensitive {
			t.Fatal("the slower Check stored a result judged against the older, plain requirement")
		}
	}
}
