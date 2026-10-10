package aliyun

import (
	"time"
)

func parseValidity(start, end *string) (time.Time, time.Time) {
	var nb, na time.Time
	if start != nil {
		nb, _ = time.Parse("2006-01-02", *start)
	}
	if end != nil {
		na, _ = time.Parse("2006-01-02", *end)
	}
	return nb, na
}
