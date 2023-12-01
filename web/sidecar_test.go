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
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/prometheus/discovery"

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

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	webHandler, port := startWebHandler(t, ctx)
	logger := log.NewLogfmtLogger(os.Stdout)

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
	all, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.NoError(t, logger.Log("resp", all))
	require.Equal(t, http.StatusServiceUnavailable, resp.StatusCode)
	require.Equal(t, "application/json; charset=utf-8", resp.Header.Get("Content-Type"))
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
	all, err = io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.NoError(t, logger.Log("resp", all))
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, "application/json; charset=utf-8", resp.Header.Get("Content-Type"))
	cleanupTestResponse(t, resp)
}

func TestSidecarAPI_get_runtimeinfo(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	webHandler, port := startWebHandler(t, ctx)
	logger := log.NewLogfmtLogger(os.Stdout)

	// Give some time for the web goroutine to run since we need the server
	// to be up before starting tests.
	time.Sleep(5 * time.Second)

	baseURL := "http://localhost" + port

	resp, err := http.Get(baseURL + "/-/sidecar/runtimeinfo")
	require.NoError(t, err)
	all, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.NoError(t, logger.Log("resp", all))
	require.Equal(t, http.StatusServiceUnavailable, resp.StatusCode)
	require.Equal(t, "application/json; charset=utf-8", resp.Header.Get("Content-Type"))
	cleanupTestResponse(t, resp)

	// Set to ready.
	webHandler.SetReady(true)

	go func() {
		rc := <-webHandler.Reload()
		rc <- nil
	}()

	resp, err = http.Get(baseURL + "/-/sidecar/runtimeinfo")
	require.NoError(t, err)
	all, err = io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.NoError(t, logger.Log("resp", all))
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, "application/json; charset=utf-8", resp.Header.Get("Content-Type"))
	cleanupTestResponse(t, resp)
}

func TestSidecarAPI_reset_config(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	webHandler, port := startWebHandler(t, ctx)
	logger := log.NewLogfmtLogger(os.Stdout)

	// Give some time for the web goroutine to run since we need the server
	// to be up before starting tests.
	time.Sleep(5 * time.Second)

	baseURL := "http://localhost" + port
	cmdJson := `{"zone_id": "default"}`

	req, err := http.NewRequest(http.MethodPost, baseURL+"/-/sidecar/reset-config", strings.NewReader(cmdJson))
	require.NoError(t, err)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	all, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.NoError(t, logger.Log("resp", all))
	require.Equal(t, http.StatusServiceUnavailable, resp.StatusCode)
	require.Equal(t, "application/json; charset=utf-8", resp.Header.Get("Content-Type"))
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
	all, err = io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.NoError(t, logger.Log("resp", all))
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, "application/json; charset=utf-8", resp.Header.Get("Content-Type"))
	cleanupTestResponse(t, resp)
}

func TestSidecarAPI_test_scrape_config(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	webHandler, port := startWebHandler(t, ctx)
	logger := log.NewLogfmtLogger(os.Stdout)

	// Give some time for the web goroutine to run since we need the server
	// to be up before starting tests.
	time.Sleep(5 * time.Second)

	baseURL := "http://localhost" + port
	cmdJson := `{"yaml":"job_name: test\nstatic_configs:\n  - targets:\n      - localhost:555"}`

	req, err := http.NewRequest(http.MethodPost, baseURL+"/-/sidecar/test/scrape-config", strings.NewReader(cmdJson))
	require.NoError(t, err)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	all, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	logger.Log("resp", all)
	require.Equal(t, http.StatusServiceUnavailable, resp.StatusCode)
	require.Equal(t, "application/json; charset=utf-8", resp.Header.Get("Content-Type"))
	cleanupTestResponse(t, resp)

	// Set to ready.
	webHandler.SetReady(true)

	go func() {
		rc := <-webHandler.Reload()
		rc <- nil
	}()

	req, err = http.NewRequest(http.MethodPost, baseURL+"/-/sidecar/test/scrape-config", strings.NewReader(cmdJson))
	require.NoError(t, err)
	resp, err = http.DefaultClient.Do(req)
	require.NoError(t, err)
	all, err = io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.NoError(t, logger.Log("resp", all))
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, "application/json; charset=utf-8", resp.Header.Get("Content-Type"))
	cleanupTestResponse(t, resp)
}

func TestSidecarAPI_test_scrape_jobs(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	webHandler, port := startWebHandler(t, ctx)
	logger := log.NewLogfmtLogger(os.Stdout)

	// Give some time for the web goroutine to run since we need the server
	// to be up before starting tests.
	time.Sleep(5 * time.Second)

	baseURL := "http://localhost" + port
	cmdJson := `{"job_names":["test"]}`

	req, err := http.NewRequest(http.MethodPost, baseURL+"/-/sidecar/test/scrape-jobs", strings.NewReader(cmdJson))
	require.NoError(t, err)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	all, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.NoError(t, logger.Log("resp", all))
	require.Equal(t, http.StatusServiceUnavailable, resp.StatusCode)
	require.Equal(t, "application/json; charset=utf-8", resp.Header.Get("Content-Type"))
	cleanupTestResponse(t, resp)

	// Set to ready.
	webHandler.SetReady(true)

	go func() {
		rc := <-webHandler.Reload()
		rc <- nil
	}()

	req, err = http.NewRequest(http.MethodPost, baseURL+"/-/sidecar/test/scrape-jobs", strings.NewReader(cmdJson))
	require.NoError(t, err)
	resp, err = http.DefaultClient.Do(req)
	require.NoError(t, err)
	all, err = io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.NoError(t, logger.Log("resp", all))
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, "application/json; charset=utf-8", resp.Header.Get("Content-Type"))
	cleanupTestResponse(t, resp)
}

func TestSidecarAPI_test_remote_write_config(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	webHandler, port := startWebHandler(t, ctx)
	logger := log.NewLogfmtLogger(os.Stdout)

	// Give some time for the web goroutine to run since we need the server
	// to be up before starting tests.
	time.Sleep(5 * time.Second)

	baseURL := "http://localhost" + port
	cmdJson := `{"yaml":"name: test\nurl: http://localhost:555"}`

	req, err := http.NewRequest(http.MethodPost, baseURL+"/-/sidecar/test/remote-write-config", strings.NewReader(cmdJson))
	require.NoError(t, err)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	all, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.NoError(t, logger.Log("resp", all))
	require.Equal(t, http.StatusServiceUnavailable, resp.StatusCode)
	require.Equal(t, "application/json; charset=utf-8", resp.Header.Get("Content-Type"))
	cleanupTestResponse(t, resp)

	// Set to ready.
	webHandler.SetReady(true)

	go func() {
		rc := <-webHandler.Reload()
		rc <- nil
	}()

	req, err = http.NewRequest(http.MethodPost, baseURL+"/-/sidecar/test/remote-write-config", strings.NewReader(cmdJson))
	require.NoError(t, err)
	resp, err = http.DefaultClient.Do(req)
	require.NoError(t, err)
	all, err = io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.NoError(t, logger.Log("resp", all))
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, "application/json; charset=utf-8", resp.Header.Get("Content-Type"))
	cleanupTestResponse(t, resp)
}

func TestSidecarAPI_test_remote_write_remotes(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	webHandler, port := startWebHandler(t, ctx)
	logger := log.NewLogfmtLogger(os.Stdout)

	// Give some time for the web goroutine to run since we need the server
	// to be up before starting tests.
	time.Sleep(5 * time.Second)

	baseURL := "http://localhost" + port
	cmdJson := `{"remote_names":["test"]}`

	req, err := http.NewRequest(http.MethodPost, baseURL+"/-/sidecar/test/remote-write-remotes", strings.NewReader(cmdJson))
	require.NoError(t, err)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	all, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.NoError(t, logger.Log("resp", all))
	require.Equal(t, http.StatusServiceUnavailable, resp.StatusCode)
	require.Equal(t, "application/json; charset=utf-8", resp.Header.Get("Content-Type"))
	cleanupTestResponse(t, resp)

	// Set to ready.
	webHandler.SetReady(true)

	go func() {
		rc := <-webHandler.Reload()
		rc <- nil
	}()

	req, err = http.NewRequest(http.MethodPost, baseURL+"/-/sidecar/test/remote-write-remotes", strings.NewReader(cmdJson))
	require.NoError(t, err)
	resp, err = http.DefaultClient.Do(req)
	require.NoError(t, err)
	all, err = io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.NoError(t, logger.Log("resp", all))
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, "application/json; charset=utf-8", resp.Header.Get("Content-Type"))
	cleanupTestResponse(t, resp)
}

func startWebHandler(t *testing.T, ctxTest context.Context) (*Handler, string) {
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
	scrapeOptions := &scrape.Options{
		NoDefaultPort:             true,
		EnableProtobufNegotiation: true,
	}

	templateConfigYaml, err := os.ReadFile("../test-data/prometheus.yml")
	testConfigDir := t.TempDir()
	testConfigFile := filepath.Join(testConfigDir, "prometheus.yml")
	require.NoError(t, os.WriteFile(testConfigFile, templateConfigYaml, 0o666))

	require.NoError(t, err)
	var (
		rootLogger           = log.NewLogfmtLogger(os.Stdout)
		promCfg, _           = config.Load(string(templateConfigYaml), false, rootLogger)
		discoveryManagerTest *discovery.Manager
	)
	discoveryManagerTest = discovery.NewManager(ctxTest, log.With(rootLogger, "component", "discovery manager test"), discovery.Name("test"))
	go func() {
		discoveryManagerTest.Run()
	}()

	logger := log.NewLogfmtLogger(os.Stdout)
	sidecarSvc := sidecar.New(logger, testConfigFile, discoveryManagerTest, scrapeOptions)

	webHandler := New(logger, opts, sidecarSvc)
	webHandler.notifier = &notifier.Manager{}

	l, err := webHandler.Listener()
	if err != nil {
		panic(fmt.Sprintf("Unable to start web listener: %s", err))
	}
	require.NoError(t, sidecarSvc.ApplyConfig(promCfg))
	require.NoError(t, webHandler.ApplyConfig(promCfg))
	go func() {
		err := webHandler.Run(ctxTest, l, "")
		if err != nil {
			panic(fmt.Sprintf("Can't start web handler:%s", err))
		}
	}()

	return webHandler, port
}
