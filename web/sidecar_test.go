// Copyright 2016 The Prometheus Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package web

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/go-kit/log"

	"github.com/prometheus/prometheus/sidecar"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/prometheus/prometheus/config"
	"github.com/prometheus/prometheus/notifier"
	"github.com/prometheus/prometheus/rules"
	"github.com/prometheus/prometheus/scrape"
	"github.com/prometheus/prometheus/tsdb"
	"github.com/prometheus/prometheus/util/testutil"
)

func TestSidecarAPI_update_config(t *testing.T) {
	t.Parallel()

	dbDir := t.TempDir()

	db, err := tsdb.Open(dbDir, nil, nil, nil, nil)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, db.Close())
	})
	port := fmt.Sprintf(":%d", testutil.RandomUnprivilegedPort(t))

	opts := &Options{
		ListenAddress:   port,
		ReadTimeout:     30 * time.Second,
		MaxConnections:  512,
		Context:         nil,
		Storage:         nil,
		LocalStorage:    &dbAdapter{db},
		TSDBDir:         dbDir,
		QueryEngine:     nil,
		ScrapeManager:   &scrape.Manager{},
		RuleManager:     &rules.Manager{},
		Notifier:        nil,
		RoutePrefix:     "/",
		EnableAdminAPI:  true,
		EnableLifecycle: true,
		ExternalURL: &url.URL{
			Scheme: "http",
			Host:   "localhost" + port,
			Path:   "/",
		},
		Version:  &PrometheusVersion{},
		Gatherer: prometheus.DefaultGatherer,
	}

	opts.Flags = map[string]string{}

	logger := log.NewLogfmtLogger(os.Stdout)
	sidecarSvc := sidecar.New(logger, "../test-data/prometheus.yml")
	webHandler := New(logger, opts, sidecarSvc)

	webHandler.config = &config.Config{}
	webHandler.notifier = &notifier.Manager{}
	l, err := webHandler.Listener()
	if err != nil {
		panic(fmt.Sprintf("Unable to start web listener: %s", err))
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		err := webHandler.Run(ctx, l, "")
		if err != nil {
			panic(fmt.Sprintf("Can't start web handler:%s", err))
		}
	}()

	// Give some time for the web goroutine to run since we need the server
	// to be up before starting tests.
	time.Sleep(5 * time.Second)

	baseURL := "http://localhost" + port
	cmdJson := `{
    "zone_id": "default",
    "yaml": "global:\n  scrape_interval: 15s\n  scrape_timeout: 10s\n  evaluation_interval: 15s\nalerting:\n  alertmanagers:\n  - follow_redirects: true\n    enable_http2: true\n    scheme: http\n    timeout: 10s\n    api_version: v2\n    static_configs:\n    - targets: []\nscrape_configs:\n- job_name: prometheus\n  honor_timestamps: true\n  scrape_interval: 15s\n  scrape_timeout: 10s\n  metrics_path: /metrics\n  scheme: http\n  follow_redirects: true\n  enable_http2: true\n  static_configs:\n  - targets:\n    - localhost:9090\n",
    "rule_files": [
        {
            "filename": "a.yaml",
            "yaml": "groups:\n  - name: test_2\n    rules:\n      - record: test_2\n        expr: vector(2)"
        }
    ],
    "secret_files": [
        {
            "secret": "reprehenderit cillum enim",
            "filename": "a.secret"
        }
    ]
}`

	req, err := http.NewRequest(http.MethodPut, baseURL+"/-/sidecar/config", strings.NewReader(cmdJson))
	require.NoError(t, err)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	require.Equal(t, http.StatusServiceUnavailable, resp.StatusCode)
	cleanupTestResponse(t, resp)

	// Set to ready.
	webHandler.SetReady(true)

	go func() {
		rc := <-webHandler.Reload()
		rc <- nil
	}()

	req, err = http.NewRequest(http.MethodPut, baseURL+"/-/sidecar/config", strings.NewReader(cmdJson))
	require.NoError(t, err)
	resp, err = http.DefaultClient.Do(req)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	cleanupTestResponse(t, resp)
}

func TestSidecarAPI_last_update_ts(t *testing.T) {
	t.Parallel()

	dbDir := t.TempDir()

	db, err := tsdb.Open(dbDir, nil, nil, nil, nil)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, db.Close())
	})
	port := fmt.Sprintf(":%d", testutil.RandomUnprivilegedPort(t))

	opts := &Options{
		ListenAddress:   port,
		ReadTimeout:     30 * time.Second,
		MaxConnections:  512,
		Context:         nil,
		Storage:         nil,
		LocalStorage:    &dbAdapter{db},
		TSDBDir:         dbDir,
		QueryEngine:     nil,
		ScrapeManager:   &scrape.Manager{},
		RuleManager:     &rules.Manager{},
		Notifier:        nil,
		RoutePrefix:     "/",
		EnableAdminAPI:  true,
		EnableLifecycle: true,
		ExternalURL: &url.URL{
			Scheme: "http",
			Host:   "localhost" + port,
			Path:   "/",
		},
		Version:  &PrometheusVersion{},
		Gatherer: prometheus.DefaultGatherer,
	}

	opts.Flags = map[string]string{}

	logger := log.NewLogfmtLogger(os.Stdout)
	sidecarSvc := sidecar.New(logger, "../test-data/prometheus.yml")
	webHandler := New(logger, opts, sidecarSvc)

	webHandler.config = &config.Config{}
	webHandler.notifier = &notifier.Manager{}
	l, err := webHandler.Listener()
	if err != nil {
		panic(fmt.Sprintf("Unable to start web listener: %s", err))
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		err := webHandler.Run(ctx, l, "")
		if err != nil {
			panic(fmt.Sprintf("Can't start web handler:%s", err))
		}
	}()

	// Give some time for the web goroutine to run since we need the server
	// to be up before starting tests.
	time.Sleep(5 * time.Second)

	baseURL := "http://localhost" + port

	resp, err := http.Get(baseURL + "/-/sidecar/last-update-ts")
	require.NoError(t, err)
	require.Equal(t, http.StatusServiceUnavailable, resp.StatusCode)
	all, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	logger.Log("resp", all)
	cleanupTestResponse(t, resp)

	// Set to ready.
	webHandler.SetReady(true)

	go func() {
		rc := <-webHandler.Reload()
		rc <- nil
	}()

	resp, err = http.Get(baseURL + "/-/sidecar/last-update-ts")
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, "application/json; charset=utf-8", resp.Header.Get("Content-Type"))
	all, err = io.ReadAll(resp.Body)
	require.NoError(t, err)
	logger.Log("resp", all)
	cleanupTestResponse(t, resp)
}

func TestSidecarAPI_reset_config(t *testing.T) {
	t.Parallel()

	dbDir := t.TempDir()

	db, err := tsdb.Open(dbDir, nil, nil, nil, nil)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, db.Close())
	})
	port := fmt.Sprintf(":%d", testutil.RandomUnprivilegedPort(t))

	opts := &Options{
		ListenAddress:   port,
		ReadTimeout:     30 * time.Second,
		MaxConnections:  512,
		Context:         nil,
		Storage:         nil,
		LocalStorage:    &dbAdapter{db},
		TSDBDir:         dbDir,
		QueryEngine:     nil,
		ScrapeManager:   &scrape.Manager{},
		RuleManager:     &rules.Manager{},
		Notifier:        nil,
		RoutePrefix:     "/",
		EnableAdminAPI:  true,
		EnableLifecycle: true,
		ExternalURL: &url.URL{
			Scheme: "http",
			Host:   "localhost" + port,
			Path:   "/",
		},
		Version:  &PrometheusVersion{},
		Gatherer: prometheus.DefaultGatherer,
	}

	opts.Flags = map[string]string{}

	logger := log.NewLogfmtLogger(os.Stdout)
	sidecarSvc := sidecar.New(logger, "../test-data/prometheus.yml")
	webHandler := New(logger, opts, sidecarSvc)

	webHandler.config = &config.Config{}
	webHandler.notifier = &notifier.Manager{}
	l, err := webHandler.Listener()
	if err != nil {
		panic(fmt.Sprintf("Unable to start web listener: %s", err))
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		err := webHandler.Run(ctx, l, "")
		if err != nil {
			panic(fmt.Sprintf("Can't start web handler:%s", err))
		}
	}()

	// Give some time for the web goroutine to run since we need the server
	// to be up before starting tests.
	time.Sleep(5 * time.Second)

	baseURL := "http://localhost" + port
	cmdJson := `{"zone_id": "default"}`

	req, err := http.NewRequest(http.MethodPost, baseURL+"/-/sidecar/reset-config", strings.NewReader(cmdJson))
	require.NoError(t, err)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	require.Equal(t, http.StatusServiceUnavailable, resp.StatusCode)
	cleanupTestResponse(t, resp)

	// Set to ready.
	webHandler.SetReady(true)

	go func() {
		rc := <-webHandler.Reload()
		rc <- nil
	}()

	req, err = http.NewRequest(http.MethodPost, baseURL+"/-/sidecar/reset-config", strings.NewReader(cmdJson))
	require.NoError(t, err)
	resp, err = http.DefaultClient.Do(req)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	cleanupTestResponse(t, resp)
}
