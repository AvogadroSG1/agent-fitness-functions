//go:build fixture

package demo

func RouteScore(kind string, retries int, urgent bool) int {
	score := 0
	if kind == "create" {
		score++
	}
	if kind == "update" {
		score++
	}
	if kind == "delete" {
		score++
	}
	if retries > 0 {
		score++
	}
	if retries > 1 {
		score++
	}
	if retries > 2 {
		score++
	}
	if urgent {
		score++
	}
	if kind == "manual" {
		score++
	}
	if kind == "batch" {
		score++
	}
	if kind == "sync" {
		score++
	}
	return score
}
