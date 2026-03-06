package status

import (
	"fmt"
)

type Status struct {
	value string
}

var (
	Waiting    = new("waiting")
	Processing = new("processing")
	Completed  = new("completed")
	Error      = new("error")
)

var statusList = map[string]Status{}

func new(value string) Status {
	s := Status{value: value}
	statusList[value] = s
	return s
}

func (s Status) String() string {
	return s.value
}

func (s Status) Equal(other Status) bool {
	return s.value == other.value
}

func (s Status) TextMarshall() ([]byte, error) {
	return []byte(s.value), nil
}

func Parse(value string) (Status, error) {
	if s, exists := statusList[value]; exists {
		return s, nil
	}

	return Status{}, fmt.Errorf("unknown status: %s", value)
}
