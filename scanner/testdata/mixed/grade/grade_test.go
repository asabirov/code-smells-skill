package grade

import "testing"

func TestLetter(t *testing.T) {
	if got := Letter(91, false); got != "A-" {
		t.Fatalf("Letter(91) = %q", got)
	}
}
