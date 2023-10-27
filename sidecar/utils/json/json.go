// Copyright 2013 The Prometheus Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package jsonutil

import (
	"encoding/json"

	"github.com/pkg/errors"
)

func MarshalJsonString(v interface{}, errorPrompt string) (string, error) {
	js, err := MarshalJson(v, errorPrompt)
	if err != nil {
		return "", err
	}
	return string(js), nil
}

func UnmarshalJsonString(js string, v interface{}, errorPrompt string) error {
	return UnmarshalJson([]byte(js), v, errorPrompt)
}

func MarshalJson(v interface{}, errorPrompt string) ([]byte, error) {
	bytes, err := json.Marshal(v)
	if err != nil {
		return nil, errors.Wrapf(err, "MarshalJson %q error", errorPrompt)
	}
	return bytes, nil
}

func UnmarshalJson(js []byte, v interface{}, errorPrompt string) error {
	err := json.Unmarshal(js, v)
	if err != nil {
		return errors.Wrapf(err, "UnmarshalJson %q error", errorPrompt)
	}
	return nil
}
