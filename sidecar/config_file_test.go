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
	"fmt"
	"os"
	"path/filepath"
	"testing"

	p8scommonconfig "github.com/prometheus/common/config"
	"github.com/stretchr/testify/require"

	p8sconfig "github.com/prometheus/prometheus/config"
)

func Test_configFileUtil_normalizeRuleFiles(t *testing.T) {
	s := &configFileUtil{
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
			wantRuleFileNames: []string{
				filepath.Join("rules", "rules-foo.yaml"),
				filepath.Join("rules", "rules-bar.yml"),
			},
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

func Test_configFileUtil_normalizeSecretFiles(t *testing.T) {
	s := &configFileUtil{
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
				"foo": filepath.Join("secrets", "secret-foo"),
				"bar": filepath.Join("secrets", "secret-bar"),
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

func Test_configFileUtil_updatePromConfigSecretFiles(t *testing.T) {
	s := &configFileUtil{
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

func Test_configFileUtil_writeSecretFiles(t *testing.T) {
	testDir, err := os.MkdirTemp("", "prom-config")
	require.NoError(t, err)
	fmt.Println("test dir:", testDir)
	defer os.RemoveAll(testDir)

	s := &configFileUtil{
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

func Test_configFileUtil_writeRuleFiles(t *testing.T) {
	testDir, err := os.MkdirTemp("", "prom-config")
	require.NoError(t, err)
	fmt.Println("test dir:", testDir)
	defer os.RemoveAll(testDir)

	s := &configFileUtil{
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
