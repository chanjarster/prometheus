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
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gogo/protobuf/proto"
	"github.com/golang/snappy"

	"github.com/prometheus/prometheus/prompb"
	"github.com/prometheus/prometheus/storage/remote"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/pkg/errors"
	config_util "github.com/prometheus/common/config"
	"gopkg.in/yaml.v2"

	p8sconfig "github.com/prometheus/prometheus/config"
	"github.com/prometheus/prometheus/discovery"
	"github.com/prometheus/prometheus/discovery/targetgroup"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/model/rulefmt"
	"github.com/prometheus/prometheus/model/textparse"
	_ "github.com/prometheus/prometheus/plugins" // RegisterPrivate plugins.
	"github.com/prometheus/prometheus/scrape"
	"github.com/prometheus/prometheus/sidecar/errs"
	fsutil "github.com/prometheus/prometheus/sidecar/utils/fs"
)

type SidecarService interface {
	// UpdateConfigReload 更新 Prometheus 配置文件
	//  更新 Prometheus 配置文件，包括 secret 和 rule 文件
	//  把 Prometheus 和 ZoneId 绑定（如果之前绑定过别的，则报错）
	//  更新 “配置变更时间戳”
	//  指示 Prometheus reload，
	UpdateConfigReload(ctx context.Context, cmd *UpdateConfigCmd, reloadCh chan chan error) error

	// GetRuntimeInfo 获得上一次更新配置文件的时间，以及绑定的 ZoneId
	GetRuntimeInfo() *Runtimeinfo

	// ResetConfigReload 恢复 Prometheus 的配置
	//  清空所有 rules、scrape config 的配置
	//  解绑 ZoneId
	//  清空 “配置变更时间戳”
	//  指示 Prometheus reload
	ResetConfigReload(ctx context.Context, zoneId string, reloadCh chan chan error) error

	// ApplyConfig 接收来自 prometheus 的最新配置
	ApplyConfig(cfg *p8sconfig.Config) error

	// TestScrapeConfig 测试 ScrapeConfig 是否正确，能否能够正常抓取指标
	TestScrapeConfig(cmd *TestScrapeConfigCmd) (*ScrapeTestResult, error)

	// TestScrapeJobs 测试 prometheus 当前配置中的 job 是否能够正常抓取指标
	TestScrapeJobs(jobNames []string) ([]*ScrapeTestResult, error)

	// TestRemoteWriteConfig 测试 RemoteWriteConfig 是否正确，能否能够正常传输数据
	TestRemoteWriteConfig(cmd *TestRemoteWriteConfigCmd) (*RemoteWriteTestResult, error)

	// TestRemoteWriteRemotes 测试 prometheus 当前配置中的 remote write 是否能够传输数据
	TestRemoteWriteRemotes(remoteNames []string) ([]*RemoteWriteTestResult, error)
}

func New(logger log.Logger, configFile string, discoveryManager discoveryManager, scrapeOptions *scrape.Options) SidecarService {
	if logger == nil {
		logger = log.NewNopLogger()
	}
	return &sidecarService{
		logger:           logger,
		configFile:       configFile,
		scrapeOptions:    scrapeOptions,
		discoveryManager: discoveryManager,
	}
}

type sidecarService struct {
	logger        log.Logger
	configFile    string
	scrapeOptions *scrape.Options

	runtimeLock  sync.Mutex
	boundZoneId  string    // 当前所绑定的 zoneId
	lastUpdateTs time.Time // 上一次更新配置文件的时间戳

	cfgLock sync.Mutex
	cfg     *p8sconfig.Config // 当前 Prometheus 的配置

	discoverLock     sync.Mutex
	discoveryManager discoveryManager
}

const (
	brand = "prometheus-mod"
)

type Runtimeinfo struct {
	Brand        string    `json:"brand"`
	ZoneId       string    `json:"zone_id"`
	LastUpdateTs time.Time `json:"last_update_ts"`
}

func (r *Runtimeinfo) MarshalJSON() ([]byte, error) {
	if r == nil {
		return []byte("null"), nil
	}
	return json.Marshal(map[string]interface{}{
		"brand":          r.Brand,
		"zone_id":        r.ZoneId,
		"last_update_ts": r.LastUpdateTs.UnixMilli(),
	})
}

func (s *sidecarService) GetRuntimeInfo() *Runtimeinfo {
	s.runtimeLock.Lock()
	defer s.runtimeLock.Unlock()
	return &Runtimeinfo{
		Brand:        brand,
		ZoneId:       s.boundZoneId,
		LastUpdateTs: s.lastUpdateTs,
	}
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

func (cmd *UpdateConfigCmd) getSecretFiles() []secretFileInner {
	return cmd.SecretFiles
}

func (cmd *UpdateConfigCmd) setSecretFiles(sfs []secretFileInner) {
	cmd.SecretFiles = sfs
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

func (s *sidecarService) UpdateConfigReload(ctx context.Context, cmd *UpdateConfigCmd, reloadCh chan chan error) error {
	s.runtimeLock.Lock()
	defer s.runtimeLock.Unlock()

	verrs := cmd.Validate(s.logger)
	if len(verrs) > 0 {
		return verrs
	}

	if err := s.assertZoneIdMatch(cmd.ZoneId); err != nil {
		return err
	}

	cfgFileUtil := &configFileUtil{configFile: s.configFile}

	// 更新规则文件名
	ruleFileNames := cfgFileUtil.normalizeRuleFiles(cmd)
	// 更新 Secret 文件名
	oldNewSecretFileName := cfgFileUtil.normalizeSecretFiles(cmd)

	// 更新 Prometheus 的配置文件
	config, _ := cmd.ToPromConfig(s.logger)
	cfgFileUtil.updatePromConfigRuleFiles(config, ruleFileNames)
	cfgFileUtil.updatePromConfigSecretFiles(config, oldNewSecretFileName)

	var reloadErr error
	writtenFiles := make([]string, 0, len(cmd.RuleFiles)+len(cmd.SecretFiles))

	defer func() {
		if reloadErr == nil {
			s.lastUpdateTs = time.Now()
			s.bindZoneId(cmd.ZoneId)
			_ = s.ApplyConfig(config)
			// 没有出错
			s.printErr(cfgFileUtil.cleanBackupRuleFiles())
			s.printErr(cfgFileUtil.cleanBackupSecretFiles())
			s.printErr(cfgFileUtil.cleanBackupPromConfigFile())
		} else {
			// 出错了
			s.printErr(cfgFileUtil.restoreRuleFiles())
			s.printErr(cfgFileUtil.restoreSecretFiles())
			s.printErr(cfgFileUtil.restorePromConfigFile())
			s.printErr(fsutil.RemoveFiles(writtenFiles))
		}
	}()

	if reloadErr = cfgFileUtil.backupPromConfigFile(); reloadErr != nil {
		return reloadErr
	}
	if reloadErr = cfgFileUtil.backupRuleFiles(); reloadErr != nil {
		return reloadErr
	}
	if reloadErr = cfgFileUtil.backupSecretFiles(); reloadErr != nil {
		return reloadErr
	}

	// 配置文件写到磁盘
	if reloadErr = cfgFileUtil.writePromConfigFile(config); reloadErr != nil {
		return reloadErr
	}
	// 规则文件写到磁盘
	if wFiles, subReloadErr := cfgFileUtil.writeRuleFiles(cmd.RuleFiles); subReloadErr != nil {
		reloadErr = subReloadErr
		writtenFiles = append(writtenFiles, wFiles...)
		return reloadErr
	} else {
		writtenFiles = append(writtenFiles, wFiles...)
	}
	// Secret 文件写到磁盘
	if wFiles, subReloadErr := cfgFileUtil.writeSecretFiles(cmd.SecretFiles); subReloadErr != nil {
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

type ResetConfigCmd struct {
	ZoneId string `json:"zone_id"`
}

func (s *sidecarService) ResetConfigReload(ctx context.Context, zoneId string, reloadCh chan chan error) error {
	cfg := s.getConfigCopy()
	if cfg == nil {
		return nil
	}

	s.runtimeLock.Lock()
	defer s.runtimeLock.Unlock()

	if zoneId = strings.TrimSpace(zoneId); zoneId == "" {
		return errs.ValidateError("ZoneId must not be blank")
	}

	if err := s.assertZoneIdMatch(zoneId); err != nil {
		return err
	}

	cfgFileUtil := &configFileUtil{configFile: s.configFile}

	var reloadErr error

	defer func() {
		if reloadErr == nil {
			s.lastUpdateTs = time.Time{}
			s.boundZoneId = ""
			_ = s.ApplyConfig(cfg)
			// 没有出错
			s.printErr(cfgFileUtil.cleanBackupRuleFiles())
			s.printErr(cfgFileUtil.cleanBackupSecretFiles())
			s.printErr(cfgFileUtil.cleanBackupPromConfigFile())
		} else {
			// 出错了
			s.printErr(cfgFileUtil.restoreRuleFiles())
			s.printErr(cfgFileUtil.restoreSecretFiles())
			s.printErr(cfgFileUtil.restorePromConfigFile())
		}
	}()
	if reloadErr = cfgFileUtil.backupPromConfigFile(); reloadErr != nil {
		return reloadErr
	}
	if reloadErr = cfgFileUtil.backupRuleFiles(); reloadErr != nil {
		return reloadErr
	}
	if reloadErr = cfgFileUtil.backupSecretFiles(); reloadErr != nil {
		return reloadErr
	}

	// 清空所有 rules、scrape config 的配置
	cfg.ScrapeConfigs = make([]*p8sconfig.ScrapeConfig, 0)
	cfg.ScrapeConfigFiles = make([]string, 0)
	cfg.RuleFiles = make([]string, 0)

	// 配置文件写到磁盘
	if reloadErr = cfgFileUtil.writePromConfigFile(cfg); reloadErr != nil {
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

func (s *sidecarService) doReload(reloadCh chan chan error) error {
	rc := make(chan error)
	reloadCh <- rc
	if err := <-rc; err != nil {
		return errors.Wrapf(err, "sidecar failed to reload config")
	}
	return nil
}

func (s *sidecarService) ApplyConfig(cfg *p8sconfig.Config) error {
	s.cfgLock.Lock()
	defer s.cfgLock.Unlock()
	s.cfg = cfg
	return nil
}

type testError struct {
	err error
}

func (e *testError) MarshalJSON() ([]byte, error) {
	if e == nil {
		return []byte(`""`), nil
	}
	return json.Marshal(e.err.Error())
}

type ScrapeTestResult struct {
	lock           sync.Mutex
	JobName        string                `json:"job_name"`
	Success        bool                  `json:"success"`
	Message        string                `json:"message"`
	Error          *testError            `json:"error,omitempty"`
	DiscoverErrors []*testError          `json:"discover_errors,omitempty"`
	TargetResults  []*targetScrapeResult `json:"target_results"`
}

func (r *ScrapeTestResult) append(tr *targetScrapeResult) {
	r.lock.Lock()
	defer r.lock.Unlock()
	if !tr.Success {
		r.Success = false
		r.Message = "some target(s) failed"
	}
	r.TargetResults = append(r.TargetResults, tr)
}

type targetScrapeResult struct {
	Success          bool          `json:"success"`
	Labels           labels.Labels `json:"labels"`
	DiscoveredLabels labels.Labels `json:"discovered_labels"`
	Error            *testError    `json:"error"`
}

type TestScrapeJobsCmd struct {
	JobNames []string `json:"job_names"`
}

func (s *sidecarService) TestScrapeJobs(jobNames []string) ([]*ScrapeTestResult, error) {
	if len(jobNames) == 0 {
		return nil, nil
	}

	config := s.getConfigCopy()
	if config == nil {
		return nil, errors.New("prometheus no config ready")
	}

	scfgs, err := config.GetScrapeConfigs()
	if err != nil {
		return nil, err
	}

	scfgMap := make(map[string]*p8sconfig.ScrapeConfig)
	for _, scfg := range scfgs {
		scfgMap[scfg.JobName] = scfg
	}

	results := make([]*ScrapeTestResult, 0, len(jobNames))

	testScfgs := make([]*p8sconfig.ScrapeConfig, 0, len(jobNames))
	for _, name := range jobNames {
		if testScfg := scfgMap[name]; testScfg != nil {
			testScfgs = append(testScfgs, testScfg)
		} else {
			results = append(results, &ScrapeTestResult{
				JobName: name,
				Success: false,
				Message: fmt.Sprintf("scrape config not found: [job=%s]", name),
			})
		}
	}
	config.ScrapeConfigFiles = make([]string, 0)
	config.ScrapeConfigs = testScfgs

	if len(config.ScrapeConfigs) > 0 {
		if subResults, err := s.doTestScrapeWholeConfig(config); err != nil {
			return results, err
		} else {
			results = append(results, subResults...)
		}
	}
	return results, err
}

type TestScrapeConfigCmd struct {
	Yaml        string            `json:"yaml"`
	SecretFiles []secretFileInner `json:"secret_files"`
}

func (cmd *TestScrapeConfigCmd) Validate() errs.ValidateErrors {
	ves := make(errs.ValidateErrors, 0)
	if cmd.Yaml = strings.TrimSpace(cmd.Yaml); cmd.Yaml == "" {
		ves = append(ves, "Yaml must not be blank")
	}
	for i, sf := range cmd.SecretFiles {
		ves = append(ves, sf.Validate().Prefix(fmt.Sprintf("SecretFiles[%d].", i))...)
	}
	// 验证一下配置文件有没有问题
	_, err := cmd.ToScrapeConfig()
	if err != nil {
		ves = append(ves, errs.ValidateError(err.Error()).Prefix("Invalid Yaml: "))
	}
	return ves
}

func (cmd *TestScrapeConfigCmd) ToScrapeConfig() (*p8sconfig.ScrapeConfig, error) {
	cfg := &p8sconfig.ScrapeConfig{}
	*cfg = p8sconfig.DefaultScrapeConfig

	err := yaml.UnmarshalStrict([]byte(cmd.Yaml), cfg)
	if err != nil {
		return nil, err
	}
	return cfg, nil
}

func (cmd *TestScrapeConfigCmd) getSecretFiles() []secretFileInner {
	return cmd.SecretFiles
}

func (cmd *TestScrapeConfigCmd) setSecretFiles(sfs []secretFileInner) {
	cmd.SecretFiles = sfs
}

func (s *sidecarService) TestScrapeConfig(cmd *TestScrapeConfigCmd) (*ScrapeTestResult, error) {
	verrs := cmd.Validate()
	if len(verrs) > 0 {
		return nil, verrs
	}

	config := s.getConfigCopy()
	if config == nil {
		return nil, errors.New("prometheus no config ready")
	}

	tmpDir, err := os.MkdirTemp("", "prom-scrape-test")
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = os.RemoveAll(tmpDir)
	}()

	tmpConfigFile := filepath.Join(tmpDir, "prom-scrape-test.yml")
	cfgFileUtil := &configFileUtil{configFile: tmpConfigFile}
	oldNewSecretFileName := cfgFileUtil.normalizeSecretFiles(cmd)

	config.ScrapeConfigFiles = make([]string, 0)
	var scfg *p8sconfig.ScrapeConfig
	if scfg, err = cmd.ToScrapeConfig(); err != nil {
		return nil, err
	} else {
		config.ScrapeConfigs = []*p8sconfig.ScrapeConfig{scfg}
	}
	cfgFileUtil.updatePromConfigSecretFiles(config, oldNewSecretFileName)
	if _, err = cfgFileUtil.writeSecretFiles(cmd.SecretFiles); err != nil {
		return nil, err
	}
	scfg.SetDirectory(tmpDir) // 设定 basedir 为 tmpdir，否则会读不到文件的

	if results, err := s.doTestScrapeWholeConfig(config); err != nil {
		return nil, err
	} else if len(results) == 0 {
		return nil, errors.New("No test result")
	} else {
		return results[0], nil
	}
}

func (s *sidecarService) doTestScrapeWholeConfig(cfg *p8sconfig.Config) ([]*ScrapeTestResult, error) {
	s.discoverLock.Lock()
	defer s.discoverLock.Unlock()

	scfgs, err := cfg.GetScrapeConfigs()
	if err != nil {
		return nil, errors.Wrap(err, "Invalid scrape config")
	}

	if err := s.applyTestScrapeConfig(scfgs); err != nil {
		return nil, errors.Wrap(err, "Apply scrape config to discover manager failed")
	} else {
		defer func() {
			// 把 discovery manager 里的配置清空掉
			err := s.applyTestScrapeConfig(nil)
			if err != nil {
				level.Error(s.logger).Log("msg", "Clear discovery manager config failed")
			}
		}()
	}

	// discoveryManager 关闭的时候不会 close 这个 channel，所以需要超时 ticker 来控制一下
	tgSetsCh := s.discoveryManager.SyncCh()
	ticker := time.NewTicker(time.Second * 30)
	defer ticker.Stop()

	var tgSet map[string][]*targetgroup.Group
	select {
	case tgSet = <-tgSetsCh:
	case <-ticker.C:
		// 没有在规定时间内得到 target group
		// 目前 discoveryManager 没有提供得到发现目标时发生错误的机制，所以只能给一个大致错误
		return nil, errors.New("Could not discover targets in 30s, see logs for more information")
	}

	results := make([]*ScrapeTestResult, len(scfgs))
	for i, scfg := range scfgs {
		results[i] = s.doTestScrapeConfig(scfg, tgSet[scfg.JobName])
	}

	return results, nil
}

func (s *sidecarService) doTestScrapeConfig(scfg *p8sconfig.ScrapeConfig, tgs []*targetgroup.Group) (jobRes *ScrapeTestResult) {
	jobRes = &ScrapeTestResult{
		JobName:        scfg.JobName,
		Success:        false,
		Error:          nil,
		DiscoverErrors: make([]*testError, 0, 5),
	}

	// 把 target group 转换成 target
	lb := labels.NewBuilder(labels.EmptyLabels())
	var all []*scrape.Target
	var targets []*scrape.Target
	for _, tg := range tgs {
		targets, failures := scrape.TargetsFromGroup(tg, scfg, s.scrapeOptions.NoDefaultPort, targets, lb)
		for _, disErr := range failures {
			jobRes.DiscoverErrors = append(jobRes.DiscoverErrors, &testError{disErr})
		}
		for _, t := range targets {
			// Replicate .Labels().IsEmpty() with a loop here to avoid generating garbage.
			nonEmpty := false
			t.LabelsRange(func(l labels.Label) { nonEmpty = true })
			if nonEmpty {
				all = append(all, t)
			}
		}
	}

	if len(all) == 0 {
		jobRes.Message = "no targets discovered"
		return
	}

	// 下面开始的代码灵感来自于 scrape.scrapePool, scrapeLoop, targetScraper
	// 每个 scrape config 对应一个 scrape/scrapePool
	// scrape/scrapePool 为每个 target 维护一个 scrape/scrapeLoop
	// scrape/scrapeLoop 内有一个 scrape/targetScraper（针对每个 target）
	// scrape/targetScraper.scrape 真正负责抓指标

	client, err := config_util.NewClientFromConfig(scfg.HTTPClientConfig, scfg.JobName, s.scrapeOptions.HTTPClientOptions...)
	if err != nil {
		jobRes.Message = "create http client error"
		jobRes.Error = &testError{err}
		return
	}

	jobRes.Success = true
	jobRes.TargetResults = make([]*targetScrapeResult, 0, len(all))

	// 使用并行的方式来测试
	var wg sync.WaitGroup
	for _, t := range all {
		wg.Add(1)

		go func(client *http.Client, t *scrape.Target, scfg *p8sconfig.ScrapeConfig) {
			ts := s.newTargetScraper(client, t, scfg)
			scrapeErr := s.doScrapeTarget(ts, time.Duration(scfg.ScrapeTimeout), scfg.ScrapeClassicHistograms)
			if scrapeErr != nil {
				jobRes.append(&targetScrapeResult{
					Labels:           t.Labels(),
					DiscoveredLabels: t.DiscoveredLabels(),
					Success:          false,
					Error:            &testError{scrapeErr},
				})
			} else {
				jobRes.append(&targetScrapeResult{
					Labels:           t.Labels(),
					DiscoveredLabels: t.DiscoveredLabels(),
					Success:          true,
				})
			}
			wg.Done()
		}(client, t, scfg)
	}

	wg.Wait()

	if jobRes.Success {
		jobRes.Message = "success"
	}
	return jobRes
}

const (
	scrapeAcceptHeader             = `application/openmetrics-text;version=1.0.0,application/openmetrics-text;version=0.0.1;q=0.75,text/plain;version=0.0.4;q=0.5,*/*;q=0.1`
	scrapeAcceptHeaderWithProtobuf = `application/vnd.google.protobuf;proto=io.prometheus.client.MetricFamily;encoding=delimited,application/openmetrics-text;version=1.0.0;q=0.8,application/openmetrics-text;version=0.0.1;q=0.75,text/plain;version=0.0.4;q=0.5,*/*;q=0.1`
)

func (s *sidecarService) newTargetScraper(client *http.Client, t *scrape.Target, scfg *p8sconfig.ScrapeConfig) *targetScraper {
	var (
		timeout       = time.Duration(scfg.ScrapeTimeout)
		bodySizeLimit = int64(scfg.BodySizeLimit)
	)
	acceptHeader := scrapeAcceptHeader
	if s.scrapeOptions.EnableProtobufNegotiation {
		acceptHeader = scrapeAcceptHeaderWithProtobuf
	}
	ts := &targetScraper{Target: t, client: client, timeout: timeout, bodySizeLimit: bodySizeLimit, acceptHeader: acceptHeader}
	return ts
}

func (s *sidecarService) doScrapeTarget(ts *targetScraper, timeout time.Duration, scrapeClassicHistograms bool) error {
	// 以下代码 copy 自 scrape/scrapeLoop.scrapeAndReport
	buf := bytes.NewBuffer(make([]byte, 1024))
	defer buf.Reset()
	scrapeCtx, cancel := context.WithTimeout(context.Background(), timeout)
	contentType, scrapeErr := ts.scrape(scrapeCtx, buf)
	cancel()
	if scrapeErr != nil {
		return scrapeErr
	}

	// 通过正儿八级的解析来看返回结果是否有问题
	// 以下代码 copy 自 scrape/scrapeLoop.append
	p, err := textparse.New(buf.Bytes(), contentType, scrapeClassicHistograms)
	if err != nil {
		level.Debug(s.logger).Log(
			"msg", "Invalid content type on scrape, using prometheus parser as fallback.",
			"content_type", contentType,
			"err", err,
		)
	}
	var parseErr error
	for {
		if _, parseErr = p.Next(); parseErr != nil {
			if errors.Is(parseErr, io.EOF) {
				parseErr = nil
			}
			break
		}
	}

	return parseErr
}

func (s *sidecarService) getConfigCopy() *p8sconfig.Config {
	s.cfgLock.Lock()
	defer s.cfgLock.Unlock()
	if s.cfg == nil {
		return nil
	}
	cfgCopy := *s.cfg
	return &cfgCopy
}

// 给 discoveryManager 更新配置
func (s *sidecarService) applyTestScrapeConfig(scfgs []*p8sconfig.ScrapeConfig) error {
	c := make(map[string]discovery.Configs)
	for _, v := range scfgs {
		c[v.JobName] = v.ServiceDiscoveryConfigs
	}
	return s.discoveryManager.ApplyConfig(c)
}

type TestRemoteWriteConfigCmd struct {
	Yaml        string            `json:"yaml"`
	SecretFiles []secretFileInner `json:"secret_files"`
}

func (cmd *TestRemoteWriteConfigCmd) Validate() errs.ValidateErrors {
	ves := make(errs.ValidateErrors, 0)
	if cmd.Yaml = strings.TrimSpace(cmd.Yaml); cmd.Yaml == "" {
		ves = append(ves, "Yaml must not be blank")
	}
	for i, sf := range cmd.SecretFiles {
		ves = append(ves, sf.Validate().Prefix(fmt.Sprintf("SecretFiles[%d].", i))...)
	}
	// 验证一下配置文件有没有问题
	_, err := cmd.ToRemoteWriteConfig()
	if err != nil {
		ves = append(ves, errs.ValidateError(err.Error()).Prefix("Invalid Yaml: "))
	}
	return ves
}

func (cmd *TestRemoteWriteConfigCmd) ToRemoteWriteConfig() (*p8sconfig.RemoteWriteConfig, error) {
	cfg := &p8sconfig.RemoteWriteConfig{}
	*cfg = p8sconfig.DefaultRemoteWriteConfig

	err := yaml.UnmarshalStrict([]byte(cmd.Yaml), cfg)
	if err != nil {
		return nil, err
	}
	return cfg, nil
}

func (cmd *TestRemoteWriteConfigCmd) getSecretFiles() []secretFileInner {
	return cmd.SecretFiles
}

func (cmd *TestRemoteWriteConfigCmd) setSecretFiles(sfs []secretFileInner) {
	cmd.SecretFiles = sfs
}

type RemoteWriteTestResult struct {
	RemoteName string     `json:"remote_name"`
	Success    bool       `json:"success"`
	Message    string     `json:"message"`
	Error      *testError `json:"error,omitempty"`
}

func (s *sidecarService) TestRemoteWriteConfig(cmd *TestRemoteWriteConfigCmd) (*RemoteWriteTestResult, error) {
	verrs := cmd.Validate()
	if len(verrs) > 0 {
		return nil, verrs
	}

	config := s.getConfigCopy()
	if config == nil {
		return nil, errors.New("prometheus no config ready")
	}

	tmpDir, err := os.MkdirTemp("", "prom-remote-write-test")
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = os.RemoveAll(tmpDir)
	}()

	tmpConfigFile := filepath.Join(tmpDir, "prom-remote-write-test.yml")
	cfgFileUtil := &configFileUtil{configFile: tmpConfigFile}
	oldNewSecretFileName := cfgFileUtil.normalizeSecretFiles(cmd)

	var rwConf *p8sconfig.RemoteWriteConfig
	if rwConf, err = cmd.ToRemoteWriteConfig(); err != nil {
		return nil, err
	} else {
		config.RemoteWriteConfigs = []*p8sconfig.RemoteWriteConfig{rwConf}
	}
	cfgFileUtil.updatePromConfigSecretFiles(config, oldNewSecretFileName)
	if _, err = cfgFileUtil.writeSecretFiles(cmd.SecretFiles); err != nil {
		return nil, err
	}
	rwConf.SetDirectory(tmpDir) // 设定 basedir 为 tmpdir，否则会读不到文件的

	return s.doTestRemoteWriteConfig(rwConf), nil
}

type TestRemoteWriteRemotesCmd struct {
	RemoteNames []string `json:"remote_names"`
}

func (s *sidecarService) TestRemoteWriteRemotes(remoteNames []string) ([]*RemoteWriteTestResult, error) {
	if len(remoteNames) == 0 {
		return nil, nil
	}

	config := s.getConfigCopy()
	if config == nil {
		return nil, errors.New("prometheus no config ready")
	}

	rwCfgMap := make(map[string]*p8sconfig.RemoteWriteConfig)
	for _, rwCfg := range config.RemoteWriteConfigs {
		rwCfgMap[rwCfg.Name] = rwCfg
	}

	remoteWriteResList := make([]*RemoteWriteTestResult, 0, len(remoteNames))

	for _, name := range remoteNames {
		if rwCfg := rwCfgMap[name]; rwCfg != nil {
			remoteWriteResList = append(remoteWriteResList, s.doTestRemoteWriteConfig(rwCfg))
		} else {
			remoteWriteResList = append(remoteWriteResList, &RemoteWriteTestResult{
				RemoteName: name,
				Success:    false,
				Message:    fmt.Sprintf("remote write config not found: [remote=%s]", name),
				Error:      nil,
			})
		}
	}

	return remoteWriteResList, nil
}

func (s *sidecarService) doTestRemoteWriteConfig(rwCfg *p8sconfig.RemoteWriteConfig) *RemoteWriteTestResult {
	remoteRes := &RemoteWriteTestResult{RemoteName: rwCfg.Name, Success: false}
	c, err := remote.NewWriteClient(rwCfg.Name, &remote.ClientConfig{
		URL:              rwCfg.URL,
		Timeout:          rwCfg.RemoteTimeout,
		HTTPClientConfig: rwCfg.HTTPClientConfig,
		SigV4Config:      rwCfg.SigV4Config,
		AzureADConfig:    rwCfg.AzureADConfig,
		Headers:          rwCfg.Headers,
		RetryOnRateLimit: rwCfg.QueueConfig.RetryOnRateLimit,
	})
	if err != nil {
		remoteRes.Success = false
		remoteRes.Message = "build remote write client failed"
		remoteRes.Error = &testError{err}
		return remoteRes
	}

	/*
	  以下逻辑借鉴自 storage/remote/queue_manager.go:sendMetadataWithBackoff
	*/

	// 构建一个不知道可以干啥的 metric meta 上传一下看看
	pmm := []prompb.MetricMetadata{{
		MetricFamilyName: "_remote_write_test",
		Help:             "remote write test only, doesn't mean anything",
		Type:             prompb.MetricMetadata_INFO,
		Unit:             "",
	}}
	pBuf := proto.NewBuffer(nil)
	req, err := buildRemoteWriteRequest(pmm, pBuf)
	if err != nil {
		remoteRes.Success = false
		remoteRes.Message = "build remote write request failed"
		remoteRes.Error = &testError{err}
		return remoteRes
	}
	testCtx, testCancel := context.WithTimeout(context.Background(), time.Duration(rwCfg.RemoteTimeout))
	defer testCancel()
	if err = c.Store(testCtx, req); err != nil {
		remoteRes.Success = false
		remoteRes.Message = "write to remote failed"
		remoteRes.Error = &testError{err}
		return remoteRes
	}
	remoteRes.Success = true
	remoteRes.Message = "success"
	return remoteRes
}

// copy 自 storage/remote/queue_manager.go:buildWriteRequest
func buildRemoteWriteRequest(metadata []prompb.MetricMetadata, pBuf *proto.Buffer) ([]byte, error) {
	req := &prompb.WriteRequest{
		Metadata: metadata,
	}

	if pBuf == nil {
		pBuf = proto.NewBuffer(nil) // For convenience in tests. Not efficient.
	} else {
		pBuf.Reset()
	}
	err := pBuf.Marshal(req)
	if err != nil {
		return nil, err
	}

	// snappy uses len() to see if it needs to allocate a new slice. Make the
	// buffer as long as possible.
	compressed := snappy.Encode(nil, pBuf.Bytes())
	return compressed, nil
}

// discoveryManager interfaces the discovery manager. This is used to keep using
// the manager that restarts SD's on reload for a few releases until we feel
// the new manager can be enabled for all users.
type discoveryManager interface {
	ApplyConfig(cfg map[string]discovery.Configs) error
	Run() error
	SyncCh() <-chan map[string][]*targetgroup.Group
}
