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

package sidecar

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	p8sconfig "github.com/prometheus/prometheus/config"
	"github.com/prometheus/prometheus/discovery"
	"github.com/prometheus/prometheus/scrape"

	"github.com/go-kit/log"
	"github.com/stretchr/testify/require"
)

func Test_sidecarService_UpdateConfigReload(t *testing.T) {
	testDir := t.TempDir()
	fmt.Println("test dir:", testDir)

	configFile := filepath.Join(testDir, "prometheus.yml")
	templateConfigYaml, err := os.ReadFile("../test-data/prometheus.yml")
	require.NoError(t, err)

	// 预先准备一些文件
	require.NoError(t, os.WriteFile(configFile, templateConfigYaml, 0o666))
	require.NoError(t, os.MkdirAll(filepath.Join(testDir, "secrets"), 0o777))
	require.NoError(t, os.MkdirAll(filepath.Join(testDir, "rules"), 0o777))
	require.NoError(t, os.WriteFile(filepath.Join(testDir, "secrets", "secret-a"), []byte("secret-a"), 0o666))
	require.NoError(t, os.WriteFile(filepath.Join(testDir, "secrets", "secret-b"), []byte("secret-b"), 0o666))
	require.NoError(t, os.WriteFile(filepath.Join(testDir, "rules", "rules-a"), []byte("rules-a"), 0o666))
	require.NoError(t, os.WriteFile(filepath.Join(testDir, "rules", "rules-b"), []byte("rules-b"), 0o666))

	s := &sidecarService{
		logger:     log.NewLogfmtLogger(os.Stdout),
		configFile: configFile,
	}

	rt := s.GetRuntimeInfo()
	require.Equal(t, "", rt.ZoneId)
	require.Equal(t, time.Time{}, rt.LastUpdateTs)

	cmd := &UpdateConfigCmd{
		ZoneId: "default",
		Yaml: `global:
  scrape_interval: 15s
  scrape_timeout: 10s
  evaluation_interval: 15s
alerting:
  alertmanagers:
  - follow_redirects: true
    enable_http2: true
    scheme: http
    timeout: 10s
    api_version: v2
    static_configs:
    - targets: []
scrape_configs:
- job_name: prometheus
  honor_timestamps: true
  scrape_interval: 15s
  scrape_timeout: 10s
  metrics_path: /metrics
  scheme: http
  follow_redirects: true
  enable_http2: true
  static_configs:
  - targets:
    - localhost:9090`,
		RuleFiles: []ruleFileInner{
			{FileName: "foo", Yaml: "groups: []"},
			{FileName: "bar", Yaml: "groups: []"},
		},
		SecretFiles: []secretFileInner{
			{FileName: "foo", Secret: "bar"},
			{FileName: "bar", Secret: "blah blah"},
		},
	}

	reloadCh := make(chan chan error)
	go func() {
		ch := <-reloadCh
		ch <- nil
	}()
	err = s.UpdateConfigReload(context.TODO(), cmd, reloadCh)
	require.NoError(t, err)

	// 绑定的 zoneId 变更了，更新时间戳也变了
	rt = s.GetRuntimeInfo()
	require.Equal(t, cmd.ZoneId, rt.ZoneId)
	require.NotEqual(t, time.Time{}, rt.LastUpdateTs)

	// 配置文件写入了
	configFileB, err := os.ReadFile(configFile)
	require.NoError(t, err)
	require.NotEqual(t, templateConfigYaml, configFileB)

	// 其他文件也写入了，旧的相关文件被删除了
	require.FileExists(t, filepath.Join(testDir, "secrets", "secret-foo"))
	require.FileExists(t, filepath.Join(testDir, "secrets", "secret-bar"))
	require.FileExists(t, filepath.Join(testDir, "rules", "rules-foo"))
	require.FileExists(t, filepath.Join(testDir, "rules", "rules-bar"))

	require.NoFileExists(t, filepath.Join(testDir, "secrets", "secret-a"))
	require.NoFileExists(t, filepath.Join(testDir, "secrets", "secret-b"))
	require.NoFileExists(t, filepath.Join(testDir, "rules", "rules-a"))
	require.NoFileExists(t, filepath.Join(testDir, "rules", "rules-b"))
}

func Test_sidecarService_UpdateConfigReload_ZoneIdMismatch(t *testing.T) {
	testDir := t.TempDir()
	fmt.Println("test dir:", testDir)

	configFile := filepath.Join(testDir, "prometheus.yml")
	templateConfigYaml, err := os.ReadFile("../test-data/prometheus.yml")
	require.NoError(t, err)

	// 预先准备一些文件
	require.NoError(t, os.WriteFile(configFile, templateConfigYaml, 0o666))
	require.NoError(t, os.MkdirAll(filepath.Join(testDir, "secrets"), 0o777))
	require.NoError(t, os.MkdirAll(filepath.Join(testDir, "rules"), 0o777))

	s := &sidecarService{
		logger:     log.NewLogfmtLogger(os.Stdout),
		configFile: configFile,
	}

	{
		// 先做一次更新
		cmd := &UpdateConfigCmd{
			ZoneId: "default",
			Yaml:   "global:\n  scrape_interval: 15s\n  scrape_timeout: 10s\n  evaluation_interval: 15s\nalerting:\n  alertmanagers:\n  - follow_redirects: true\n    enable_http2: true\n    scheme: http\n    timeout: 10s\n    api_version: v2\n    static_configs:\n    - targets: []\nscrape_configs:\n- job_name: prometheus\n  honor_timestamps: true\n  scrape_interval: 15s\n  scrape_timeout: 10s\n  metrics_path: /metrics\n  scheme: http\n  follow_redirects: true\n  enable_http2: true\n  static_configs:\n  - targets:\n    - localhost:9090\n",
			RuleFiles: []ruleFileInner{
				{FileName: "foo", Yaml: "groups: []"},
				{FileName: "bar", Yaml: "groups: []"},
			},
			SecretFiles: []secretFileInner{
				{FileName: "foo", Secret: "bar"},
				{FileName: "bar", Secret: "blah blah"},
			},
		}

		reloadCh := make(chan chan error)
		go func() {
			ch := <-reloadCh
			ch <- nil
		}()
		err = s.UpdateConfigReload(context.TODO(), cmd, reloadCh)
		require.NoError(t, err)
		rt := s.GetRuntimeInfo()
		require.Equal(t, "default", rt.ZoneId)
		require.NotEqual(t, time.Time{}, rt.LastUpdateTs)
	}

	{
		lastRt := s.GetRuntimeInfo()
		lastConfigFileB, err := os.ReadFile(configFile)
		require.NoError(t, err)

		// 下达一个 zoneId 不匹配的指令
		cmd2 := &UpdateConfigCmd{
			ZoneId: "default2",
			Yaml:   "global:\n  scrape_interval: 15s\n  scrape_timeout: 10s\n  evaluation_interval: 15s\nalerting:\n  alertmanagers:\n  - follow_redirects: true\n    enable_http2: true\n    scheme: http\n    timeout: 10s\n    api_version: v2\n    static_configs:\n    - targets: []\nscrape_configs:\n- job_name: prometheus\n  honor_timestamps: true\n  scrape_interval: 15s\n  scrape_timeout: 10s\n  metrics_path: /metrics\n  scheme: http\n  follow_redirects: true\n  enable_http2: true\n  static_configs:\n  - targets:\n    - localhost:9090\n",
			RuleFiles: []ruleFileInner{
				{FileName: "foo2", Yaml: "groups: []"},
				{FileName: "bar2", Yaml: "groups: []"},
			},
			SecretFiles: []secretFileInner{
				{FileName: "foo2", Secret: "bar"},
				{FileName: "bar2", Secret: "blah blah"},
			},
		}

		reloadCh := make(chan chan error)
		go func() {
			ch := <-reloadCh
			ch <- nil
		}()
		err = s.UpdateConfigReload(context.TODO(), cmd2, reloadCh)
		require.Error(t, err)

		thisRt := s.GetRuntimeInfo()
		// 配置绑定 zoneId 没有变更，时间戳也没变
		require.Equal(t, lastRt, thisRt)
		// 配置文件也没有变更
		thisConfigFileB, err := os.ReadFile(configFile)
		require.NoError(t, err)
		require.Equal(t, lastConfigFileB, thisConfigFileB)

		require.FileExists(t, filepath.Join(testDir, "secrets", "secret-foo"))
		require.FileExists(t, filepath.Join(testDir, "secrets", "secret-bar"))
		require.FileExists(t, filepath.Join(testDir, "rules", "rules-foo"))
		require.FileExists(t, filepath.Join(testDir, "rules", "rules-bar"))

		require.NoFileExists(t, filepath.Join(testDir, "secrets", "secret-foo2"))
		require.NoFileExists(t, filepath.Join(testDir, "secrets", "secret-bar2"))
		require.NoFileExists(t, filepath.Join(testDir, "rules", "rules-foo2"))
		require.NoFileExists(t, filepath.Join(testDir, "rules", "rules-bar2"))
	}
}

func Test_sidecarService_UpdateConfigReload_ErrorRestore(t *testing.T) {
	testDir := t.TempDir()
	fmt.Println("test dir:", testDir)

	configFile := filepath.Join(testDir, "prometheus.yml")
	templateConfigYaml, err := os.ReadFile("../test-data/prometheus.yml")
	require.NoError(t, err)

	// 预先准备一些文件
	require.NoError(t, os.WriteFile(configFile, templateConfigYaml, 0o666))
	require.NoError(t, os.MkdirAll(filepath.Join(testDir, "secrets"), 0o777))
	require.NoError(t, os.MkdirAll(filepath.Join(testDir, "rules"), 0o777))
	require.NoError(t, os.WriteFile(filepath.Join(testDir, "secrets", "secret-a"), []byte("secret-a"), 0o666))
	require.NoError(t, os.WriteFile(filepath.Join(testDir, "secrets", "secret-b"), []byte("secret-b"), 0o666))
	require.NoError(t, os.WriteFile(filepath.Join(testDir, "rules", "rules-a"), []byte("rules-a"), 0o666))
	require.NoError(t, os.WriteFile(filepath.Join(testDir, "rules", "rules-b"), []byte("rules-b"), 0o666))

	s := &sidecarService{
		logger:     log.NewLogfmtLogger(os.Stdout),
		configFile: configFile,
	}

	rt := s.GetRuntimeInfo()
	require.Equal(t, "", rt.ZoneId)
	require.Equal(t, time.Time{}, rt.LastUpdateTs)

	cmd := &UpdateConfigCmd{
		ZoneId: "default",
		Yaml:   "global:\n  scrape_interval: 15s\n  scrape_timeout: 10s\n  evaluation_interval: 15s\nalerting:\n  alertmanagers:\n  - follow_redirects: true\n    enable_http2: true\n    scheme: http\n    timeout: 10s\n    api_version: v2\n    static_configs:\n    - targets: []\nscrape_configs:\n- job_name: prometheus\n  honor_timestamps: true\n  scrape_interval: 15s\n  scrape_timeout: 10s\n  metrics_path: /metrics\n  scheme: http\n  follow_redirects: true\n  enable_http2: true\n  static_configs:\n  - targets:\n    - localhost:9090\n",
		RuleFiles: []ruleFileInner{
			{FileName: "foo", Yaml: "groups: []"},
			{FileName: "bar", Yaml: "groups: []"},
		},
		SecretFiles: []secretFileInner{
			{FileName: "foo", Secret: "bar"},
			{FileName: "bar", Secret: "blah blah"},
		},
	}

	reloadCh := make(chan chan error)
	go func() {
		ch := <-reloadCh
		ch <- errors.New("on purpose")
	}()
	err = s.UpdateConfigReload(context.TODO(), cmd, reloadCh)
	require.Error(t, err)

	// 绑定的 zoneId 依然是空，时间戳也是空
	rt = s.GetRuntimeInfo()
	require.Equal(t, "", rt.ZoneId)
	require.Equal(t, time.Time{}, rt.LastUpdateTs)

	// 配置文件没有变更
	configFileYaml, err := os.ReadFile(configFile)
	require.NoError(t, err)
	require.Equal(t, templateConfigYaml, configFileYaml)

	// 原来的文件都在，新的文件没有写入
	require.NoFileExists(t, filepath.Join(testDir, "secrets", "secret-foo"))
	require.NoFileExists(t, filepath.Join(testDir, "secrets", "secret-bar"))
	require.NoFileExists(t, filepath.Join(testDir, "rules", "rules-foo"))
	require.NoFileExists(t, filepath.Join(testDir, "rules", "rules-bar"))

	require.FileExists(t, filepath.Join(testDir, "secrets", "secret-a"))
	require.FileExists(t, filepath.Join(testDir, "secrets", "secret-b"))
	require.FileExists(t, filepath.Join(testDir, "rules", "rules-a"))
	require.FileExists(t, filepath.Join(testDir, "rules", "rules-b"))
}

func Test_sidecarService_ResetConfigReload(t *testing.T) {
	testDir := t.TempDir()
	fmt.Println("test dir:", testDir)

	configFile := filepath.Join(testDir, "prometheus.yml")
	templateConfigYaml, err := os.ReadFile("../test-data/prometheus.yml")
	require.NoError(t, err)

	require.NoError(t, os.WriteFile(configFile, templateConfigYaml, 0o666))

	s := &sidecarService{
		logger:     log.NewLogfmtLogger(os.Stdout),
		configFile: configFile,
	}

	t.Run("reset origin empty", func(t *testing.T) {
		reloadCh := make(chan chan error)
		go func() {
			ch := <-reloadCh
			ch <- nil
		}()

		err = s.ResetConfigReload(context.TODO(), "default", reloadCh)
		require.NoError(t, err)

		rt := s.GetRuntimeInfo()
		require.Equal(t, "prometheus-mod", rt.Brand)
		require.Equal(t, "", rt.ZoneId)
		require.Equal(t, time.Time{}, rt.LastUpdateTs)
	})

	t.Run("reset configured", func(t *testing.T) {
		cmd := &UpdateConfigCmd{
			ZoneId: "default",
			Yaml:   "global:\n  scrape_interval: 15s\n  scrape_timeout: 10s\n  evaluation_interval: 15s\nalerting:\n  alertmanagers:\n  - follow_redirects: true\n    enable_http2: true\n    scheme: http\n    timeout: 10s\n    api_version: v2\n    static_configs:\n    - targets: []\nscrape_configs:\n- job_name: prometheus\n  honor_timestamps: true\n  scrape_interval: 15s\n  scrape_timeout: 10s\n  metrics_path: /metrics\n  scheme: http\n  follow_redirects: true\n  enable_http2: true\n  static_configs:\n  - targets:\n    - localhost:9090\n",
			RuleFiles: []ruleFileInner{
				{FileName: "foo", Yaml: "groups: []"},
				{FileName: "bar", Yaml: "groups: []"},
			},
			SecretFiles: []secretFileInner{
				{FileName: "foo", Secret: "bar"},
				{FileName: "bar", Secret: "blah blah"},
			},
		}

		{
			reloadCh := make(chan chan error)
			go func() {
				ch := <-reloadCh
				ch <- nil
			}()
			require.NoError(t, s.UpdateConfigReload(context.TODO(), cmd, reloadCh))
		}

		reloadCh := make(chan chan error)
		go func() {
			ch := <-reloadCh
			ch <- nil
		}()
		err = s.ResetConfigReload(context.TODO(), "default", reloadCh)
		require.NoError(t, err)

		thisRt := s.GetRuntimeInfo()
		require.Equal(t, "prometheus-mod", thisRt.Brand)
		require.Equal(t, "", thisRt.ZoneId)
		require.Equal(t, time.Time{}, thisRt.LastUpdateTs)

		// 文件都消失了
		require.NoFileExists(t, filepath.Join(testDir, "secrets", "secret-foo"))
		require.NoFileExists(t, filepath.Join(testDir, "secrets", "secret-bar"))
		require.NoFileExists(t, filepath.Join(testDir, "rules", "rules-foo"))
		require.NoFileExists(t, filepath.Join(testDir, "rules", "rules-bar"))

		config := s.getConfigCopy()
		require.Empty(t, config.ScrapeConfigs)
		require.Empty(t, config.ScrapeConfigFiles)
		require.Empty(t, config.RuleFiles)
	})
}

func Test_sidecarService_TestScrapeConfig(t *testing.T) {
	testDir := t.TempDir()
	fmt.Println("test dir:", testDir)

	configFile := filepath.Join(testDir, "prometheus.yml")
	templateConfigYaml, err := os.ReadFile("../test-data/prometheus.yml")
	require.NoError(t, err)

	require.NoError(t, os.WriteFile(configFile, templateConfigYaml, 0o666))

	var (
		rootLogger           = log.NewLogfmtLogger(os.Stdout)
		config, _            = p8sconfig.Load(string(templateConfigYaml), false, rootLogger)
		ctxTest, cancelTest  = context.WithCancel(context.Background())
		discoveryManagerTest *discovery.Manager
	)
	defer cancelTest()
	discoveryManagerTest = discovery.NewManager(ctxTest, log.With(rootLogger, "component", "discovery manager test"), discovery.Name("test"))
	go func() {
		discoveryManagerTest.Run()
	}()

	s := &sidecarService{
		logger: log.With(rootLogger, "component", "sidecar"),
		scrapeOptions: &scrape.Options{
			NoDefaultPort:             true,
			EnableProtobufNegotiation: true,
		},
		discoveryManager: discoveryManagerTest,
		configFile:       configFile,
	}
	require.NoError(t, s.ApplyConfig(config))

	t.Run("good", func(t *testing.T) {
		server := mockMetricServer(false, "", "")
		defer server.Close()
		serverURL, err := url.Parse(server.URL)
		require.NoError(t, err)

		cmd := &TestScrapeConfigCmd{
			Yaml: fmt.Sprintf(`job_name: test
static_configs:
  - targets: 
      - %s`, serverURL.Host),
			SecretFiles: nil,
		}
		result, err := s.TestScrapeConfig(cmd)
		require.NoError(t, err)
		require.Equal(t, "test", result.JobName)
		require.Equal(t, "success", result.Message)
		require.Equal(t, true, result.Success)
		require.Nil(t, result.Error)
		require.Equal(t, true, result.TargetResults[0].Success)
		require.Nil(t, result.TargetResults[0].Error)
	})

	t.Run("broken target", func(t *testing.T) {
		server := mockMetricServer(true, "", "")
		defer server.Close()
		serverURL, err := url.Parse(server.URL)
		require.NoError(t, err)

		cmd := &TestScrapeConfigCmd{
			Yaml: fmt.Sprintf(`job_name: test
static_configs:
  - targets: 
      - %s`, serverURL.Host),
			SecretFiles: nil,
		}
		result, err := s.TestScrapeConfig(cmd)
		require.NoError(t, err)
		require.Equal(t, "test", result.JobName)
		require.Equal(t, "some target(s) failed", result.Message)
		require.Equal(t, false, result.Success)
		require.Nil(t, result.Error)
		require.Equal(t, false, result.TargetResults[0].Success)
		require.NotNil(t, result.TargetResults[0].Error)
	})

	t.Run("unreachable target", func(t *testing.T) {
		cmd := &TestScrapeConfigCmd{
			Yaml: `job_name: test
static_configs:
  - targets: 
      - localhost:555`,
			SecretFiles: nil,
		}
		result, err := s.TestScrapeConfig(cmd)
		require.NoError(t, err)
		require.Equal(t, "test", result.JobName)
		require.Equal(t, "some target(s) failed", result.Message)
		require.Equal(t, false, result.Success)
		require.Nil(t, result.Error)
		require.Equal(t, false, result.TargetResults[0].Success)
		require.NotNil(t, result.TargetResults[0].Error)
	})

	t.Run("good password", func(t *testing.T) {
		server := mockMetricServer(false, "foo", "bar")
		defer server.Close()
		serverURL, err := url.Parse(server.URL)
		require.NoError(t, err)

		cmd := &TestScrapeConfigCmd{
			Yaml: fmt.Sprintf(`job_name: test
basic_auth:
  username: foo
  password: bar
static_configs:
  - targets: 
      - %s`, serverURL.Host),
			SecretFiles: nil,
		}
		result, err := s.TestScrapeConfig(cmd)
		require.NoError(t, err)
		require.Equal(t, "test", result.JobName)
		require.Equal(t, "success", result.Message)
		require.Equal(t, true, result.Success)
		require.Nil(t, result.Error)
		require.Equal(t, true, result.TargetResults[0].Success)
		require.Nil(t, result.TargetResults[0].Error)
	})

	t.Run("good password file", func(t *testing.T) {
		server := mockMetricServer(false, "foo", "bar")
		defer server.Close()
		serverURL, err := url.Parse(server.URL)
		require.NoError(t, err)

		cmd := &TestScrapeConfigCmd{
			Yaml: fmt.Sprintf(`job_name: test
basic_auth:
  username: foo
  password_file: secret-foo
static_configs:
  - targets: 
      - %s`, serverURL.Host),
			SecretFiles: []secretFileInner{
				{FileName: "secret-foo", Secret: "bar"},
			},
		}
		result, err := s.TestScrapeConfig(cmd)
		require.NoError(t, err)
		require.Equal(t, "test", result.JobName)
		require.Equal(t, "success", result.Message)
		require.Equal(t, true, result.Success)
		require.Nil(t, result.Error)
		require.Equal(t, true, result.TargetResults[0].Success)
		require.Nil(t, result.TargetResults[0].Error)
	})
}

func Test_sidecarService_TestScrapeJobs(t *testing.T) {
	var (
		rootLogger           = log.NewLogfmtLogger(os.Stdout)
		ctxTest, cancelTest  = context.WithCancel(context.Background())
		discoveryManagerTest *discovery.Manager
	)
	defer cancelTest()
	discoveryManagerTest = discovery.NewManager(ctxTest, log.With(rootLogger, "component", "discovery manager test"), discovery.Name("test"))
	go func() {
		discoveryManagerTest.Run()
	}()

	s := &sidecarService{
		logger: log.With(rootLogger, "component", "sidecar"),
		scrapeOptions: &scrape.Options{
			NoDefaultPort:             true,
			EnableProtobufNegotiation: true,
		},
		discoveryManager: discoveryManagerTest,
	}

	t.Run("good", func(t *testing.T) {
		server := mockMetricServer(false, "", "")
		defer server.Close()
		serverURL, err := url.Parse(server.URL)
		require.NoError(t, err)

		configYaml := fmt.Sprintf(`
global:
  scrape_interval: 15s
  evaluation_interval: 15s
scrape_configs:
  - job_name: "test"
    static_configs:
      - targets: ["%s"]
`, serverURL.Host)
		config, err := p8sconfig.Load(configYaml, false, rootLogger)
		require.NoError(t, err)
		require.NoError(t, s.ApplyConfig(config))

		results, err := s.TestScrapeJobs([]string{"test"})
		require.NoError(t, err)
		require.Len(t, results, 1)
		result := results[0]
		require.Equal(t, "test", result.JobName)
		require.Equal(t, "success", result.Message)
		require.Equal(t, true, result.Success)
		require.Nil(t, result.Error)
		require.Equal(t, true, result.TargetResults[0].Success)
		require.Nil(t, result.TargetResults[0].Error)
	})

	t.Run("broken target", func(t *testing.T) {
		server := mockMetricServer(true, "", "")
		defer server.Close()
		serverURL, err := url.Parse(server.URL)
		require.NoError(t, err)

		configYaml := fmt.Sprintf(`
global:
  scrape_interval: 15s
  evaluation_interval: 15s
scrape_configs:
  - job_name: "test"
    static_configs:
      - targets: ["%s"]
`, serverURL.Host)
		config, err := p8sconfig.Load(configYaml, false, rootLogger)
		require.NoError(t, err)
		require.NoError(t, s.ApplyConfig(config))

		results, err := s.TestScrapeJobs([]string{"test"})
		require.NoError(t, err)
		require.Len(t, results, 1)
		result := results[0]
		require.Equal(t, "test", result.JobName)
		require.Equal(t, "some target(s) failed", result.Message)
		require.Equal(t, false, result.Success)
		require.Nil(t, result.Error)
		require.Equal(t, false, result.TargetResults[0].Success)
		require.NotNil(t, result.TargetResults[0].Error)
	})

	t.Run("unreachable target", func(t *testing.T) {
		configYaml := fmt.Sprintf(`
global:
  scrape_interval: 15s
  evaluation_interval: 15s
scrape_configs:
  - job_name: "test"
    static_configs:
      - targets: ["%s"]
`, "localhost:555")
		config, err := p8sconfig.Load(configYaml, false, rootLogger)
		require.NoError(t, err)
		require.NoError(t, s.ApplyConfig(config))

		results, err := s.TestScrapeJobs([]string{"test"})
		require.NoError(t, err)
		require.Len(t, results, 1)
		result := results[0]
		require.Equal(t, "test", result.JobName)
		require.Equal(t, "some target(s) failed", result.Message)
		require.Equal(t, false, result.Success)
		require.Nil(t, result.Error)
		require.Equal(t, false, result.TargetResults[0].Success)
		require.NotNil(t, result.TargetResults[0].Error)
	})

	t.Run("good password", func(t *testing.T) {
		server := mockMetricServer(false, "foo", "bar")
		defer server.Close()
		serverURL, err := url.Parse(server.URL)
		require.NoError(t, err)

		configYaml := fmt.Sprintf(`
global:
  scrape_interval: 15s
  evaluation_interval: 15s
scrape_configs:
  - job_name: "test"
    basic_auth:
      username: foo
      password: bar
    static_configs:
      - targets: ["%s"]
`, serverURL.Host)
		config, err := p8sconfig.Load(configYaml, false, rootLogger)
		require.NoError(t, err)
		require.NoError(t, s.ApplyConfig(config))

		results, err := s.TestScrapeJobs([]string{"test"})
		require.NoError(t, err)
		require.Len(t, results, 1)
		result := results[0]
		require.Equal(t, "test", result.JobName)
		require.Equal(t, "success", result.Message)
		require.Equal(t, true, result.Success)
		require.Nil(t, result.Error)
		require.Equal(t, true, result.TargetResults[0].Success)
		require.Nil(t, result.TargetResults[0].Error)
	})

	t.Run("good password file", func(t *testing.T) {
		testDir := t.TempDir()
		fmt.Println("test dir:", testDir)
		require.NoError(t, os.WriteFile(filepath.Join(testDir, "secret-foo"), []byte("bar"), 0o666))

		server := mockMetricServer(false, "foo", "bar")
		defer server.Close()
		serverURL, err := url.Parse(server.URL)
		require.NoError(t, err)

		configYaml := fmt.Sprintf(`
global:
  scrape_interval: 15s
  evaluation_interval: 15s
scrape_configs:
  - job_name: "test"
    basic_auth:
      username: foo
      password_file: secret-foo
    static_configs:
      - targets: ["%s"]
`, serverURL.Host)

		config, err := p8sconfig.Load(configYaml, false, rootLogger)
		require.NoError(t, err)
		config.SetDirectory(testDir)
		require.NoError(t, s.ApplyConfig(config))

		results, err := s.TestScrapeJobs([]string{"test"})
		require.NoError(t, err)
		require.Len(t, results, 1)
		result := results[0]
		require.Equal(t, "test", result.JobName)
		require.Equal(t, "success", result.Message)
		require.Equal(t, true, result.Success)
		require.Nil(t, result.Error)
		require.Equal(t, true, result.TargetResults[0].Success)
		require.Nil(t, result.TargetResults[0].Error)
	})

	t.Run("not exist job", func(t *testing.T) {
		server := mockMetricServer(true, "", "")
		defer server.Close()
		serverURL, err := url.Parse(server.URL)
		require.NoError(t, err)

		configYaml := fmt.Sprintf(`
global:
  scrape_interval: 15s
  evaluation_interval: 15s
scrape_configs:
  - job_name: "test"
    static_configs:
      - targets: ["%s"]
`, serverURL.Host)
		config, err := p8sconfig.Load(configYaml, false, rootLogger)
		require.NoError(t, err)
		require.NoError(t, s.ApplyConfig(config))

		results, err := s.TestScrapeJobs([]string{"test2"})
		require.NoError(t, err)
		require.Len(t, results, 1)
		result := results[0]
		require.Equal(t, "test2", result.JobName)
		require.Equal(t, `scrape config not found: [job=test2]`, result.Message)
		require.Equal(t, false, result.Success)
		require.Nil(t, result.Error)
		require.Len(t, result.TargetResults, 0)
	})
}

func Test_sidecarService_TestRemoteWriteConfig(t *testing.T) {
	testDir := t.TempDir()
	fmt.Println("test dir:", testDir)

	configFile := filepath.Join(testDir, "prometheus.yml")
	templateConfigYaml, err := os.ReadFile("../test-data/prometheus.yml")
	require.NoError(t, err)

	require.NoError(t, os.WriteFile(configFile, templateConfigYaml, 0o666))

	var (
		rootLogger           = log.NewLogfmtLogger(os.Stdout)
		config, _            = p8sconfig.Load(string(templateConfigYaml), false, rootLogger)
		ctxTest, cancelTest  = context.WithCancel(context.Background())
		discoveryManagerTest *discovery.Manager
	)
	defer cancelTest()
	discoveryManagerTest = discovery.NewManager(ctxTest, log.With(rootLogger, "component", "discovery manager test"), discovery.Name("test"))
	go func() {
		discoveryManagerTest.Run()
	}()

	s := &sidecarService{
		logger: log.With(rootLogger, "component", "sidecar"),
		scrapeOptions: &scrape.Options{
			NoDefaultPort:             true,
			EnableProtobufNegotiation: true,
		},
		discoveryManager: discoveryManagerTest,
		configFile:       configFile,
	}
	require.NoError(t, s.ApplyConfig(config))

	t.Run("good", func(t *testing.T) {
		server := mockRemoteServer(false, "", "")
		defer server.Close()
		cmd := &TestRemoteWriteConfigCmd{
			Yaml: fmt.Sprintf(`name: test
url: %s`, server.URL),
			SecretFiles: nil,
		}
		result, err := s.TestRemoteWriteConfig(cmd)
		require.NoError(t, err)
		require.Equal(t, "test", result.RemoteName)
		require.Equal(t, true, result.Success)
		require.Equal(t, "success", result.Message)
		require.Nil(t, result.Error)
	})

	t.Run("broken remote", func(t *testing.T) {
		server := mockRemoteServer(true, "", "")
		defer server.Close()
		cmd := &TestRemoteWriteConfigCmd{
			Yaml: fmt.Sprintf(`name: test
url: %s`, server.URL),
			SecretFiles: nil,
		}
		result, err := s.TestRemoteWriteConfig(cmd)
		require.NoError(t, err)
		require.Equal(t, "test", result.RemoteName)
		require.Equal(t, false, result.Success)
		require.Equal(t, "write to remote failed", result.Message)
		require.NotNil(t, result.Error)
	})

	t.Run("unreachable target", func(t *testing.T) {
		cmd := &TestRemoteWriteConfigCmd{
			Yaml: `name: test
url: http://localhost:555`,
			SecretFiles: nil,
		}
		result, err := s.TestRemoteWriteConfig(cmd)
		require.NoError(t, err)
		require.Equal(t, "test", result.RemoteName)
		require.Equal(t, false, result.Success)
		require.Equal(t, "write to remote failed", result.Message)
		require.NotNil(t, result.Error)
	})

	t.Run("good password", func(t *testing.T) {
		server := mockRemoteServer(false, "foo", "bar")
		defer server.Close()

		cmd := &TestRemoteWriteConfigCmd{
			Yaml: fmt.Sprintf(`name: test
basic_auth:
  username: foo
  password: bar
url: %s`, server.URL),
			SecretFiles: nil,
		}
		result, err := s.TestRemoteWriteConfig(cmd)
		require.NoError(t, err)
		require.Equal(t, "test", result.RemoteName)
		require.Equal(t, true, result.Success)
		require.Equal(t, "success", result.Message)
		require.Nil(t, result.Error)
	})

	t.Run("good password file", func(t *testing.T) {
		server := mockRemoteServer(false, "foo", "bar")
		defer server.Close()

		cmd := &TestRemoteWriteConfigCmd{
			Yaml: fmt.Sprintf(`name: test
basic_auth:
  username: foo
  password_file: secret-foo
url: %s`, server.URL),
			SecretFiles: []secretFileInner{
				{FileName: "secret-foo", Secret: "bar"},
			},
		}
		result, err := s.TestRemoteWriteConfig(cmd)
		require.NoError(t, err)
		require.Equal(t, "test", result.RemoteName)
		require.Equal(t, true, result.Success)
		require.Equal(t, "success", result.Message)
		require.Nil(t, result.Error)
	})
}

func mockMetricServer(serverError bool, expectedUser, expectedPass string) *httptest.Server {
	return httptest.NewServer(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if serverError {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			if expectedUser != "" {
				username, password, ok := r.BasicAuth()
				if !ok {
					w.WriteHeader(http.StatusBadRequest)
					w.Write([]byte("Bad Authorization header"))
					return
				}
				if username != expectedUser || password != expectedPass {
					w.WriteHeader(http.StatusUnauthorized)
					w.Write([]byte("Authentication failed"))
					return
				}
			}
			w.Header().Set("Content-Type", `text/plain; version=0.0.4`)
			w.Write([]byte("metric_a 1\nmetric_b 2\n"))
		}),
	)
}

func mockRemoteServer(serverError bool, expectedUser, expectedPass string) *httptest.Server {
	return httptest.NewServer(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if serverError {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			if expectedUser != "" {
				username, password, ok := r.BasicAuth()
				if !ok {
					w.WriteHeader(http.StatusBadRequest)
					w.Write([]byte("Bad Authorization header"))
					return
				}
				if username != expectedUser || password != expectedPass {
					w.WriteHeader(http.StatusUnauthorized)
					w.Write([]byte("Authentication failed"))
					return
				}
			}
			w.WriteHeader(http.StatusOK)
		}),
	)
}

func Test_sidecarService_TestRemoteWriteRemotes(t *testing.T) {
	var (
		rootLogger           = log.NewLogfmtLogger(os.Stdout)
		ctxTest, cancelTest  = context.WithCancel(context.Background())
		discoveryManagerTest *discovery.Manager
	)
	defer cancelTest()
	discoveryManagerTest = discovery.NewManager(ctxTest, log.With(rootLogger, "component", "discovery manager test"), discovery.Name("test"))
	go func() {
		discoveryManagerTest.Run()
	}()

	s := &sidecarService{
		logger: log.With(rootLogger, "component", "sidecar"),
		scrapeOptions: &scrape.Options{
			NoDefaultPort:             true,
			EnableProtobufNegotiation: true,
		},
		discoveryManager: discoveryManagerTest,
	}

	t.Run("good", func(t *testing.T) {
		server := mockRemoteServer(false, "", "")
		defer server.Close()

		configYaml := fmt.Sprintf(`
global:
  scrape_interval: 15s
  evaluation_interval: 15s
remote_write:
  - name: "test"
    url: "%s"
`, server.URL)
		config, err := p8sconfig.Load(configYaml, false, rootLogger)
		require.NoError(t, err)
		require.NoError(t, s.ApplyConfig(config))

		results, err := s.TestRemoteWriteRemotes([]string{"test"})
		require.NoError(t, err)
		require.Len(t, results, 1)
		result := results[0]
		require.Equal(t, "test", result.RemoteName)
		require.Equal(t, "success", result.Message)
		require.Equal(t, true, result.Success)
		require.Nil(t, result.Error)
	})

	t.Run("broken target", func(t *testing.T) {
		server := mockRemoteServer(true, "", "")
		defer server.Close()

		configYaml := fmt.Sprintf(`
global:
  scrape_interval: 15s
  evaluation_interval: 15s
remote_write:
  - name: "test"
    url: "%s"
`, server.URL)
		config, err := p8sconfig.Load(configYaml, false, rootLogger)
		require.NoError(t, err)
		require.NoError(t, s.ApplyConfig(config))

		results, err := s.TestRemoteWriteRemotes([]string{"test"})
		require.NoError(t, err)
		require.Len(t, results, 1)
		result := results[0]
		require.Equal(t, "test", result.RemoteName)
		require.Equal(t, "write to remote failed", result.Message)
		require.Equal(t, false, result.Success)
		require.NotNil(t, result.Error)
	})

	t.Run("unreachable target", func(t *testing.T) {
		configYaml := fmt.Sprintf(`
global:
  scrape_interval: 15s
  evaluation_interval: 15s
remote_write:
  - name: "test"
    url: "%s"
`, "http://localhost:555")
		config, err := p8sconfig.Load(configYaml, false, rootLogger)
		require.NoError(t, err)
		require.NoError(t, s.ApplyConfig(config))

		results, err := s.TestRemoteWriteRemotes([]string{"test"})
		require.NoError(t, err)
		require.Len(t, results, 1)
		result := results[0]
		require.Equal(t, "test", result.RemoteName)
		require.Equal(t, "write to remote failed", result.Message)
		require.Equal(t, false, result.Success)
		require.NotNil(t, result.Error)
	})

	t.Run("good password", func(t *testing.T) {
		server := mockRemoteServer(false, "foo", "bar")
		defer server.Close()

		configYaml := fmt.Sprintf(`
global:
  scrape_interval: 15s
  evaluation_interval: 15s
remote_write:
  - name: "test"
    url: "%s"
    basic_auth:
      username: foo
      password: bar
`, server.URL)
		config, err := p8sconfig.Load(configYaml, false, rootLogger)
		require.NoError(t, err)
		require.NoError(t, s.ApplyConfig(config))

		results, err := s.TestRemoteWriteRemotes([]string{"test"})
		require.NoError(t, err)
		require.Len(t, results, 1)
		result := results[0]
		require.Equal(t, "test", result.RemoteName)
		require.Equal(t, "success", result.Message)
		require.Equal(t, true, result.Success)
		require.Nil(t, result.Error)
	})

	t.Run("good password file", func(t *testing.T) {
		testDir := t.TempDir()
		fmt.Println("test dir:", testDir)
		require.NoError(t, os.WriteFile(filepath.Join(testDir, "secret-foo"), []byte("bar"), 0o666))

		server := mockRemoteServer(false, "foo", "bar")
		defer server.Close()

		configYaml := fmt.Sprintf(`
global:
  scrape_interval: 15s
  evaluation_interval: 15s
remote_write:
  - name: "test"
    url: "%s"
    basic_auth:
      username: foo
      password_file: secret-foo
`, server.URL)

		config, err := p8sconfig.Load(configYaml, false, rootLogger)
		require.NoError(t, err)
		config.SetDirectory(testDir)
		require.NoError(t, s.ApplyConfig(config))

		results, err := s.TestRemoteWriteRemotes([]string{"test"})
		require.NoError(t, err)
		require.Len(t, results, 1)
		result := results[0]
		require.Equal(t, "test", result.RemoteName)
		require.Equal(t, "success", result.Message)
		require.Equal(t, true, result.Success)
		require.Nil(t, result.Error)
	})

	t.Run("not exist remote", func(t *testing.T) {
		server := mockRemoteServer(true, "", "")
		defer server.Close()

		configYaml := fmt.Sprintf(`
global:
  scrape_interval: 15s
  evaluation_interval: 15s
remote_write:
  - name: "test"
    url: "%s"
    basic_auth:
      username: foo
      password: bar
`, server.URL)
		config, err := p8sconfig.Load(configYaml, false, rootLogger)
		require.NoError(t, err)
		require.NoError(t, s.ApplyConfig(config))

		results, err := s.TestRemoteWriteRemotes([]string{"test2"})
		require.NoError(t, err)
		require.Len(t, results, 1)
		result := results[0]
		require.Equal(t, "test2", result.RemoteName)
		require.Equal(t, `remote write config not found: [remote=test2]`, result.Message)
		require.Equal(t, false, result.Success)
		require.Nil(t, result.Error)
	})
}
