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
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/go-kit/log"
	"github.com/pkg/errors"
	"gopkg.in/yaml.v3"

	p8sconfig "github.com/prometheus/prometheus/config"
	"github.com/prometheus/prometheus/model/rulefmt"
	_ "github.com/prometheus/prometheus/plugins" // RegisterPrivate plugins.
	"github.com/prometheus/prometheus/sidecar/errs"
	fsutil "github.com/prometheus/prometheus/sidecar/utils/fs"
)

var builtinJobs = map[string]bool{
	"prometheus":   true,
	"alertmanager": true,
	"pushgateway":  true,
	"blackbox":     true,
}

// isBuiltinJob 根据 Id / Name 判断是否内置 Job
//
// 目前的内置 Job 有 prometheus, alertmanager, pushgateway
func isBuiltinJob(idOrName string) bool {
	return builtinJobs[idOrName]
}

type ruleFileInner struct {
	FileName string `json:"filename"`
	Yaml     string `json:"yaml"`
}

func (rf ruleFileInner) Validate() errs.ValidateErrors {
	ves := make(errs.ValidateErrors, 0, 2)
	if strings.TrimSpace(rf.FileName) == "" {
		ves = append(ves, "Filename must not be blank")
	}
	if strings.TrimSpace(rf.Yaml) == "" {
		ves = append(ves, "Yaml must not be blank")
	}
	return ves
}

type secretFileInner struct {
	FileName string `json:"filename"`
	Secret   string `json:"secret"`
}

func (sf secretFileInner) Validate() errs.ValidateErrors {
	ves := make(errs.ValidateErrors, 0, 2)
	if strings.TrimSpace(sf.FileName) == "" {
		ves = append(ves, "Filename must not be blank")
	}
	if strings.TrimSpace(sf.Secret) == "" {
		ves = append(ves, "Secret must not be blank")
	}
	return ves
}

type UpdateConfigCmd struct {
	Yaml        string            `json:"yaml"`
	RuleFiles   []ruleFileInner   `json:"rule_files"`
	SecretFiles []secretFileInner `json:"secret_files"`
}

func (cmd *UpdateConfigCmd) Validate(logger log.Logger) errs.ValidateErrors {
	ves := make(errs.ValidateErrors, 0)
	if strings.TrimSpace(cmd.Yaml) == "" {
		ves = append(ves, "Yaml must not be blank")
	}
	for i, rf := range cmd.RuleFiles {
		ves = append(ves, rf.Validate().Prefix(fmt.Sprintf("RuleFiles[%d].", i))...)
	}
	for i, sf := range cmd.SecretFiles {
		ves = append(ves, sf.Validate().Prefix(fmt.Sprintf("SecretFiles[%d].", i))...)
	}

	// 验证一下配置文件有没有问题
	_, err := cmd.ToPromConfig(logger)
	if err != nil {
		ves = append(ves, errs.ValidateError(err.Error()).Prefix("Invalid Yaml: "))
	}

	// 验证一下每个RuleFile有没有问题
	for i, file := range cmd.RuleFiles {
		_, parseErrs := rulefmt.Parse([]byte(file.Yaml))
		for _, parseErr := range parseErrs {
			ves = append(ves, errs.ValidateError(parseErr.Error()).Prefix(fmt.Sprintf("Invalid RuleFiles[%d].Yaml: ", i)))
		}
	}

	return ves
}

func (cmd *UpdateConfigCmd) ToPromConfig(logger log.Logger) (*p8sconfig.Config, error) {
	return p8sconfig.Load(cmd.Yaml, false, logger)
}

type SidecarService interface {
	// UpdateConfigReload 更新 Prometheus 配置文件，并且指示 Prometheus reload
	UpdateConfigReload(ctx context.Context, cmd *UpdateConfigCmd, reloadCh chan chan error) error
	// GetLastUpdateTs 获得上一次更新配置文件的时间
	GetLastUpdateTs() time.Time
}

func New(logger log.Logger, configFile string) SidecarService {
	if logger == nil {
		logger = log.NewNopLogger()
	}
	return &sidecarService{
		logger:     logger,
		configFile: configFile,
	}
}

type sidecarService struct {
	logger       log.Logger
	configFile   string
	lock         sync.Mutex
	lastUpdateTs time.Time // 上一次更新配置文件的时间戳
}

const (
	rulesDir   = "rules"
	secretsDir = "secrets"
)

func (s *sidecarService) GetLastUpdateTs() time.Time {
	s.lock.Lock()
	defer s.lock.Unlock()
	return s.lastUpdateTs
}

// 修改 Cmd 中 RuleFile 的文件名，修改路径为 rules/rules-* ，同时返回修改后的文件名列表
func (s *sidecarService) normalizeCmdRuleFiles(cmd *UpdateConfigCmd) (ruleFileNames []string) {
	ruleFileNames = make([]string, len(cmd.RuleFiles))
	for i, rf := range cmd.RuleFiles {
		// 去掉文件名中的特殊符号, 并修改文件名，rules-*
		rf.FileName = "rules-" + fsutil.NormFilename(rf.FileName)
		cmd.RuleFiles[i] = rf
		ruleFileNames[i] = filepath.Join(rulesDir, rf.FileName)
	}
	return ruleFileNames
}

// 更新 Prometheus Config 的 RuleFiles
func (s *sidecarService) updatePromConfigRuleFiles(config *p8sconfig.Config, ruleFileNames []string) {
	config.RuleFiles = ruleFileNames
}

// 修改 Cmd 中 SecretFile 的文件名，修改路径为 secrets/secret-*，同时返回 old -> new 文件名的 map
func (s *sidecarService) normalizeSecretFiles(cmd *UpdateConfigCmd) (oldNewSecretFileName map[string]string) {
	oldNewSecretFileName = make(map[string]string)
	for i, sf := range cmd.SecretFiles {
		oldName := sf.FileName
		// 去掉文件名中的特殊符号，并修改文件名，secret-*
		sf.FileName = "secret-" + fsutil.NormFilename(oldName)
		cmd.SecretFiles[i] = sf
		oldNewSecretFileName[oldName] = filepath.Join(secretsDir, sf.FileName)
	}
	return oldNewSecretFileName
}

// 更新 Prometheus Config 中所有和 Secret 有关的文件的信息
func (s *sidecarService) updatePromConfigSecretFiles(config *p8sconfig.Config, oldNewSecretFileName map[string]string) {
	// 目前只更新 scrape_config.basic_auth.password_file
	for _, scrapeConfig := range config.ScrapeConfigs {
		if isBuiltinJob(scrapeConfig.JobName) {
			continue
		}
		if ba := scrapeConfig.HTTPClientConfig.BasicAuth; ba != nil && ba.PasswordFile != "" {
			ba.PasswordFile = oldNewSecretFileName[ba.PasswordFile]
		}
	}
}

// 规则文件写到磁盘
func (s *sidecarService) writeRuleFiles(cmd *UpdateConfigCmd) error {
	basedir := filepath.Join(filepath.Dir(s.configFile), rulesDir)
	fileContents := make([]fsutil.FileContent, 0, len(cmd.RuleFiles))
	for _, rf := range cmd.RuleFiles {
		fileContents = append(fileContents, fsutil.FileContent{Filename: rf.FileName, Content: []byte(rf.Yaml)})
	}
	// 清空旧的规则文件
	// 写入新的规则文件
	return fsutil.RefreshDirFiles(s.logger, 0o755, basedir, "rules-*.yaml", 0o644, fileContents)
}

func (s *sidecarService) writeSecretFiles(cmd *UpdateConfigCmd) error {
	basedir := filepath.Join(filepath.Dir(s.configFile), secretsDir)
	fileContents := make([]fsutil.FileContent, 0, len(cmd.SecretFiles))
	for _, sf := range cmd.SecretFiles {
		fileContents = append(fileContents, fsutil.FileContent{Filename: sf.FileName, Content: []byte(sf.Secret)})
	}
	// 清空旧的 Secret 文件
	// 写入新的 Secret 文件
	return fsutil.RefreshDirFiles(s.logger, 0o755, basedir, "secret-*.yaml", 0o644, fileContents)
}

func (s *sidecarService) writePromConfigFile(config *p8sconfig.Config) error {
	configYaml, err := yaml.Marshal(config)
	if err != nil {
		return errors.Wrap(err, "Marshal config failed")
	}

	err = os.WriteFile(s.configFile, configYaml, 0o644)
	if err != nil {
		return errors.Wrapf(err, "Write config file %q failed", s.configFile)
	}
	return nil
}

func (s *sidecarService) UpdateConfigReload(ctx context.Context, cmd *UpdateConfigCmd, reloadCh chan chan error) error {
	s.lock.Lock()
	defer s.lock.Unlock()

	verrs := cmd.Validate(s.logger)
	if len(verrs) > 0 {
		return verrs
	}

	// 更新规则文件名
	ruleFileNames := s.normalizeCmdRuleFiles(cmd)
	// 更新 Secret 文件名
	oldNewSecretFileName := s.normalizeSecretFiles(cmd)

	// 更新 Prometheus 的配置文件
	config, _ := cmd.ToPromConfig(s.logger)
	s.updatePromConfigRuleFiles(config, ruleFileNames)
	s.updatePromConfigSecretFiles(config, oldNewSecretFileName)

	if err := s.writePromConfigFile(config); err != nil {
		return err
	}

	// 规则文件写到磁盘
	if err := s.writeRuleFiles(cmd); err != nil {
		return err
	}
	// Secret 文件写到磁盘
	if err := s.writeSecretFiles(cmd); err != nil {
		return err
	}

	// 指示 Prometheus reload 配置文件
	err := s.doReload(reloadCh)
	if err == nil {
		s.lastUpdateTs = time.Now()
	}
	return err
}

func (s *sidecarService) doReload(reloadCh chan chan error) error {
	rc := make(chan error)
	reloadCh <- rc
	if err := <-rc; err != nil {
		return errors.Wrapf(err, "sidecar failed to reload config")
	}
	return nil
}
