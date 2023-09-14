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

package errs

import (
	"fmt"
	"io"
	"net/http"
	"strings"

	jsonutil "github.com/prometheus/prometheus/sidecar/utils/json"
)

type ErrBadParameter string

func (e ErrBadParameter) Error() string {
	return string(e)
}

type ErrUnauthorized string

func (e ErrUnauthorized) Error() string {
	return string(e)
}

type ErrForbidden string

func (e ErrForbidden) Error() string {
	return string(e)
}

type ErrNotFound string

func (e ErrNotFound) Error() string {
	return string(e)
}

// NotFoundErrorf uses fmt.Sprintf to build a not found error.
func NotFoundErrorf(format string, args ...interface{}) ErrNotFound {
	return ErrNotFound(fmt.Sprintf(format, args...))
}

type ValidateError string

func (e ValidateError) Error() string {
	return string(e)
}

// ValidateErrorf uses fmt.Sprintf to build a not found error.
func ValidateErrorf(format string, args ...interface{}) ValidateError {
	return ValidateError(fmt.Sprintf(format, args...))
}

func (e ValidateError) Prefix(prefix string) ValidateError {
	return ValidateError(fmt.Sprintf("%s%v", prefix, e))
}

func (e ValidateError) Prefixf(template string, args ...interface{}) ValidateError {
	return e.Prefix(fmt.Sprintf(template, args...))
}

func (e ValidateError) Suffix(suffix string) ValidateError {
	return ValidateError(fmt.Sprintf("%v%s", e, suffix))
}

func (e ValidateError) Suffixf(template string, args ...interface{}) ValidateError {
	return e.Suffix(fmt.Sprintf(template, args...))
}

type ValidateErrors []ValidateError

func (es ValidateErrors) Error() string {
	sb := strings.Builder{}
	sb.WriteString("[")
	for i, e := range es {
		sb.WriteString(string(e))
		if i < len(es)-1 {
			sb.WriteString(", ")
		}
	}
	sb.WriteString("]")
	return sb.String()
}

func (es ValidateErrors) Prefix(prefix string) ValidateErrors {
	if len(es) == 0 {
		return es
	}
	nes := make(ValidateErrors, len(es))
	for i, e := range es {
		nes[i] = e.Prefix(prefix)
	}
	return nes
}

func (es ValidateErrors) Prefixf(template string, args ...interface{}) ValidateErrors {
	if len(es) == 0 {
		return es
	}
	p := fmt.Sprintf(template, args...)
	return es.Prefix(p)
}

func (es ValidateErrors) Suffix(suffix string) ValidateErrors {
	if len(es) == 0 {
		return es
	}
	nes := make(ValidateErrors, len(es))
	for i, e := range es {
		nes[i] = e.Suffix(suffix)
	}
	return nes
}

func (es ValidateErrors) Suffixf(template string, args ...interface{}) ValidateErrors {
	if len(es) == 0 {
		return es
	}
	p := fmt.Sprintf(template, args...)
	return es.Suffix(p)
}

type HttpResponseError struct {
	StatusCode   int    `json:"status_code"`
	ErrorMessage string `json:"error_message,omitempty"`
	Body         string `json:"body"`
}

func (e *HttpResponseError) Error() string {
	jsonString, err := jsonutil.MarshalJsonString(e, "HttpResponseError")
	if err != nil {
		return err.Error()
	}
	return jsonString
}

func (e *HttpResponseError) Populate(resp *http.Response) {
	e.StatusCode = resp.StatusCode
	bytes, err := io.ReadAll(resp.Body)
	if err != nil {
		e.Body = fmt.Sprintf("read response failed: %v", err)
	} else {
		e.Body = string(bytes)
	}
}

func NewHttpResponseError(resp *http.Response) *HttpResponseError {
	herr := &HttpResponseError{}
	herr.Populate(resp)
	return herr
}

type ErrorsPkg []error

func (p ErrorsPkg) Error() string {
	emsgs := make([]string, 0, len(p))
	for _, err := range p {
		emsgs = append(emsgs, err.Error())
	}
	return strings.Join(emsgs, ", ")
}
