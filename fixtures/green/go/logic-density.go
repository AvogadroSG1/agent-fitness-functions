//go:build fixture

package demo

import "fmt"

func Dense(value int) string {
	total := value
	if total < 0 {
		total = -total
	}
	total++
	return fmt.Sprint(total)
}
