package spec

import (
	"fmt"
	"strings"
	"testing"
)

func TestASampleIsAFractionOfRunsAndTheSameOnesEveryTime(t *testing.T) {
	picked := map[string]bool{}
	count := 0
	for i := range 4000 {
		id := fmt.Sprintf("run-%d", i)
		got, err := EvalBool(`sample(run.id + "/approve", 0.1)`, Scope{Run: map[string]any{"id": id}})
		if err != nil {
			t.Fatal(err)
		}
		picked[id] = got
		if got {
			count++
		}
	}
	// One in ten of 4,000 is 400. A fair pick lands well inside 320 to 480.
	if count < 320 || count > 480 {
		t.Errorf("%d of 4000 runs were picked at a rate of 0.1, want about 400", count)
	}
	// Asked again, every run gets the answer it got before: a retry cannot
	// dodge a review, and the pick can be checked afterwards.
	for id, was := range picked {
		got, _ := EvalBool(`sample(run.id + "/approve", 0.1)`, Scope{Run: map[string]any{"id": id}})
		if got != was {
			t.Fatalf("run %s was picked=%v and then picked=%v", id, was, got)
		}
	}
	// A second step with its own seed picks a different set of runs.
	same := 0
	for id, was := range picked {
		got, _ := EvalBool(`sample(run.id + "/sign", 0.1)`, Scope{Run: map[string]any{"id": id}})
		if got && was {
			same++
		}
	}
	if same > count/2 {
		t.Errorf("%d of the %d runs picked at one step were picked at another too: the seeds are not independent", same, count)
	}
}

func TestASampleOfNoneOrAllAndARateThatIsNotOne(t *testing.T) {
	for _, id := range []string{"a", "b", "c", "d", "e"} {
		scope := Scope{Run: map[string]any{"id": id}}
		if got, err := EvalBool(`sample(run.id, 0)`, scope); err != nil || got {
			t.Errorf("a rate of 0 picked run %s (%v)", id, err)
		}
		if got, err := EvalBool(`sample(run.id, 1)`, scope); err != nil || !got {
			t.Errorf("a rate of 1 passed over run %s (%v)", id, err)
		}
	}
	for _, rate := range []string{"1.5", "-0.1", "2"} {
		_, err := EvalBool(`sample(run.id, `+rate+`)`, Scope{Run: map[string]any{"id": "a"}})
		if err == nil || !strings.Contains(err.Error(), "between 0 and 1") {
			t.Errorf("a rate of %s: err = %v, want one that says what a rate may be", rate, err)
		}
	}
	// The seed is text and the rate a number: anything else is refused when
	// the workflow is published, not when a run reaches it.
	if err := CheckExpression(`sample(0.1, run.id)`); err == nil {
		t.Error("a sample with its seed and rate the wrong way round was accepted")
	}
}
