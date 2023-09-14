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
	"fmt"
	"net/http"

	"github.com/go-kit/log/level"

	"github.com/prometheus/prometheus/sidecar"
)

// EXTENSION: 扩展的 sidecar 功能
func (h *Handler) updateConfig(w http.ResponseWriter, q *http.Request) {
	h.logger.Log("msg", "Refreshing configuration")

	var cmd sidecar.UpdateConfigCmd
	err := json.NewDecoder(q.Body).Decode(&cmd)
	if err != nil {
		errmsg := fmt.Sprintf("Parse request json error: %s", err.Error())
		h.logger.Log("msg", errmsg)
		http.Error(w, errmsg, http.StatusBadRequest)
		return
	}
	err = h.sidecarSvc.UpdateConfigReload(q.Context(), &cmd, h.reloadCh)
	if err != nil {
		errmsg := fmt.Sprintf("Update configuration error: %s", err.Error())
		h.logger.Log("msg", errmsg)
		http.Error(w, errmsg, http.StatusInternalServerError)
	}

	w.WriteHeader(http.StatusOK)
	_, err = w.Write([]byte(`{"code":200,"message":"success"}`))
	if err != nil {
		level.Error(h.logger).Log("err", err)
	}

	h.logger.Log("msg", "Completed refreshing configuration")
}

// EXTENSION: 扩展的 sidecar 功能
func (h *Handler) getLastUpdateTs(w http.ResponseWriter, q *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, err := w.Write([]byte(fmt.Sprintf(`{"code":200,"message":"success","lastUpdateTs":%d}`, h.sidecarSvc.GetLastUpdateTs().Unix())))
	if err != nil {
		level.Error(h.logger).Log("err", err)
	}
}
