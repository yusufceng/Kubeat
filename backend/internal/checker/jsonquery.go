package checker

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// JSONOperators are the comparisons a JSON check can make between the value
// it found and the expected one.
var JSONOperators = []string{"==", "!=", ">", ">=", "<", "<=", "contains"}

// DefaultJSONOperator applies when a JSON check names none.
const DefaultJSONOperator = "=="

// maxShownValue bounds, in characters, how much of the value a JSON check
// found its result repeats. A short value tells what was found; repeated
// whole, a path that selects a large object would copy most of a response
// into results that everyone who can read the monitor sees.
const maxShownValue = 100

// shownValue is found as a result repeats it: cut after maxShownValue
// characters.
func shownValue(found string) string {
	n := 0
	for i := range found { // i is where each character starts
		if n == maxShownValue {
			return found[:i] + "…"
		}
		n++
	}
	return found
}

// jsonStep is one step of a path: a key of an object or an index of an array.
type jsonStep struct {
	key     string
	index   int
	isIndex bool
}

// ParseJSONPath parses a path such as "status", "checks[0].state" or
// "data.items[2]". Keys are separated by dots; an index in brackets selects
// an element of an array.
func ParseJSONPath(path string) ([]jsonStep, error) {
	var steps []jsonStep
	for i, part := range strings.Split(path, ".") {
		key, brackets, indexed := strings.Cut(part, "[")
		// Only the document itself can be an array without a key before it.
		if key == "" && !(indexed && i == 0) {
			return nil, fmt.Errorf("json path %q has an empty key", path)
		}
		if key != "" {
			steps = append(steps, jsonStep{key: key})
		}
		if !indexed {
			continue
		}
		for _, entry := range strings.Split(brackets, "[") { // "0][1]" -> "0]", "1]"
			number, closed := strings.CutSuffix(entry, "]")
			index, err := strconv.Atoi(number)
			if !closed || err != nil || index < 0 {
				return nil, fmt.Errorf("json path %q: an index is a number in brackets, like [0]", path)
			}
			steps = append(steps, jsonStep{index: index, isIndex: true})
		}
	}
	return steps, nil
}

// matchJSON looks up path in a JSON document and compares what it finds to
// expected. It returns a description of the value on success.
func matchJSON(document []byte, path, operator, expected string) (string, error) {
	steps, err := ParseJSONPath(path)
	if err != nil {
		return "", err
	}
	decoder := json.NewDecoder(bytes.NewReader(document))
	decoder.UseNumber() // keep numbers as written: 1.0 and large integers
	var value any
	if err := decoder.Decode(&value); err != nil {
		return "", errors.New("the response is not valid JSON")
	}

	for _, step := range steps {
		switch node := value.(type) {
		case map[string]any:
			next, ok := node[step.key]
			if step.isIndex || !ok {
				return "", fmt.Errorf("json path %q: nothing at %s", path, describeStep(step))
			}
			value = next
		case []any:
			if !step.isIndex || step.index >= len(node) {
				return "", fmt.Errorf("json path %q: nothing at %s", path, describeStep(step))
			}
			value = node[step.index]
		default:
			return "", fmt.Errorf("json path %q: nothing at %s", path, describeStep(step))
		}
	}

	found := jsonText(value)
	if operator == "" {
		operator = DefaultJSONOperator
	}
	ok, err := compare(found, operator, expected)
	if err != nil {
		return "", fmt.Errorf("json path %q: %w", path, err)
	}
	if !ok {
		return "", fmt.Errorf("json path %q is %s, expected %s %s", path, shownValue(found), operator, expected)
	}
	return path + " = " + shownValue(found), nil
}

func describeStep(s jsonStep) string {
	if s.isIndex {
		return fmt.Sprintf("[%d]", s.index)
	}
	return strconv.Quote(s.key)
}

// jsonText renders a value the way it is compared: strings bare, everything
// else as JSON.
func jsonText(v any) string {
	switch v := v.(type) {
	case string:
		return v
	case json.Number:
		return v.String()
	case nil:
		return "null"
	default:
		b, _ := json.Marshal(v)
		return string(b)
	}
}

// compare applies operator to found and expected. Equality compares numbers
// by value when both sides are numbers, and text otherwise; ordering needs
// numbers on both sides.
func compare(found, operator, expected string) (bool, error) {
	a, aErr := strconv.ParseFloat(found, 64)
	b, bErr := strconv.ParseFloat(expected, 64)
	numeric := aErr == nil && bErr == nil

	switch operator {
	case "==":
		return found == expected || numeric && a == b, nil
	case "!=":
		return !(found == expected || numeric && a == b), nil
	case "contains":
		return strings.Contains(found, expected), nil
	case ">", ">=", "<", "<=":
		if !numeric {
			return false, fmt.Errorf("%s needs numbers, but the value is %s and the expected one %s", operator, shownValue(found), expected)
		}
		switch operator {
		case ">":
			return a > b, nil
		case ">=":
			return a >= b, nil
		case "<":
			return a < b, nil
		default:
			return a <= b, nil
		}
	default:
		return false, fmt.Errorf("unknown operator %q", operator)
	}
}
