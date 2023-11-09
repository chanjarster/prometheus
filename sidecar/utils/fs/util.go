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

func WriteDirFiles(dirperm fs.FileMode, dir string,
	fileperm fs.FileMode, fileContents []FileContent,
) ([]string, error) {
	writtenFiles := make([]string, 0, len(fileContents))

	if err := os.MkdirAll(dir, dirperm); err != nil {
		return writtenFiles, errors.Wrapf(err, "mkdir %s failed", dir)
	}

	errList := make(ErrorList, 0, len(fileContents))
	// 写入新的规则文件
	for _, rf := range fileContents {
		writtenFile := filepath.Join(dir, rf.Filename)
		if err := os.WriteFile(writtenFile, rf.Content, fileperm); err != nil {
			errList = append(errList, errors.Wrapf(err, "Write file %q failed", rf.Filename))
		} else {
			writtenFiles = append(writtenFiles, writtenFile)
		}
	}

	if len(errList) == 0 {
		return writtenFiles, nil
	}
	return writtenFiles, errList
}

func RemoveFiles(files []string) error {
	errList := make(ErrorList, 0, len(files))

	for _, file := range files {
		if err := os.Remove(file); err != nil {
			errList = append(errList, errors.Wrapf(err, "Remove file %q failed", file))
		}
	}

	if len(errList) == 0 {
		return nil
	}
	return errList
}

// RemoveFilesByPattern 清空 dir 下的所有匹配 filePattern 的文件
func RemoveFilesByPattern(dir, filePattern string) error {
	searchPattern := filepath.Join(dir, filePattern)
	existFiles, err := filepath.Glob(searchPattern)
	if err != nil {
		return errors.Wrapf(err, "Search files %q failed", searchPattern)
	}
	errList := make(ErrorList, 0, len(existFiles))

	for _, file := range existFiles {
		if err := os.Remove(file); err != nil {
			errList = append(errList, errors.Wrapf(err, "Remove file %q failed", file))
		}
	}

	if len(errList) == 0 {
		return nil
	}
	return errList
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

const (
	backupSuffix = ".del"
)

// RestoreDirFiles 从 *.del 文件还原
func RestoreDirFiles(dir, originFilePattern string) error {
	searchPattern := filepath.Join(dir, originFilePattern+backupSuffix)
	existBackupFiles, err := filepath.Glob(searchPattern)
	if err != nil {
		return errors.Wrapf(err, "Search files %q failed", searchPattern)
	}

	errList := make(ErrorList, 0, 10)

	for _, backupFile := range existBackupFiles {
		originFile := strings.TrimSuffix(backupFile, backupSuffix)
		if err := RestoreFile(originFile); err != nil {
			errList = append(errList, err)
		}
	}

	if len(errList) == 0 {
		return nil
	}
	return errList
}

// CleanBackupDirFiles 清空 *.del 的备份文件
func CleanBackupDirFiles(dir, originFilePattern string) error {
	searchPattern := filepath.Join(dir, originFilePattern+backupSuffix)
	existBackupFiles, err := filepath.Glob(searchPattern)
	if err != nil {
		return errors.Wrapf(err, "Search files %q failed", searchPattern)
	}

	errList := make(ErrorList, 0, 10)

	for _, backupFile := range existBackupFiles {
		originFile := strings.TrimSuffix(backupFile, backupSuffix)
		if err := CleanBackupFile(originFile); err != nil {
			errList = append(errList, err)
		}
	}

	if len(errList) == 0 {
		return nil
	}
	return errList
}

// BackupDirFiles 把原始文件改个名字变成 *.del
func BackupDirFiles(dir, filePattern string) error {
	searchPattern := filepath.Join(dir, filePattern)
	existFiles, err := filepath.Glob(searchPattern)
	if err != nil {
		return errors.Wrapf(err, "Search files %q failed", searchPattern)
	}

	errList := make(ErrorList, 0, 10)
	for _, file := range existFiles {
		if err := BackupFile(file); err != nil {
			errList = append(errList, err)
		}
	}

	if len(errList) == 0 {
		return nil
	}
	return errList
}

// RestoreFile 从 *.del 文件还原，如果 *.del 文件不存在，那什么都不会发生
func RestoreFile(originFile string) error {
	backupFile := originFile + backupSuffix
	if err := os.Rename(backupFile, originFile); os.IsNotExist(err) {
		return nil
	} else {
		return errors.Wrapf(err, "Restore file %q failed", backupFile)
	}
}

// CleanBackupFile 清空 *.del 的备份文件，如果 *.del 文件不存在，那什么都不会发生
func CleanBackupFile(originFile string) error {
	backupFile := originFile + backupSuffix
	if err := os.Remove(backupFile); os.IsNotExist(err) {
		return nil
	} else {
		return errors.Wrapf(err, "Remove backup file %q failed", backupFile)
	}
}

// BackupFile 把原始文件改个名字变成 *.del，如果原始文件不存在，那什么都不会发生
func BackupFile(originFile string) error {
	backupFile := originFile + backupSuffix
	if err := os.Rename(originFile, backupFile); os.IsNotExist(err) {
		return nil
	} else {
		return errors.Wrapf(err, "Move file %q => %q failed", originFile, backupFile)
	}
}

type ErrorList []error

func (el ErrorList) Error() string {
	sb := &strings.Builder{}
	for i, err := range el {
		sb.WriteString(fmt.Sprintf("error[%d]: %s", i, err.Error()))
		if i < len(el)-1 {
			sb.WriteString("\n")
		}
	}
	return sb.String()
}
