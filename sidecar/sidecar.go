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
	"github.com/go-kit/log/level"
	"github.com/pkg/errors"
	"gopkg.in/yaml.v3"

	p8sconfig "github.com/prometheus/prometheus/config"
	"github.com/prometheus/prometheus/model/rulefmt"
	_ "github.com/prometheus/prometheus/plugins" // RegisterPrivate plugins.
	"github.com/prometheus/prometheus/sidecar/errs"
	fsutil "github.com/prometheus/prometheus/sidecar/utils/fs"
)

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
	ZoneId      string            `json:"zone_id"`
	Yaml        string            `json:"yaml"`
	RuleFiles   []ruleFileInner   `json:"rule_files"`
	SecretFiles []secretFileInner `json:"secret_files"`
}

func (cmd *UpdateConfigCmd) Validate(logger log.Logger) errs.ValidateErrors {
	ves := make(errs.ValidateErrors, 0)
	if cmd.ZoneId = strings.TrimSpace(cmd.ZoneId); cmd.ZoneId == "" {
		ves = append(ves, "ZoneId must not be blank")
	}
	if cmd.Yaml = strings.TrimSpace(cmd.Yaml); cmd.Yaml == "" {
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

type ResetConfigCmd struct {
	ZoneId string `json:"zone_id"`
}

type SidecarService interface {
	// UpdateConfigReload 更新 Prometheus 配置文件
	//  更新 Prometheus 配置文件，包括 secret 和 rule 文件
	//  把 Prometheus 和 ZoneId 绑定（如之前没绑定过，否则报错）
	//  更新 “配置变更时间戳”
	//  指示 Prometheus reload，
	UpdateConfigReload(ctx context.Context, cmd *UpdateConfigCmd, reloadCh chan chan error) error

	// GetLastUpdateTs 获得上一次更新配置文件的时间，以及绑定的 ZoneId
	GetLastUpdateTs() (boundZoneId string, ts time.Time)

	// ResetConfigReload 恢复 Prometheus 的配置
	//  清空所有配置
	//  清空 “配置变更时间戳”
	//  指示 Prometheus reload
	ResetConfigReload(ctx context.Context, zoneId string, reloadCh chan chan error) error
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
	boundZoneId  string    // 当前所绑定的 zoneId
	lastUpdateTs time.Time // 上一次更新配置文件的时间戳
}

const (
	rulesDir          = "rules"
	secretsDir        = "secrets"
	ruleFilePattern   = "rules-*"
	secretFilePattern = "secret-*"
)

func (s *sidecarService) GetLastUpdateTs() (boundZoneId string, ts time.Time) {
	s.lock.Lock()
	defer s.lock.Unlock()
	return s.boundZoneId, s.lastUpdateTs
}

// 修改 Cmd 中 RuleFile 的文件名，修改路径为 rules/rules-* ，同时返回修改后的文件名列表
func (s *sidecarService) normalizeRuleFiles(cmd *UpdateConfigCmd) (ruleFileNames []string) {
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
	// 更新 scrape_config.basic_auth.password_file
	for _, scrapeConfig := range config.ScrapeConfigs {
		if ba := scrapeConfig.HTTPClientConfig.BasicAuth; ba != nil && ba.PasswordFile != "" {
			ba.PasswordFile = oldNewSecretFileName[ba.PasswordFile]
		}
	}
	// 更新 remote_write_config.basic_auth.password_file
	for _, rwConfig := range config.RemoteWriteConfigs {
		if ba := rwConfig.HTTPClientConfig.BasicAuth; ba != nil && ba.PasswordFile != "" {
			ba.PasswordFile = oldNewSecretFileName[ba.PasswordFile]
		}
	}
}

// 规则文件写到磁盘
func (s *sidecarService) writeRuleFiles(ruleFiles []ruleFileInner) ([]string, error) {
	basedir := filepath.Join(filepath.Dir(s.configFile), rulesDir)
	fileContents := make([]fsutil.FileContent, 0, len(ruleFiles))
	for _, rf := range ruleFiles {
		fileContents = append(fileContents, fsutil.FileContent{Filename: rf.FileName, Content: []byte(rf.Yaml)})
	}
	return fsutil.WriteDirFiles(0o755, basedir, 0o644, fileContents)
}

func (s *sidecarService) backupRuleFiles() error {
	basedir := filepath.Join(filepath.Dir(s.configFile), rulesDir)
	return fsutil.BackupDirFiles(basedir, ruleFilePattern)
}

func (s *sidecarService) cleanBackupRuleFiles() error {
	basedir := filepath.Join(filepath.Dir(s.configFile), rulesDir)
	return fsutil.CleanBackupDirFiles(basedir, ruleFilePattern)
}

func (s *sidecarService) restoreRuleFiles() error {
	basedir := filepath.Join(filepath.Dir(s.configFile), rulesDir)
	return fsutil.RestoreDirFiles(basedir, ruleFilePattern)
}

func (s *sidecarService) writeSecretFiles(secretFiles []secretFileInner) ([]string, error) {
	basedir := filepath.Join(filepath.Dir(s.configFile), secretsDir)
	fileContents := make([]fsutil.FileContent, 0, len(secretFiles))
	for _, sf := range secretFiles {
		fileContents = append(fileContents, fsutil.FileContent{Filename: sf.FileName, Content: []byte(sf.Secret)})
	}
	return fsutil.WriteDirFiles(0o755, basedir, 0o644, fileContents)
}

func (s *sidecarService) backupSecretFiles() error {
	basedir := filepath.Join(filepath.Dir(s.configFile), secretsDir)
	return fsutil.BackupDirFiles(basedir, secretFilePattern)
}

func (s *sidecarService) restoreSecretFiles() error {
	basedir := filepath.Join(filepath.Dir(s.configFile), secretsDir)
	return fsutil.RestoreDirFiles(basedir, secretFilePattern)
}

func (s *sidecarService) cleanBackupSecretFiles() error {
	basedir := filepath.Join(filepath.Dir(s.configFile), secretsDir)
	return fsutil.CleanBackupDirFiles(basedir, secretFilePattern)
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

func (s *sidecarService) backupPromConfigFile() error {
	return fsutil.BackupFile(s.configFile)
}

func (s *sidecarService) cleanBackupPromConfigFile() error {
	return fsutil.CleanBackupFile(s.configFile)
}

func (s *sidecarService) restorePromConfigFile() error {
	return fsutil.RestoreFile(s.configFile)
}

func (s *sidecarService) UpdateConfigReload(ctx context.Context, cmd *UpdateConfigCmd, reloadCh chan chan error) error {
	s.lock.Lock()
	defer s.lock.Unlock()

	verrs := cmd.Validate(s.logger)
	if len(verrs) > 0 {
		return verrs
	}

	if err := s.assertZoneIdMatch(cmd.ZoneId); err != nil {
		return err
	}

	// 更新规则文件名
	ruleFileNames := s.normalizeRuleFiles(cmd)
	// 更新 Secret 文件名
	oldNewSecretFileName := s.normalizeSecretFiles(cmd)

	// 更新 Prometheus 的配置文件
	config, _ := cmd.ToPromConfig(s.logger)
	s.updatePromConfigRuleFiles(config, ruleFileNames)
	s.updatePromConfigSecretFiles(config, oldNewSecretFileName)

	var reloadErr error
	writtenFiles := make([]string, 0, len(cmd.RuleFiles)+len(cmd.SecretFiles))

	defer func() {
		if reloadErr == nil {
			s.lastUpdateTs = time.Now()
			s.bindZoneId(cmd.ZoneId)
			// 没有出错
			s.printErr(s.cleanBackupRuleFiles())
			s.printErr(s.cleanBackupSecretFiles())
			s.printErr(s.cleanBackupPromConfigFile())
		} else {
			// 出错了
			s.printErr(s.restoreRuleFiles())
			s.printErr(s.restoreSecretFiles())
			s.printErr(s.restorePromConfigFile())
			s.printErr(fsutil.RemoveFiles(writtenFiles))
		}
	}()

	if reloadErr = s.backupPromConfigFile(); reloadErr != nil {
		return reloadErr
	}
	if reloadErr = s.backupRuleFiles(); reloadErr != nil {
		return reloadErr
	}
	if reloadErr = s.backupSecretFiles(); reloadErr != nil {
		return reloadErr
	}

	// 配置文件写到磁盘
	if reloadErr = s.writePromConfigFile(config); reloadErr != nil {
		return reloadErr
	}
	// 规则文件写到磁盘
	if wFiles, subReloadErr := s.writeRuleFiles(cmd.RuleFiles); reloadErr != nil {
		reloadErr = subReloadErr
		writtenFiles = append(writtenFiles, wFiles...)
		return reloadErr
	} else {
		writtenFiles = append(writtenFiles, wFiles...)
	}
	// Secret 文件写到磁盘
	if wFiles, subReloadErr := s.writeSecretFiles(cmd.SecretFiles); reloadErr != nil {
		reloadErr = subReloadErr
		writtenFiles = append(writtenFiles, wFiles...)
		return reloadErr
	} else {
		writtenFiles = append(writtenFiles, wFiles...)
	}

	// 指示 Prometheus reload 配置文件
	reloadErr = s.doReload(reloadCh)
	return reloadErr
}

func (s *sidecarService) printErr(err error) {
	if err == nil {
		return
	}

	if errList, ok := err.(fsutil.ErrorList); ok {
		for _, err2 := range errList {
			level.Warn(s.logger).Log("err", err2)
		}
	} else {
		level.Warn(s.logger).Log("err", err)
	}
}

const (
	emptyCfgYaml = `global:
  evaluation_interval: 15s
  scrape_interval: 15s
  scrape_timeout: 10s`
)

func (s *sidecarService) ResetConfigReload(ctx context.Context, zoneId string, reloadCh chan chan error) error {
	s.lock.Lock()
	defer s.lock.Unlock()

	if zoneId = strings.TrimSpace(zoneId); zoneId == "" {
		return errs.ValidateError("ZoneId must not be blank")
	}

	if err := s.assertZoneIdMatch(zoneId); err != nil {
		return err
	}

	var reloadErr error

	defer func() {
		if reloadErr == nil {
			s.lastUpdateTs = time.Time{}
			// 没有出错
			s.printErr(s.cleanBackupRuleFiles())
			s.printErr(s.cleanBackupSecretFiles())
			s.printErr(s.cleanBackupPromConfigFile())
		} else {
			// 出错了
			s.printErr(s.restoreRuleFiles())
			s.printErr(s.restoreSecretFiles())
			s.printErr(s.restorePromConfigFile())
		}
	}()
	if reloadErr = s.backupPromConfigFile(); reloadErr != nil {
		return reloadErr
	}
	if reloadErr = s.backupRuleFiles(); reloadErr != nil {
		return reloadErr
	}
	if reloadErr = s.backupSecretFiles(); reloadErr != nil {
		return reloadErr
	}

	var emptyCfg *p8sconfig.Config
	emptyCfg, reloadErr = p8sconfig.Load(emptyCfgYaml, false, s.logger)
	if reloadErr != nil {
		return reloadErr
	}

	// 配置文件写到磁盘
	if reloadErr = s.writePromConfigFile(emptyCfg); reloadErr != nil {
		return reloadErr
	}

	// 指示 Prometheus reload 配置文件
	reloadErr = s.doReload(reloadCh)
	return reloadErr
}

func (s *sidecarService) assertZoneIdMatch(zoneId string) error {
	if s.boundZoneId == "" {
		// 这个 prometheus 还没有和 zone 绑定过
		return nil
	}
	if s.boundZoneId != zoneId {
		return errs.ValidateErrorf("Current prometheus bound zoneId=%s, command zoneId=%s, mismatch",
			s.boundZoneId, zoneId)
	}
	return nil
}

func (s *sidecarService) bindZoneId(zoneId string) {
	s.boundZoneId = zoneId
}

func (s *sidecarService) unbindZoneId() {
	s.boundZoneId = ""
}

func (s *sidecarService) doReload(reloadCh chan chan error) error {
	rc := make(chan error)
	reloadCh <- rc
	if err := <-rc; err != nil {
		return errors.Wrapf(err, "sidecar failed to reload config")
	}
	return nil
}
