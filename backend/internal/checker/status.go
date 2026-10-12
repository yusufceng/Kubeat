package checker

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// StatusSet is a set of HTTP status codes.
type StatusSet []statusRange

type statusRange struct{ lo, hi int }

// ParseStatusSet parses a comma-separated list of codes and inclusive
// ranges, such as "200-299,401".
func ParseStatusSet(s string) (StatusSet, error) {
	var set StatusSet
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			return nil, errors.New("empty entry in status list")
		}
		lo, hi, isRange := strings.Cut(part, "-")
		r := statusRange{}
		var err error
		if r.lo, err = parseStatus(lo); err != nil {
			return nil, err
		}
		r.hi = r.lo
		if isRange {
			if r.hi, err = parseStatus(hi); err != nil {
				return nil, err
			}
			if r.hi < r.lo {
				return nil, fmt.Errorf("range %s is reversed", part)
			}
		}
		set = append(set, r)
	}
	return set, nil
}

func parseStatus(s string) (int, error) {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil || n < 100 || n > 599 {
		return 0, fmt.Errorf("%q is not a status code between 100 and 599", s)
	}
	return n, nil
}

// Contains reports whether code is in the set.
func (s StatusSet) Contains(code int) bool {
	for _, r := range s {
		if code >= r.lo && code <= r.hi {
			return true
		}
	}
	return false
}
