package llm

import "fmt"

var (
	Default  = new("")
	KronkAPI = new("kronk-api")
	KronkSDK = new("kronk-sdk")
)

type Provider struct {
	value string
}

var providersList []Provider

func new(value string) Provider {
	v := Provider{value: value}
	providersList = append(providersList, v)
	return v
}

func (t Provider) String() string {
	return t.value
}

func (t Provider) Equal(other Provider) bool {
	return t.value == other.value
}

func (t Provider) TextMarshall() ([]byte, error) {
	return []byte(t.value), nil
}

func Parse(value string) (Provider, error) {
	for _, p := range providersList {
		if p.value == value {
			return p, nil
		}
	}
	return Provider{}, fmt.Errorf("unknown provider: %s", value)
}
