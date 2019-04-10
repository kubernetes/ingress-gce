package utils

import "sync"

type ErrorList struct {
	errList []error
	lock    sync.Mutex
}

func (e *ErrorList) Add(err error) {
	e.lock.Lock()
	defer e.lock.Unlock()
	e.errList = append(e.errList, err)
}

func (e *ErrorList) List() []error {
	e.lock.Lock()
	defer e.lock.Unlock()
	return e.errList
}
