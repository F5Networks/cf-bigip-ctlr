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
 * WITHOUT WARRANTIES OR conditionS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package bigipResources

import (
	"encoding/base64"
	"encoding/json"
)

// NewInternalDataGroup returns a new internal data group
func NewInternalDataGroup(name string) *InternalDataGroup {
	idg := &InternalDataGroup{
		Name:    name,
		Records: []*InternalDataGroupRecord{},
	}
	return idg
}

// AddRecord to the data group
func (idg *InternalDataGroup) AddRecord(name string, data string) {
	_, exists := idg.ReturnRecord(name)
	if !exists {
		newRecord := &InternalDataGroupRecord{
			Name: name,
			Data: data,
		}
		idg.Records = append(idg.Records, newRecord)
	}
}

// RemoveRecord from the data group
func (idg *InternalDataGroup) RemoveRecord(name string) {
	for i := range idg.Records {
		if idg.Records[i].Name == name {
			idg.Records[i] = idg.Records[len(idg.Records)-1]
			idg.Records[len(idg.Records)-1] = nil
			idg.Records = idg.Records[:len(idg.Records)-1]
			break
		}
	}
}

// ReturnRecord and true or nil and false if it does not exist
func (idg *InternalDataGroup) ReturnRecord(name string) (*InternalDataGroupRecord, bool) {
	for i := range idg.Records {
		if idg.Records[i].Name == name {
			return idg.Records[i], true
		}
	}
	return nil, false
}

// ReturnTier2VirtualAddress returns the Data of a record as a VirtualAddress
func (idgr *InternalDataGroupRecord) ReturnTier2VirtualAddress() (*VirtualAddress, error) {
	str, err := base64.StdEncoding.DecodeString(idgr.Data)
	if nil != err {
		return nil, err
	}
	var td *VirtualAddress
	err = json.Unmarshal(str, &td)
	if nil != err {
		return nil, err
	}
	return td, nil
}
