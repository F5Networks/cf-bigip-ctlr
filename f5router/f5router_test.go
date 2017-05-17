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
	"fmt"
	"sort"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSort(t *testing.T) {
	l7 := policies{}

	expectedList := make(policies, 10)

	p := policy{}
	p.FullURI = "bar"
	l7 = append(l7, &p)
	expectedList[1] = &p

	p = policy{}
	p.FullURI = "foo"
	l7 = append(l7, &p)
	expectedList[5] = &p

	p = policy{}
	p.FullURI = "foo"
	l7 = append(l7, &p)
	expectedList[7] = &p

	p = policy{}
	p.FullURI = "baz"
	l7 = append(l7, &p)
	expectedList[2] = &p

	p = policy{}
	p.FullURI = "foo"
	l7 = append(l7, &p)
	expectedList[6] = &p

	p = policy{}
	p.FullURI = "foo"
	l7 = append(l7, &p)
	expectedList[9] = &p

	p = policy{}
	p.FullURI = "baz"
	l7 = append(l7, &p)
	expectedList[3] = &p

	p = policy{}
	p.FullURI = "foo"
	l7 = append(l7, &p)
	expectedList[8] = &p

	p = policy{}
	p.FullURI = "foo"
	l7 = append(l7, &p)
	expectedList[4] = &p

	p = policy{}
	p.FullURI = "bar"
	l7 = append(l7, &p)
	expectedList[0] = &p

	sort.Sort(l7)

	for i := range expectedList {
		require.EqualValues(t, expectedList[i], l7[i],
			"Sorted list elements should be equal")
	}
}

func TestUpdateEndpoints(t *testing.T) {

}
