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

package fsutil

import (
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/regexp"
	"github.com/pkg/errors"
)

type FileContent struct {
	Filename string
	Content  []byte
}

// RefreshDirFiles 清空 dir 下的所有匹配 filePattern 的文件，然后写入新文件
func RefreshDirFiles(logger log.Logger,
	dirperm fs.FileMode, dir string,
	filePattern string, fileperm fs.FileMode, fileContents []FileContent,
) error {
	if err := os.MkdirAll(dir, dirperm); err != nil {
		return errors.Wrapf(err, "mkdir %s failed", dir)
	}

	// 清空的规则文件
	searchPattern := filepath.Join(dir, filePattern)
	existFiles, err := filepath.Glob(searchPattern)
	if err != nil {
		return errors.Wrapf(err, "Search files %q failed", searchPattern)
	}
	for _, file := range existFiles {
		err = os.Remove(file)
		if err != nil && logger != nil {
			level.Warn(logger).Log("msg", fmt.Sprintf("Remove file %q failed", file), "err", err)
		}
	}

	// 写入新的规则文件
	for _, rf := range fileContents {
		err = os.WriteFile(filepath.Join(dir, rf.Filename), rf.Content, fileperm)
		if err != nil {
			return errors.Wrapf(err, "Write file %q failed", rf.Filename)
		}
	}
	return nil
}

const (
	badFilenameChars = `\s!@#$%^&*()+=\[\]\\\{\}\|;:'",<>/?~` + "`"
)

// NormFilename 把文件名中的空格符号等统统替换成下划线
func NormFilename(s string) string {
	reg := regexp.MustCompile("[" + badFilenameChars + "]+")
	return reg.ReplaceAllString(s, "_")
}

type FileContentConsumer func(filepath string, content []byte) error

type FilenameSuffixes []string

func (s FilenameSuffixes) IsMatch(filename string) bool {
	for _, suffix := range s {
		if strings.HasSuffix(filename, suffix) {
			return true
		}
	}
	return false
}

// ScanDir 递归扫描指定目录，找到所有后缀匹配的文件，消费文件内容
func ScanDir(logger log.Logger, dir string, filenameSuffixes FilenameSuffixes, consumer FileContentConsumer) error {
	files, err := os.ReadDir(dir)
	if err != nil {
		return errors.Wrapf(err, "scan dir %q error", dir)
	}

	for _, file := range files {
		filepath := path.Join(dir, file.Name())
		if file.IsDir() {
			err = ScanDir(logger, filepath, filenameSuffixes, consumer)
			if err != nil {
				return err
			}
			continue
		}

		if !filenameSuffixes.IsMatch(file.Name()) {
			continue
		}

		content, err := os.ReadFile(filepath)
		if err != nil {
			return errors.Wrapf(err, "read file %q error", filepath)
		}
		level.Info(logger).Log("msg", fmt.Sprintf("read file: %s", filepath))

		err = consumer(filepath, content)
		if err != nil {
			return errors.WithMessagef(err, "consume file content %q error", filepath)
		}
	}
	return nil
}
