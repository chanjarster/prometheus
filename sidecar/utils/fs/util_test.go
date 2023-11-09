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
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBackupDirFiles(t *testing.T) {
	t.Run("exist origin files", func(t *testing.T) {
		testDir, err := os.MkdirTemp("", "fsutil")
		require.NoError(t, err)
		fmt.Println("test dir:", testDir)
		defer os.RemoveAll(testDir)

		require.NoError(t, os.WriteFile(filepath.Join(testDir, "foo.yml"), []byte("foo"), 0o666))
		require.NoError(t, os.WriteFile(filepath.Join(testDir, "bar.yml"), []byte("bar"), 0o666))
		require.NoError(t, os.WriteFile(filepath.Join(testDir, "zoo.yml"), []byte("zoo"), 0o666))

		require.NoError(t, BackupDirFiles(testDir, "*.yml"))
		require.NoFileExists(t, filepath.Join(testDir, "foo.yml"))
		require.NoFileExists(t, filepath.Join(testDir, "bar.yml"))
		require.NoFileExists(t, filepath.Join(testDir, "zoo.yml"))

		require.FileExists(t, filepath.Join(testDir, "foo.yml.del"))
		require.FileExists(t, filepath.Join(testDir, "bar.yml.del"))
		require.FileExists(t, filepath.Join(testDir, "zoo.yml.del"))
	})

	t.Run("not exist origin files", func(t *testing.T) {
		testDir, err := os.MkdirTemp("", "fsutil")
		require.NoError(t, err)
		fmt.Println("test dir:", testDir)
		defer os.RemoveAll(testDir)

		require.NoError(t, os.WriteFile(filepath.Join(testDir, "foo.yml"), []byte("foo"), 0o666))
		require.NoError(t, os.WriteFile(filepath.Join(testDir, "bar.yml"), []byte("bar"), 0o666))
		require.NoError(t, os.WriteFile(filepath.Join(testDir, "zoo.yml"), []byte("zoo"), 0o666))

		require.NoError(t, BackupDirFiles(testDir, "*.yaml"))

		require.FileExists(t, filepath.Join(testDir, "foo.yml"))
		require.FileExists(t, filepath.Join(testDir, "bar.yml"))
		require.FileExists(t, filepath.Join(testDir, "zoo.yml"))

		require.NoFileExists(t, filepath.Join(testDir, "foo.yaml.del"))
		require.NoFileExists(t, filepath.Join(testDir, "bar.yaml.del"))
		require.NoFileExists(t, filepath.Join(testDir, "zoo.yaml.del"))
	})

	t.Run("specific pattern", func(t *testing.T) {
		testDir, err := os.MkdirTemp("", "fsutil")
		require.NoError(t, err)
		fmt.Println("test dir:", testDir)
		defer os.RemoveAll(testDir)

		require.NoError(t, os.WriteFile(filepath.Join(testDir, "foo.yml"), []byte("foo"), 0o666))
		require.NoError(t, os.WriteFile(filepath.Join(testDir, "bar.yml"), []byte("bar"), 0o666))
		require.NoError(t, os.WriteFile(filepath.Join(testDir, "zoo.yml"), []byte("zoo"), 0o666))

		require.NoError(t, BackupDirFiles(testDir, "foo*"))

		require.NoFileExists(t, filepath.Join(testDir, "foo.yml"))
		require.FileExists(t, filepath.Join(testDir, "bar.yml"))
		require.FileExists(t, filepath.Join(testDir, "zoo.yml"))

		require.FileExists(t, filepath.Join(testDir, "foo.yml.del"))
		require.NoFileExists(t, filepath.Join(testDir, "bar.yml.del"))
		require.NoFileExists(t, filepath.Join(testDir, "zoo.yml.del"))
	})
}

func TestCleanBackupDirFiles(t *testing.T) {
	t.Run("exist backup files", func(t *testing.T) {
		testDir, err := os.MkdirTemp("", "fsutil")
		require.NoError(t, err)
		fmt.Println("test dir:", testDir)
		defer os.RemoveAll(testDir)

		require.NoError(t, os.WriteFile(filepath.Join(testDir, "foo.yml"), []byte("foo"), 0o666))
		require.NoError(t, os.WriteFile(filepath.Join(testDir, "bar.yml"), []byte("bar"), 0o666))
		require.NoError(t, os.WriteFile(filepath.Join(testDir, "zoo.yml"), []byte("zoo"), 0o666))

		require.NoError(t, BackupDirFiles(testDir, "*.yml"))
		require.NoError(t, CleanBackupDirFiles(testDir, "*.yml"))

		require.NoFileExists(t, filepath.Join(testDir, "foo.yml.del"))
		require.NoFileExists(t, filepath.Join(testDir, "bar.yml.del"))
		require.NoFileExists(t, filepath.Join(testDir, "zoo.yml.del"))
	})

	t.Run("not exist backup files", func(t *testing.T) {
		testDir, err := os.MkdirTemp("", "fsutil")
		require.NoError(t, err)
		fmt.Println("test dir:", testDir)
		defer os.RemoveAll(testDir)

		require.NoError(t, os.WriteFile(filepath.Join(testDir, "foo.yml"), []byte("foo"), 0o666))
		require.NoError(t, os.WriteFile(filepath.Join(testDir, "bar.yml"), []byte("bar"), 0o666))
		require.NoError(t, os.WriteFile(filepath.Join(testDir, "zoo.yml"), []byte("zoo"), 0o666))

		require.NoError(t, CleanBackupDirFiles(testDir, "*.yml"))

		require.FileExists(t, filepath.Join(testDir, "foo.yml"))
		require.FileExists(t, filepath.Join(testDir, "bar.yml"))
		require.FileExists(t, filepath.Join(testDir, "zoo.yml"))
	})

	t.Run("specific pattern", func(t *testing.T) {
		testDir, err := os.MkdirTemp("", "fsutil")
		require.NoError(t, err)
		fmt.Println("test dir:", testDir)
		defer os.RemoveAll(testDir)

		require.NoError(t, os.WriteFile(filepath.Join(testDir, "foo.yml"), []byte("foo"), 0o666))
		require.NoError(t, os.WriteFile(filepath.Join(testDir, "bar.yml"), []byte("bar"), 0o666))
		require.NoError(t, os.WriteFile(filepath.Join(testDir, "zoo.yml"), []byte("zoo"), 0o666))

		require.NoError(t, BackupDirFiles(testDir, "foo*"))
		require.NoError(t, CleanBackupDirFiles(testDir, "foo*"))

		require.NoFileExists(t, filepath.Join(testDir, "foo.yml"))
		require.FileExists(t, filepath.Join(testDir, "bar.yml"))
		require.FileExists(t, filepath.Join(testDir, "zoo.yml"))

		require.NoFileExists(t, filepath.Join(testDir, "foo.yml.del"))
		require.NoFileExists(t, filepath.Join(testDir, "bar.yml.del"))
		require.NoFileExists(t, filepath.Join(testDir, "zoo.yml.del"))
	})
}

func TestRestoreFile(t *testing.T) {
	t.Run("exist backup files", func(t *testing.T) {
		testDir, err := os.MkdirTemp("", "fsutil")
		require.NoError(t, err)
		fmt.Println("test dir:", testDir)
		defer os.RemoveAll(testDir)

		require.NoError(t, os.WriteFile(filepath.Join(testDir, "foo.yml"), []byte("foo"), 0o666))
		require.NoError(t, os.WriteFile(filepath.Join(testDir, "bar.yml"), []byte("bar"), 0o666))
		require.NoError(t, os.WriteFile(filepath.Join(testDir, "zoo.yml"), []byte("zoo"), 0o666))

		require.NoError(t, BackupDirFiles(testDir, "*.yml"))
		require.NoError(t, RestoreDirFiles(testDir, "*.yml"))

		require.FileExists(t, filepath.Join(testDir, "foo.yml"))
		require.FileExists(t, filepath.Join(testDir, "bar.yml"))
		require.FileExists(t, filepath.Join(testDir, "zoo.yml"))

		require.NoFileExists(t, filepath.Join(testDir, "foo.yml.del"))
		require.NoFileExists(t, filepath.Join(testDir, "bar.yml.del"))
		require.NoFileExists(t, filepath.Join(testDir, "zoo.yml.del"))
	})

	t.Run("not exist backup files", func(t *testing.T) {
		testDir, err := os.MkdirTemp("", "fsutil")
		require.NoError(t, err)
		fmt.Println("test dir:", testDir)
		defer os.RemoveAll(testDir)

		require.NoError(t, os.WriteFile(filepath.Join(testDir, "foo.yml"), []byte("foo"), 0o666))
		require.NoError(t, os.WriteFile(filepath.Join(testDir, "bar.yml"), []byte("bar"), 0o666))
		require.NoError(t, os.WriteFile(filepath.Join(testDir, "zoo.yml"), []byte("zoo"), 0o666))

		require.NoError(t, RestoreDirFiles(testDir, "*.yml"))

		require.FileExists(t, filepath.Join(testDir, "foo.yml"))
		require.FileExists(t, filepath.Join(testDir, "bar.yml"))
		require.FileExists(t, filepath.Join(testDir, "zoo.yml"))

		require.NoFileExists(t, filepath.Join(testDir, "foo.yml.del"))
		require.NoFileExists(t, filepath.Join(testDir, "bar.yml.del"))
		require.NoFileExists(t, filepath.Join(testDir, "zoo.yml.del"))
	})

	t.Run("specific pattern", func(t *testing.T) {
		testDir, err := os.MkdirTemp("", "fsutil")
		require.NoError(t, err)
		fmt.Println("test dir:", testDir)
		defer os.RemoveAll(testDir)

		require.NoError(t, os.WriteFile(filepath.Join(testDir, "foo.yml"), []byte("foo"), 0o666))
		require.NoError(t, os.WriteFile(filepath.Join(testDir, "bar.yml"), []byte("bar"), 0o666))
		require.NoError(t, os.WriteFile(filepath.Join(testDir, "zoo.yml"), []byte("zoo"), 0o666))

		require.NoError(t, BackupDirFiles(testDir, "foo*"))
		require.NoError(t, RestoreDirFiles(testDir, "foo*"))

		require.FileExists(t, filepath.Join(testDir, "foo.yml"))
		require.FileExists(t, filepath.Join(testDir, "bar.yml"))
		require.FileExists(t, filepath.Join(testDir, "zoo.yml"))

		require.NoFileExists(t, filepath.Join(testDir, "foo.yml.del"))
		require.NoFileExists(t, filepath.Join(testDir, "bar.yml.del"))
		require.NoFileExists(t, filepath.Join(testDir, "zoo.yml.del"))
	})
}
