/*-
 * Copyright (c) 2016,2017, F5 Networks, Inc.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *    http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package f5router

import (
	"crypto/sha256"
	"os"
	"sort"
	"testing"
	"time"

	"github.com/cf-bigip-ctlr/config"
	"github.com/cf-bigip-ctlr/registry/container"
	"github.com/cf-bigip-ctlr/route"
	"github.com/cf-bigip-ctlr/test_util"

	"code.cloudfoundry.org/routing-api/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var (
	expectedSum = [][32]uint8{
		[32]uint8{0xee, 0x3c, 0x38, 0xcc, 0xbc, 0xe6, 0x43, 0xbf, 0xcd, 0xa9, 0xf, 0xf6, 0xe4, 0x75, 0x2d, 0x6e, 0xdd, 0x48, 0x65, 0x19, 0xce, 0x7, 0x19, 0x62, 0x6, 0x90, 0xc7, 0xdd, 0x80, 0xd4, 0x3b, 0x9},
		[32]uint8{0xaa, 0xf6, 0x45, 0x1a, 0x40, 0x77, 0x87, 0xb0, 0xe0, 0xdc, 0x22, 0xf7, 0x4b, 0x84, 0x20, 0x57, 0x4b, 0x2f, 0x63, 0x4c, 0x3e, 0x0, 0xcb, 0xb3, 0x6b, 0x1a, 0x99, 0xd2, 0x8f, 0x86, 0x30, 0x8a},
		[32]uint8{0xc0, 0xb5, 0x42, 0xed, 0x57, 0xdb, 0x39, 0x68, 0xdf, 0x44, 0x7d, 0x3d, 0x34, 0x12, 0xf, 0x72, 0x26, 0xf4, 0x9e, 0xcc, 0xe, 0xe7, 0xe5, 0xdd, 0x12, 0xd5, 0xe9, 0xce, 0xf6, 0xc4, 0x6f, 0x9d},
	}
)

type testRoutes struct {
	Key         route.Uri
	Addrs       []*route.Endpoint
	ContextPath string
}

type MockWriter struct {
	Input   []byte
	OnWrite func()
}

func (mw *MockWriter) GetOutputFilename() string {
	return "mock-file"
}

func (mw *MockWriter) Write(input []byte) (n int, err error) {
	mw.Input = input

	if nil != mw.OnWrite {
		mw.OnWrite()
	}
	return len(input), nil
}

type MockSignal int

func (ms MockSignal) String() string {
	return "mock signal"
}

func (ms MockSignal) Signal() {
	return
}

func makeConfig() *config.Config {
	c := config.DefaultConfig()
	c.BigIP.URL = "http://example.com"
	c.BigIP.User = "admin"
	c.BigIP.Pass = "pass"
	c.BigIP.Partitions = []string{"cf"}
	c.BigIP.ExternalAddr = "127.0.0.1"

	return c
}

func TestRouteConfigSort(t *testing.T) {
	routeconfigs := routeConfigs{}

	expectedList := make(routeConfigs, 10)

	rc := routeConfig{}
	rc.Item.Backend.ServiceName = "bar"
	rc.Item.Backend.ServicePort = 80
	routeconfigs = append(routeconfigs, &rc)
	expectedList[1] = &rc

	rc = routeConfig{}
	rc.Item.Backend.ServiceName = "foo"
	rc.Item.Backend.ServicePort = 2
	routeconfigs = append(routeconfigs, &rc)
	expectedList[5] = &rc

	rc = routeConfig{}
	rc.Item.Backend.ServiceName = "foo"
	rc.Item.Backend.ServicePort = 8080
	routeconfigs = append(routeconfigs, &rc)
	expectedList[7] = &rc

	rc = routeConfig{}
	rc.Item.Backend.ServiceName = "baz"
	rc.Item.Backend.ServicePort = 1
	routeconfigs = append(routeconfigs, &rc)
	expectedList[2] = &rc

	rc = routeConfig{}
	rc.Item.Backend.ServiceName = "foo"
	rc.Item.Backend.ServicePort = 80
	routeconfigs = append(routeconfigs, &rc)
	expectedList[6] = &rc

	rc = routeConfig{}
	rc.Item.Backend.ServiceName = "foo"
	rc.Item.Backend.ServicePort = 9090
	routeconfigs = append(routeconfigs, &rc)
	expectedList[9] = &rc

	rc = routeConfig{}
	rc.Item.Backend.ServiceName = "baz"
	rc.Item.Backend.ServicePort = 1000
	routeconfigs = append(routeconfigs, &rc)
	expectedList[3] = &rc

	rc = routeConfig{}
	rc.Item.Backend.ServiceName = "foo"
	rc.Item.Backend.ServicePort = 8080
	routeconfigs = append(routeconfigs, &rc)
	expectedList[8] = &rc

	rc = routeConfig{}
	rc.Item.Backend.ServiceName = "foo"
	rc.Item.Backend.ServicePort = 1
	routeconfigs = append(routeconfigs, &rc)
	expectedList[4] = &rc

	rc = routeConfig{}
	rc.Item.Backend.ServiceName = "bar"
	rc.Item.Backend.ServicePort = 1
	routeconfigs = append(routeconfigs, &rc)
	expectedList[0] = &rc

	sort.Sort(routeconfigs)

	for i := range expectedList {
		require.EqualValues(t, expectedList[i], routeconfigs[i],
			"Sorted list elements should be equal")
	}
}

func TestRulesSort(t *testing.T) {
	l7 := rules{}

	expectedList := make(rules, 10)

	p := rule{}
	p.FullURI = "bar"
	l7 = append(l7, &p)
	expectedList[1] = &p

	p = rule{}
	p.FullURI = "foo"
	l7 = append(l7, &p)
	expectedList[5] = &p

	p = rule{}
	p.FullURI = "foo"
	l7 = append(l7, &p)
	expectedList[7] = &p

	p = rule{}
	p.FullURI = "baz"
	l7 = append(l7, &p)
	expectedList[2] = &p

	p = rule{}
	p.FullURI = "foo"
	l7 = append(l7, &p)
	expectedList[6] = &p

	p = rule{}
	p.FullURI = "foo"
	l7 = append(l7, &p)
	expectedList[9] = &p

	p = rule{}
	p.FullURI = "baz"
	l7 = append(l7, &p)
	expectedList[3] = &p

	p = rule{}
	p.FullURI = "foo"
	l7 = append(l7, &p)
	expectedList[8] = &p

	p = rule{}
	p.FullURI = "foo"
	l7 = append(l7, &p)
	expectedList[4] = &p

	p = rule{}
	p.FullURI = "bar"
	l7 = append(l7, &p)
	expectedList[0] = &p

	sort.Sort(l7)

	for i := range expectedList {
		require.EqualValues(t, expectedList[i], l7[i],
			"Sorted list elements should be equal")
	}
}

func TestBadConfig(t *testing.T) {
	logger := test_util.NewTestZapLogger("router-test")
	mw := &MockWriter{}
	c := config.DefaultConfig()

	r, err := NewF5Router(logger, nil, mw)
	assert.Nil(t, r)
	assert.Error(t, err)

	r, err = NewF5Router(logger, c, nil)
	assert.Nil(t, r)
	assert.Error(t, err)

	c.BigIP.URL = "http://example.com"
	r, err = NewF5Router(logger, c, mw)
	assert.Nil(t, r)
	assert.Error(t, err)

	c.BigIP.User = "admin"
	r, err = NewF5Router(logger, c, mw)
	assert.Nil(t, r)
	assert.Error(t, err)

	c.BigIP.Pass = "pass"
	r, err = NewF5Router(logger, c, mw)
	assert.Nil(t, r)
	assert.Error(t, err)

	c.BigIP.Partitions = []string{"cf"}
	r, err = NewF5Router(logger, c, mw)
	assert.Nil(t, r)
	assert.Error(t, err)

	c.BigIP.ExternalAddr = "127.0.0.1"
	r, err = NewF5Router(logger, c, mw)
	assert.NotNil(t, r)
	assert.NoError(t, err)
}

func TestRun(t *testing.T) {
	logger := test_util.NewTestZapLogger("router-test")
	mw := &MockWriter{}
	c := makeConfig()

	router, err := NewF5Router(logger, c, mw)
	require.NotNil(t, router)
	require.NoError(t, err)

	ready := make(chan struct{})
	os := make(chan os.Signal)
	go func() {
		select {
		case _, ok := <-ready:
			assert.False(t, ok, "ready returned and not closed")
		case <-time.After(10 * time.Second):
			require.FailNow(t, "timed out waiting for ready")
		}

		os <- MockSignal(123)
	}()

	done := make(chan struct{})
	go func() {
		assert.NotPanics(t, func() {
			err = router.Run(os, ready)
			assert.NoError(t, err)
			close(done)
		})
	}()

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		require.FailNow(t, "timed out waiting for Run to complete")
	}
}

func makeEndpoints(addrs ...string) []*route.Endpoint {
	var r []*route.Endpoint
	for _, addr := range addrs {
		r = append(r, makeEndpoint(addr))
	}

	return r
}

func makeEndpoint(addr string) *route.Endpoint {
	r := route.NewEndpoint("1",
		addr,
		80,
		"1",
		"1",
		make(map[string]string),
		1,
		"",
		models.ModificationTag{
			Guid:  "1",
			Index: 1,
		},
	)
	return r
}

func createTrie() *container.Trie {
	data := container.NewTrie()

	routes := []testRoutes{
		{
			Key:         "foo.cf.com",
			Addrs:       makeEndpoints("127.0.0.1"),
			ContextPath: "/",
		},
		{
			Key:         "bar.cf.com",
			Addrs:       makeEndpoints("127.0.1.1", "127.0.1.2"),
			ContextPath: "/",
		},
		{
			Key:         "baz.cf.com",
			Addrs:       makeEndpoints("127.0.2.1"),
			ContextPath: "/",
		},
		{
			Key:         "baz.cf.com/segment1",
			Addrs:       makeEndpoints("127.0.3.1", "127.0.3.2"),
			ContextPath: "/segment1",
		},
		{
			Key:         "baz.cf.com/segment1/segment2/segment3",
			Addrs:       makeEndpoints("127.0.4.1", "127.0.4.2"),
			ContextPath: "/segment1/segment2/segment3",
		},
		{
			Key:         "*.cf.com",
			Addrs:       makeEndpoints("127.0.5.1"),
			ContextPath: "/",
		},
		{
			Key:         "*.foo.cf.com",
			Addrs:       makeEndpoints("127.0.6.1"),
			ContextPath: "/",
		},
	}

	for i := range routes {
		pool := route.NewPool(1, routes[i].ContextPath)
		for j := range routes[i].Addrs {
			pool.Put(routes[i].Addrs[j])
		}
		data.Insert(routes[i].Key, pool)
	}

	return data
}

func TestRouteUpdates(t *testing.T) {
	logger := test_util.NewTestZapLogger("router-test")
	mw := &MockWriter{}
	c := makeConfig()

	router, err := NewF5Router(logger, c, mw)
	require.NotNil(t, router)
	require.NoError(t, err)

	data := createTrie()

	// set this after NewF5Router is created as it writes an
	// initial config - either that or make the callback
	// smarter
	wrote := make(chan struct{})
	var update = 0
	mw.OnWrite = func() {
		sum := sha256.Sum256(mw.Input)
		assert.Equal(t, expectedSum[update], sum)
		update++
		wrote <- struct{}{}
	}

	data.EachNodeWithPool(func(t *container.Trie) {
		t.Pool.Each(func(e *route.Endpoint) {
			go func(t *container.Trie, uri string) {
				router.RouteUpdate(
					Add,
					t,
					route.Uri(uri),
				)
			}(data, t.ToPath())
		})
	})

	ready := make(chan struct{})
	os := make(chan os.Signal)
	done := make(chan struct{})
	go func() {
		assert.NotPanics(t, func() {
			err = router.Run(os, ready)
			assert.NoError(t, err)
			close(done)
		})
	}()

	select {
	case <-wrote:
	case <-time.After(10 * time.Second):
		require.FailNow(t, "timed out waiting for config write to complete")
	}

	// make some changes and update the verification function
	p := data.Find(route.Uri("bar.cf.com"))
	require.NotNil(t, p)
	removed := p.Remove(makeEndpoint("127.0.1.1"))
	assert.True(t, removed)

	p = data.Find(route.Uri("baz.cf.com/segment1"))
	require.NotNil(t, p)
	removed = p.Remove(makeEndpoint("127.0.3.2"))
	assert.True(t, removed)

	p = data.Find(route.Uri("baz.cf.com"))
	require.NotNil(t, p)
	added := p.Put(makeEndpoint("127.0.2.2"))
	assert.True(t, added)

	removed = data.Delete(route.Uri("*.foo.cf.com"))
	assert.True(t, removed)

	router.RouteUpdate(
		Remove,
		data,
		route.Uri("*.foo.cf.com"),
	)

	select {
	case <-wrote:
	case <-time.After(10 * time.Second):
		require.FailNow(t, "timed out waiting for config write to complete")
	}

	p = route.NewPool(1, "qux.cf.com")
	p.Put(makeEndpoint("127.0.7.1"))
	data.Insert(route.Uri("qux.cf.com"), p)

	router.RouteUpdate(
		Add,
		data,
		route.Uri("qux.cf.com"),
	)

	select {
	case <-wrote:
	case <-time.After(10 * time.Second):
		require.FailNow(t, "timed out waiting for config write to complete")
	}

	os <- MockSignal(123)
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		require.FailNow(t, "timed out waiting for Run to complete")
	}
}
