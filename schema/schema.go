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

package schema

import (
	"os"

	"github.com/F5Networks/cf-bigip-ctlr/logger"
	"github.com/uber-go/zap"
	"github.com/xeipuuv/gojsonschema"
)

// the newest version always needs to be at index 0
var schemaVersions = []string{
	"cf-schema_v1.1.0.json",
}

// VerifySchema takes a json string and compares it to all known versions
// of the schema. Matching against an older version casuses a warning
func VerifySchema(data string, logger logger.Logger) (bool, error) {
	logger.Session("verify-schema")
	folderPath, err := os.Getwd()
	if nil != err {
		return false, err
	}
	// check the json against all versions of the schema starting with the latest
	for i, schema := range schemaVersions {
		schemaLocation := "file://" + folderPath + "/src/f5/schemas/" + schema
		schemaLoader := gojsonschema.NewReferenceLoader(schemaLocation)
		documentLoader := gojsonschema.NewStringLoader(data)
		result, err := gojsonschema.Validate(schemaLoader, documentLoader)
		if err != nil {
			return false, err
		}
		if result.Valid() {
			// if we are valid but is not the latest warn the user
			if i != 0 {
				logger.Warn(
					"schema-version-is-old",
					zap.String("current-verion", schemaVersions[0]),
					zap.String("version-used", schema),
				)
			}
			return result.Valid(), nil
		}
		logger.Warn("schema-not-valid",
			zap.String("current-verion", schemaVersions[0]),
			zap.String("version-compared-against", schema),
			zap.Object("errors", result.Errors()),
		)
	}
	return false, nil
}
