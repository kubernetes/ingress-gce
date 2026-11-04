package experiments

import "errors"

type NilArgError struct {
	argName string
}

func NewNilArgError(argName string) error {
	return &NilArgError{argName: argName}
}

func (e *NilArgError) Error() string {
	return "arg " + e.argName + " cannot be nil"
}

func IsNilArgError(err error) bool {
	var nilArgError *NilArgError
	return errors.As(err, &nilArgError)
}
