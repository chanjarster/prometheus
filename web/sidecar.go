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

package web

import (
	"encoding/json"
	"net/http"

	jsoniter "github.com/json-iterator/go"

	"github.com/go-kit/log/level"

	"github.com/prometheus/prometheus/sidecar"
)

type SidecarResponse struct {
	Code    int         `json:"code"`
	Message string      `json:"message,omitempty"`
	Error   string      `json:"error,omitempty"`
	Data    interface{} `json:"data,omitempty"`
}

type sidecarApiFuncResult struct {
	data interface{}
	err  *sidecarApiError
}

type sidecarApiError struct {
	code    int
	summary string
	err     error
}

type sidecarApiFunc func(r *http.Request) sidecarApiFuncResult

func (h *Handler) wrapSidecarApi(f sidecarApiFunc) http.HandlerFunc {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		result := f(r)
		if result.err != nil {
			h.respondSidecarError(w, result.err, result.data)
			return
		}
		h.respondSidecar(w, result.data)
	})
}

func (h *Handler) respondSidecar(w http.ResponseWriter, data interface{}) {
	json := jsoniter.ConfigCompatibleWithStandardLibrary
	b, err := json.Marshal(&SidecarResponse{
		Code:    http.StatusOK,
		Message: "success",
		Data:    data,
	})
	if err != nil {
		level.Error(h.logger).Log("msg", "error marshaling json response", "err", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	if n, err := w.Write(b); err != nil {
		level.Error(h.logger).Log("msg", "error writing response", "bytesWritten", n, "err", err)
	}
}

func (h *Handler) respondSidecarError(w http.ResponseWriter, apiErr *sidecarApiError, data interface{}) {
	json := jsoniter.ConfigCompatibleWithStandardLibrary
	b, err := json.Marshal(&SidecarResponse{
		Code:    apiErr.code,
		Message: apiErr.summary,
		Error:   apiErr.err.Error(),
		Data:    data,
	})
	if err != nil {
		level.Error(h.logger).Log("msg", "error marshaling json response", "err", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(apiErr.code)
	if n, err := w.Write(b); err != nil {
		level.Error(h.logger).Log("msg", "error writing response", "bytesWritten", n, "err", err)
	}
}

// EXTENSION: 扩展的 sidecar 功能
func (h *Handler) updateConfig(q *http.Request) sidecarApiFuncResult {
	level.Info(h.logger).Log("msg", "Refreshing configuration")
	var cmd sidecar.UpdateConfigCmd
	err := json.NewDecoder(q.Body).Decode(&cmd)
	if err != nil {
		return sidecarApiFuncResult{
			err: &sidecarApiError{code: http.StatusBadRequest, summary: "Parse request json error", err: err},
		}
	}
	err = h.sidecarSvc.UpdateConfigReload(q.Context(), &cmd, h.reloadCh)
	if err != nil {
		return sidecarApiFuncResult{
			err: &sidecarApiError{code: http.StatusInternalServerError, summary: "Update configuration error", err: err},
		}
	}
	level.Info(h.logger).Log("msg", "Completed refreshing configuration")
	return sidecarApiFuncResult{}
}

// EXTENSION: 扩展的 sidecar 功能
func (h *Handler) getRuntimeInfo(*http.Request) sidecarApiFuncResult {
	return sidecarApiFuncResult{data: h.sidecarSvc.GetRuntimeInfo()}
}

// EXTENSION: 扩展的 sidecar 功能
func (h *Handler) resetConfig(q *http.Request) sidecarApiFuncResult {
	level.Info(h.logger).Log("msg", "Resetting configuration")
	var cmd sidecar.ResetConfigCmd
	err := json.NewDecoder(q.Body).Decode(&cmd)
	if err != nil {
		return sidecarApiFuncResult{
			err: &sidecarApiError{code: http.StatusBadRequest, summary: "Parse request json error", err: err},
		}
	}
	err = h.sidecarSvc.ResetConfigReload(q.Context(), cmd.ZoneId, h.reloadCh)
	if err != nil {
		return sidecarApiFuncResult{
			err: &sidecarApiError{code: http.StatusInternalServerError, summary: "Reset configuration error", err: err},
		}
	}

	level.Info(h.logger).Log("msg", "Completed resetting configuration")
	return sidecarApiFuncResult{}
}

// EXTENSION: 扩展的 sidecar 功能
func (h *Handler) testScrapeConfig(q *http.Request) sidecarApiFuncResult {
	var cmd sidecar.TestScrapeConfigCmd
	err := json.NewDecoder(q.Body).Decode(&cmd)
	if err != nil {
		return sidecarApiFuncResult{
			err: &sidecarApiError{code: http.StatusBadRequest, summary: "Parse request json error", err: err},
		}
	}
	result, err := h.sidecarSvc.TestScrapeConfig(&cmd)
	if err != nil {
		return sidecarApiFuncResult{
			err: &sidecarApiError{code: http.StatusInternalServerError, summary: "Test scrape config error", err: err},
		}
	}
	return sidecarApiFuncResult{data: result}
}

// EXTENSION: 扩展的 sidecar 功能
func (h *Handler) testScrapeJobs(q *http.Request) sidecarApiFuncResult {
	var cmd sidecar.TestScrapeJobsCmd
	err := json.NewDecoder(q.Body).Decode(&cmd)
	if err != nil {
		return sidecarApiFuncResult{
			err: &sidecarApiError{code: http.StatusBadRequest, summary: "Parse request json error", err: err},
		}
	}
	result, err := h.sidecarSvc.TestScrapeJobs(cmd.JobNames)
	if err != nil {
		return sidecarApiFuncResult{
			err: &sidecarApiError{code: http.StatusInternalServerError, summary: "Test scrape jobs error", err: err},
		}
	}
	return sidecarApiFuncResult{data: result}
}

// EXTENSION: 扩展的 sidecar 功能
func (h *Handler) testRemoteWriteConfig(q *http.Request) sidecarApiFuncResult {
	var cmd sidecar.TestRemoteWriteConfigCmd
	err := json.NewDecoder(q.Body).Decode(&cmd)
	if err != nil {
		return sidecarApiFuncResult{
			err: &sidecarApiError{code: http.StatusBadRequest, summary: "Parse request json error", err: err},
		}
	}
	result, err := h.sidecarSvc.TestRemoteWriteConfig(&cmd)
	if err != nil {
		return sidecarApiFuncResult{
			err: &sidecarApiError{code: http.StatusInternalServerError, summary: "Test remote write config error", err: err},
		}
	}
	return sidecarApiFuncResult{data: result}
}

// EXTENSION: 扩展的 sidecar 功能
func (h *Handler) testRemoteWriteRemotes(q *http.Request) sidecarApiFuncResult {
	var cmd sidecar.TestRemoteWriteRemotesCmd
	err := json.NewDecoder(q.Body).Decode(&cmd)
	if err != nil {
		return sidecarApiFuncResult{
			err: &sidecarApiError{code: http.StatusBadRequest, summary: "Parse request json error", err: err},
		}
	}
	result, err := h.sidecarSvc.TestRemoteWriteRemotes(cmd.RemoteNames)
	if err != nil {
		return sidecarApiFuncResult{
			err: &sidecarApiError{code: http.StatusInternalServerError, summary: "Test remote write remotes error", err: err},
		}
	}
	return sidecarApiFuncResult{data: result}
}

// Checks if server is ready, calls f if it is, returns 503 if it is not.
func (h *Handler) testSidecarReady(f http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if h.isReady() {
			f(w, r)
		} else {
			b := []byte(`{"code":503,"message":"Service Unavailable"}`)
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusServiceUnavailable)
			if n, err := w.Write(b); err != nil {
				level.Error(h.logger).Log("msg", "error writing response", "bytesWritten", n, "err", err)
			}
		}
	}
}

// Checks if server is ready, calls f if it is, returns 503 if it is not.
func (h *Handler) sidecarAPINotEnabled(w http.ResponseWriter, _ *http.Request) {
	b := []byte(`{"code":403,"message":"Lifecycle API is not enabled"}`)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusForbidden)
	if n, err := w.Write(b); err != nil {
		level.Error(h.logger).Log("msg", "error writing response", "bytesWritten", n, "err", err)
	}
}
