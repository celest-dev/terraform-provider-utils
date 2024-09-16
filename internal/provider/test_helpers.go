package provider

import (
	"fmt"
)

type listOfGreaterThan struct {
	count int
}

func (l listOfGreaterThan) CheckValue(v any) error {
	val, ok := v.([]any)

	if !ok {
		return fmt.Errorf("expected []any value for ListOfGreaterThan check, got: %T", v)
	}

	if len(val) <= l.count {
		return fmt.Errorf("expected list length to be greater than %d, but got: %v (%d elements)", l.count, val, len(val))
	}

	return nil
}

func (l listOfGreaterThan) String() string {
	return fmt.Sprintf("list of length greater than %d", l.count)
}

type listNotEmpty struct{}

func (listNotEmpty) CheckValue(v any) error {
	val, ok := v.([]any)

	if !ok {
		return fmt.Errorf("expected []any value for ListNotEmpty check, got: %T", v)
	}

	if len(val) == 0 {
		return fmt.Errorf("expected non-empty list for ListNotEmpty check, but list was empty")
	}

	return nil
}

func (listNotEmpty) String() string {
	return "non-empty list"
}

type listEquals struct {
	values []string
}

func (l listEquals) CheckValue(v any) error {
	val, ok := v.([]any)

	if !ok {
		return fmt.Errorf("expected []any value for ListEquals check, got: %T", v)
	}

	if len(val) != len(l.values) {
		return fmt.Errorf("expected list values to be equal for ListEquals check, but got: %v", val)
	}

	for i, v := range val {
		if v != l.values[i] {
			return fmt.Errorf("expected list values to be equal for ListEquals check, but got: %v", val)
		}
	}

	return nil
}

func (l listEquals) String() string {
	return fmt.Sprintf("list equals: %v", l.values)
}
