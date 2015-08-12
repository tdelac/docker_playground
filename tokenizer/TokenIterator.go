package tokenizer

import (
	"strings"
)

type TokenIterator struct {
	input string
}

func (ti *TokenIterator) HasNext() bool {
	if ti == nil {
		return false
	}

	ti.input = strings.Trim(ti.input, " \n\t")
	if ti.input == "" {
		return false
	}
	return true
}

func (ti *TokenIterator) Next() (string, error) {
	var token string
	if !ti.HasNext() {
		return "", fmt.Errorf("No next token")
	}

	idx := strings.Index(ti.input, " \n\t")
	if idx == -1 {
		token = ti.input
		ti.input = ""
	} else {
		token = ti.input[0:idx]
		ti.input = ti.input[idx:]
	}

	return token
}
