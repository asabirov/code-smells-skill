package grade

import (
	"fmt"
	"strings"
)

// Describe writes a short report card. It is long and flat, so a copy of it makes a
// duplicate group.
func Describe(name string, scores []int, curved bool) string {
	var b strings.Builder
	total, best, worst := 0, 0, 100
	for _, s := range scores {
		total += s
		best = max(best, s)
		worst = min(worst, s)
	}
	average := 0
	if len(scores) > 0 {
		average = total / len(scores)
	}
	fmt.Fprintf(&b, "Report card for %s\n", strings.TrimSpace(name))
	fmt.Fprintf(&b, "  scores:  %v\n", scores)
	fmt.Fprintf(&b, "  average: %d (%s)\n", average, Letter(average, curved))
	fmt.Fprintf(&b, "  best:    %d (%s)\n", best, Letter(best, curved))
	fmt.Fprintf(&b, "  worst:   %d (%s)\n", worst, Letter(worst, curved))
	fmt.Fprintf(&b, "  curved:  %t\n", curved)
	return b.String()
}
