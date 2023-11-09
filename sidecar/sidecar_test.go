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
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-kit/log"
	p8scommonconfig "github.com/prometheus/common/config"
	"github.com/stretchr/testify/require"

	p8sconfig "github.com/prometheus/prometheus/config"
)

func Test_sidecarService_normalizeCmdRuleFiles(t *testing.T) {
	s := &sidecarService{
		logger:     log.NewLogfmtLogger(os.Stdout),
		configFile: "/home/test/prometheus.yml",
	}
	type args struct {
		cmd *UpdateConfigCmd
	}
	tests := []struct {
		name              string
		args              args
		wantRuleFiles     []ruleFileInner
		wantRuleFileNames []string
	}{
		{
			name: "",
			args: args{
				cmd: &UpdateConfigCmd{
					RuleFiles: []ruleFileInner{
						{FileName: "foo.yaml", Yaml: "bar"},
						{FileName: "bar.yml", Yaml: "blah blah"},
					},
				},
			},
			wantRuleFiles: []ruleFileInner{
				{FileName: "rules-foo.yaml", Yaml: "bar"},
				{FileName: "rules-bar.yml", Yaml: "blah blah"},
			},
			wantRuleFileNames: []string{"rules/rules-foo.yaml", "rules/rules-bar.yml"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotRuleFileNames := s.normalizeRuleFiles(tt.args.cmd)
			require.Equal(t, tt.wantRuleFileNames, gotRuleFileNames)
			require.Equal(t, tt.wantRuleFiles, tt.args.cmd.RuleFiles)
		})
	}
}

func Test_sidecarService_normalizeSecretFiles(t *testing.T) {
	s := &sidecarService{
		logger:     log.NewLogfmtLogger(os.Stdout),
		configFile: "/home/test/prometheus.yml",
	}
	type args struct {
		cmd *UpdateConfigCmd
	}
	tests := []struct {
		name                     string
		args                     args
		wantSecretFiles          []secretFileInner
		wantOldNewSecretFileName map[string]string
	}{
		{
			name: "",
			args: args{
				cmd: &UpdateConfigCmd{
					SecretFiles: []secretFileInner{
						{FileName: "foo", Secret: "bar"},
						{FileName: "bar", Secret: "blah blah"},
					},
				},
			},
			wantSecretFiles: []secretFileInner{
				{FileName: "secret-foo", Secret: "bar"},
				{FileName: "secret-bar", Secret: "blah blah"},
			},
			wantOldNewSecretFileName: map[string]string{
				"foo": "secrets/secret-foo",
				"bar": "secrets/secret-bar",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotOldNewSecretFileName := s.normalizeSecretFiles(tt.args.cmd)
			require.Equal(t, tt.wantOldNewSecretFileName, gotOldNewSecretFileName)
			require.Equal(t, tt.wantSecretFiles, tt.args.cmd.SecretFiles)
		})
	}
}

func Test_sidecarService_updatePromConfigSecretFiles(t *testing.T) {
	s := &sidecarService{
		logger:     log.NewLogfmtLogger(os.Stdout),
		configFile: "/home/test/prometheus.yml",
	}
	type args struct {
		config               *p8sconfig.Config
		oldNewSecretFileName map[string]string
	}
	tests := []struct {
		name              string
		args              args
		wantScrapeConfigs []*p8sconfig.ScrapeConfig
	}{
		{
			args: args{
				config: &p8sconfig.Config{
					ScrapeConfigs: []*p8sconfig.ScrapeConfig{
						{},
						{
							HTTPClientConfig: p8scommonconfig.HTTPClientConfig{
								BasicAuth: &p8scommonconfig.BasicAuth{
									Username:     "test",
									PasswordFile: "foo",
								},
							},
						},
					},
				},
				oldNewSecretFileName: map[string]string{
					"foo": "secrets/secret-foo",
				},
			},
			wantScrapeConfigs: []*p8sconfig.ScrapeConfig{
				{},
				{
					HTTPClientConfig: p8scommonconfig.HTTPClientConfig{
						BasicAuth: &p8scommonconfig.BasicAuth{
							Username:     "test",
							PasswordFile: "secrets/secret-foo",
						},
					},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s.updatePromConfigSecretFiles(tt.args.config, tt.args.oldNewSecretFileName)
			require.Equal(t, tt.wantScrapeConfigs, tt.args.config.ScrapeConfigs)
		})
	}
}

func Test_sidecarService_writeSecretFiles(t *testing.T) {
	testDir, err := os.MkdirTemp("", "prom-config")
	require.NoError(t, err)
	fmt.Println("test dir:", testDir)
	defer os.RemoveAll(testDir)

	s := &sidecarService{
		logger:     log.NewLogfmtLogger(os.Stdout),
		configFile: filepath.Join(testDir, "prometheus.yml"),
	}
	type args struct {
		cmd *UpdateConfigCmd
	}
	tests := []struct {
		name    string
		args    args
		wantErr require.ErrorAssertionFunc
	}{
		{
			args: args{
				cmd: &UpdateConfigCmd{
					SecretFiles: []secretFileInner{
						{FileName: "secret-foo", Secret: "bar"},
						{FileName: "secret-bar", Secret: "blah blah"},
					},
				},
			},
			wantErr: require.NoError,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			files, err2 := s.writeSecretFiles(tt.args.cmd.SecretFiles)
			tt.wantErr(t, err2, fmt.Sprintf("writeSecretFiles(%v)", tt.args.cmd))
			require.Len(t, files, 2)
			require.FileExists(t, filepath.Join(testDir, "secrets", "secret-foo"))
			require.FileExists(t, filepath.Join(testDir, "secrets", "secret-bar"))
		})
	}
}

func Test_sidecarService_writeRuleFiles(t *testing.T) {
	testDir, err := os.MkdirTemp("", "prom-config")
	require.NoError(t, err)
	fmt.Println("test dir:", testDir)
	defer os.RemoveAll(testDir)

	s := &sidecarService{
		logger:     log.NewLogfmtLogger(os.Stdout),
		configFile: filepath.Join(testDir, "prometheus.yml"),
	}

	type args struct {
		cmd *UpdateConfigCmd
	}
	tests := []struct {
		name    string
		args    args
		wantErr require.ErrorAssertionFunc
	}{
		{
			args: args{
				cmd: &UpdateConfigCmd{
					RuleFiles: []ruleFileInner{
						{FileName: "rules-foo", Yaml: "bar"},
						{FileName: "rules-bar", Yaml: "blah blah"},
					},
				},
			},
			wantErr: require.NoError,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			files, err2 := s.writeRuleFiles(tt.args.cmd.RuleFiles)
			tt.wantErr(t, err2, fmt.Sprintf("writeRuleFiles(%v)", tt.args.cmd))
			require.Len(t, files, 2)
			require.FileExists(t, filepath.Join(testDir, "rules/rules-foo"))
			require.FileExists(t, filepath.Join(testDir, "rules/rules-bar"))
		})
	}
}

func Test_sidecarService_UpdateConfigReload(t *testing.T) {
	testDir, err := os.MkdirTemp("", "prom-config")
	require.NoError(t, err)
	fmt.Println("test dir:", testDir)
	defer os.RemoveAll(testDir)

	configFile := filepath.Join(testDir, "prometheus.yml")
	templateConfigYaml, err := os.ReadFile("../test-data/prometheus.yml")
	require.NoError(t, err)

	// 预先准备一些文件
	require.NoError(t, os.MkdirAll(filepath.Join(testDir, "secrets"), 0o777))
	require.NoError(t, os.MkdirAll(filepath.Join(testDir, "rules"), 0o777))
	require.NoError(t, os.WriteFile(configFile, templateConfigYaml, 0o666))
	require.NoError(t, os.WriteFile(filepath.Join(testDir, "secrets", "secret-a"), []byte("secret-a"), 0o666))
	require.NoError(t, os.WriteFile(filepath.Join(testDir, "secrets", "secret-b"), []byte("secret-b"), 0o666))
	require.NoError(t, os.WriteFile(filepath.Join(testDir, "rules", "rules-a"), []byte("rules-a"), 0o666))
	require.NoError(t, os.WriteFile(filepath.Join(testDir, "rules", "rules-b"), []byte("rules-b"), 0o666))

	s := &sidecarService{
		logger:     log.NewLogfmtLogger(os.Stdout),
		configFile: configFile,
	}

	require.Equal(t, time.Time{}, s.GetLastUpdateTs())

	cmd := &UpdateConfigCmd{
		Yaml: "global:\n  scrape_interval: 15s\n  scrape_timeout: 10s\n  evaluation_interval: 15s\nalerting:\n  alertmanagers:\n  - follow_redirects: true\n    enable_http2: true\n    scheme: http\n    timeout: 10s\n    api_version: v2\n    static_configs:\n    - targets: []\nscrape_configs:\n- job_name: prometheus\n  honor_timestamps: true\n  scrape_interval: 15s\n  scrape_timeout: 10s\n  metrics_path: /metrics\n  scheme: http\n  follow_redirects: true\n  enable_http2: true\n  static_configs:\n  - targets:\n    - localhost:9090\n",
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
	require.NotEqual(t, time.Time{}, s.GetLastUpdateTs())

	require.FileExists(t, filepath.Join(testDir, "secrets", "secret-foo"))
	require.FileExists(t, filepath.Join(testDir, "secrets", "secret-bar"))
	require.FileExists(t, filepath.Join(testDir, "rules", "rules-foo"))
	require.FileExists(t, filepath.Join(testDir, "rules", "rules-bar"))

	require.NoFileExists(t, filepath.Join(testDir, "secrets", "secret-a"))
	require.NoFileExists(t, filepath.Join(testDir, "secrets", "secret-b"))
	require.NoFileExists(t, filepath.Join(testDir, "rules", "rules-a"))
	require.NoFileExists(t, filepath.Join(testDir, "rules", "rules-b"))
}

func Test_sidecarService_UpdateConfigReload_ErrorRestore(t *testing.T) {
	testDir, err := os.MkdirTemp("", "prom-config")
	require.NoError(t, err)
	fmt.Println("test dir:", testDir)
	defer os.RemoveAll(testDir)

	configFile := filepath.Join(testDir, "prometheus.yml")
	templateConfigYaml, err := os.ReadFile("../test-data/prometheus.yml")
	require.NoError(t, err)

	// 预先准备一些文件
	require.NoError(t, os.MkdirAll(filepath.Join(testDir, "secrets"), 0o777))
	require.NoError(t, os.MkdirAll(filepath.Join(testDir, "rules"), 0o777))
	require.NoError(t, os.WriteFile(configFile, templateConfigYaml, 0o666))
	require.NoError(t, os.WriteFile(filepath.Join(testDir, "secrets", "secret-a"), []byte("secret-a"), 0o666))
	require.NoError(t, os.WriteFile(filepath.Join(testDir, "secrets", "secret-b"), []byte("secret-b"), 0o666))
	require.NoError(t, os.WriteFile(filepath.Join(testDir, "rules", "rules-a"), []byte("rules-a"), 0o666))
	require.NoError(t, os.WriteFile(filepath.Join(testDir, "rules", "rules-b"), []byte("rules-b"), 0o666))

	s := &sidecarService{
		logger:     log.NewLogfmtLogger(os.Stdout),
		configFile: configFile,
	}

	require.Equal(t, time.Time{}, s.GetLastUpdateTs())

	cmd := &UpdateConfigCmd{
		Yaml: "global:\n  scrape_interval: 15s\n  scrape_timeout: 10s\n  evaluation_interval: 15s\nalerting:\n  alertmanagers:\n  - follow_redirects: true\n    enable_http2: true\n    scheme: http\n    timeout: 10s\n    api_version: v2\n    static_configs:\n    - targets: []\nscrape_configs:\n- job_name: prometheus\n  honor_timestamps: true\n  scrape_interval: 15s\n  scrape_timeout: 10s\n  metrics_path: /metrics\n  scheme: http\n  follow_redirects: true\n  enable_http2: true\n  static_configs:\n  - targets:\n    - localhost:9090\n",
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

	configFileYaml, err := os.ReadFile(configFile)
	require.NoError(t, err)
	require.Equal(t, templateConfigYaml, configFileYaml)

	require.Equal(t, time.Time{}, s.GetLastUpdateTs())

	require.NoFileExists(t, filepath.Join(testDir, "secrets", "secret-foo"))
	require.NoFileExists(t, filepath.Join(testDir, "secrets", "secret-bar"))
	require.NoFileExists(t, filepath.Join(testDir, "rules", "rules-foo"))
	require.NoFileExists(t, filepath.Join(testDir, "rules", "rules-bar"))

	require.FileExists(t, filepath.Join(testDir, "secrets", "secret-a"))
	require.FileExists(t, filepath.Join(testDir, "secrets", "secret-b"))
	require.FileExists(t, filepath.Join(testDir, "rules", "rules-a"))
	require.FileExists(t, filepath.Join(testDir, "rules", "rules-b"))
}
