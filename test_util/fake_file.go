/*
 * Portions Copyright (c) 2017, F5 Networks, Inc.
 */

package test_util

import "sync"

type FakeFile struct {
	mutex   sync.Mutex
	payload []byte
}

func (f *FakeFile) Write(data []byte) (int, error) {
	f.mutex.Lock()
	f.payload = data
	f.mutex.Unlock()
	return len(data), nil
}

func (f *FakeFile) Read(data *[]byte) (int, error) {
	f.mutex.Lock()
	*data = f.payload
	f.mutex.Unlock()
	return len(*data), nil
}
