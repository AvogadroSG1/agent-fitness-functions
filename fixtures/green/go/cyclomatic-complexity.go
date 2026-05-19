//go:build fixture

package demo

func RouteScore(kind string, retries int, urgent bool) int {
	score := map[string]int{
		"create": 1,
		"update": 1,
		"delete": 1,
		"manual": 1,
		"batch":  1,
		"sync":   1,
	}[kind]
	if urgent {
		score++
	}
	if retries > 0 {
		score += min(retries, 3)
	}
	return score
}
