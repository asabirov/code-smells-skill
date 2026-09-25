// Package grade turns scores into letters. Letter is over the complexity limit on
// purpose, so the fixture's baseline has a cap.
package grade

func Letter(score int, curved bool) string {
	if curved {
		score += 5
	}
	for score > 100 {
		score -= 10
		if score < 0 && curved {
			return "?"
		}
	}
	if score >= 90 {
		if curved || score > 95 {
			if score%2 == 0 {
				return "A+"
			}
			return "A"
		}
		return "A-"
	} else if score >= 80 {
		switch {
		case score > 85 && curved:
			return "B+"
		case score > 85 || !curved:
			return "B"
		}
		return "B-"
	} else if score >= 70 {
		return "C"
	}
	return "F"
}
