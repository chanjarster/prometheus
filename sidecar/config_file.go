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
	"os"
	"path/filepath"

	"github.com/pkg/errors"
	"gopkg.in/yaml.v3"

	p8sconfig "github.com/prometheus/prometheus/config"
	fsutil "github.com/prometheus/prometheus/sidecar/utils/fs"
)

const (
	rulesDir          = "rules"
	secretsDir        = "secrets"
	ruleFilePattern   = "rules-*"
	secretFilePattern = "secret-*"
)

type configFileUtil struct {
	configFile string
}

// 修改 Cmd 中 RuleFile 的文件名，修改路径为 rules/rules-* ，同时返回修改后的文件名列表
func (s *configFileUtil) normalizeRuleFiles(cmd *UpdateConfigCmd) (ruleFileNames []string) {
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
func (s *configFileUtil) updatePromConfigRuleFiles(config *p8sconfig.Config, ruleFileNames []string) {
	config.RuleFiles = ruleFileNames
}

type secretFilesHolder interface {
	getSecretFiles() []secretFileInner
	setSecretFiles(sfs []secretFileInner)
}

// 修改 Cmd 中 SecretFile 的文件名，修改路径为 secrets/secret-*，同时返回 old -> new 文件名的 map
func (s *configFileUtil) normalizeSecretFiles(cmd secretFilesHolder) (oldNewSecretFileName map[string]string) {
	oldNewSecretFileName = make(map[string]string)
	secretFiles := cmd.getSecretFiles()
	for i, sf := range secretFiles {
		oldName := sf.FileName
		// 去掉文件名中的特殊符号，并修改文件名，secret-*
		sf.FileName = "secret-" + fsutil.NormFilename(oldName)
		secretFiles[i] = sf
		oldNewSecretFileName[oldName] = filepath.Join(secretsDir, sf.FileName)
	}
	cmd.setSecretFiles(secretFiles)
	return oldNewSecretFileName
}

// 更新 Prometheus Config 中所有和 Secret 有关的文件的信息
//
// 目前只负责 Scrape Config 和 Remote Write Config 中的 Basic Auth 的 password_file
func (s *configFileUtil) updatePromConfigSecretFiles(config *p8sconfig.Config, oldNewSecretFileName map[string]string) {
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
func (s *configFileUtil) writeRuleFiles(ruleFiles []ruleFileInner) ([]string, error) {
	basedir := filepath.Join(filepath.Dir(s.configFile), rulesDir)
	fileContents := make([]fsutil.FileContent, 0, len(ruleFiles))
	for _, rf := range ruleFiles {
		fileContents = append(fileContents, fsutil.FileContent{Filename: rf.FileName, Content: []byte(rf.Yaml)})
	}
	return fsutil.WriteDirFiles(0o755, basedir, 0o644, fileContents)
}

func (s *configFileUtil) backupRuleFiles() error {
	basedir := filepath.Join(filepath.Dir(s.configFile), rulesDir)
	return fsutil.BackupDirFiles(basedir, ruleFilePattern)
}

func (s *configFileUtil) cleanBackupRuleFiles() error {
	basedir := filepath.Join(filepath.Dir(s.configFile), rulesDir)
	return fsutil.CleanBackupDirFiles(basedir, ruleFilePattern)
}

func (s *configFileUtil) restoreRuleFiles() error {
	basedir := filepath.Join(filepath.Dir(s.configFile), rulesDir)
	return fsutil.RestoreDirFiles(basedir, ruleFilePattern)
}

func (s *configFileUtil) writeSecretFiles(secretFiles []secretFileInner) ([]string, error) {
	basedir := filepath.Join(filepath.Dir(s.configFile), secretsDir)
	fileContents := make([]fsutil.FileContent, 0, len(secretFiles))
	for _, sf := range secretFiles {
		fileContents = append(fileContents, fsutil.FileContent{Filename: sf.FileName, Content: []byte(sf.Secret)})
	}
	return fsutil.WriteDirFiles(0o755, basedir, 0o644, fileContents)
}

func (s *configFileUtil) backupSecretFiles() error {
	basedir := filepath.Join(filepath.Dir(s.configFile), secretsDir)
	return fsutil.BackupDirFiles(basedir, secretFilePattern)
}

func (s *configFileUtil) restoreSecretFiles() error {
	basedir := filepath.Join(filepath.Dir(s.configFile), secretsDir)
	return fsutil.RestoreDirFiles(basedir, secretFilePattern)
}

func (s *configFileUtil) cleanBackupSecretFiles() error {
	basedir := filepath.Join(filepath.Dir(s.configFile), secretsDir)
	return fsutil.CleanBackupDirFiles(basedir, secretFilePattern)
}

func (s *configFileUtil) writePromConfigFile(config *p8sconfig.Config) error {
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

func (s *configFileUtil) backupPromConfigFile() error {
	return fsutil.BackupFile(s.configFile)
}

func (s *configFileUtil) cleanBackupPromConfigFile() error {
	return fsutil.CleanBackupFile(s.configFile)
}

func (s *configFileUtil) restorePromConfigFile() error {
	return fsutil.RestoreFile(s.configFile)
}
