package childproc

import (
	"reflect"
	"testing"
	"time"
)

func TestWaitForReapTreatsNilAsReaped(t *testing.T) {
	got := WaitForReap([]Operation{{Name: "child"}}, time.Second)
	if len(got) != 0 {
		t.Fatalf("WaitForReap(nil channel) = %v, want none pending", got)
	}
}

func TestWaitForReapReportsPendingNamesSorted(t *testing.T) {
	never := make(chan struct{})
	got := WaitForReap([]Operation{
		{Name: "run", Done: never},
		{Name: "replay", Done: never},
	}, 20*time.Millisecond)
	want := []string{"replay", "run"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("WaitForReap = %v, want %v", got, want)
	}
}

func TestWaitForReapReturnsWhenEveryChannelCloses(t *testing.T) {
	first := make(chan struct{})
	second := make(chan struct{})
	close(first)
	close(second)
	got := WaitForReap([]Operation{
		{Name: "first", Done: first},
		{Name: "second", Done: second},
	}, time.Second)
	if len(got) != 0 {
		t.Fatalf("WaitForReap(closed channels) = %v, want none pending", got)
	}
}

// TestWaitForReapKeepsEveryRepeatedName checks that a name is only a report
// label: a closed operation must not stand in for a live operation that shares
// its name, and the name is reported once.
func TestWaitForReapKeepsEveryRepeatedName(t *testing.T) {
	reaped := make(chan struct{})
	close(reaped)
	live := make(chan struct{})
	got := WaitForReap([]Operation{
		{Name: "run", Done: reaped},
		{Name: "run", Done: live},
	}, 20*time.Millisecond)
	want := []string{"run"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("WaitForReap = %v, want %v", got, want)
	}
}
