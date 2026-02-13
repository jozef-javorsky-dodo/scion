// Copyright 2026 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package daemon

import (
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWaitForExit_AlreadyStopped(t *testing.T) {
	// Use a temp dir with no PID file — daemon is not running
	dir := t.TempDir()

	err := WaitForExit(dir, 1*time.Second)
	assert.NoError(t, err)
}

func TestWaitForExit_StalePIDFile(t *testing.T) {
	// Write a PID file with a PID that doesn't correspond to a running process
	dir := t.TempDir()

	// PID 99999999 is extremely unlikely to exist
	err := WritePID(dir, 99999999)
	require.NoError(t, err)

	err = WaitForExit(dir, 1*time.Second)
	assert.NoError(t, err)
}

func TestWaitForExit_Timeout(t *testing.T) {
	// Write our own PID — this process is always running
	dir := t.TempDir()

	err := WritePID(dir, os.Getpid())
	require.NoError(t, err)

	err = WaitForExit(dir, 500*time.Millisecond)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "did not exit")

	// Clean up the PID file
	_ = RemovePID(dir)
}

func TestStatus_NoPIDFile(t *testing.T) {
	dir := t.TempDir()

	running, _, err := Status(dir)
	assert.False(t, running)
	assert.ErrorIs(t, err, ErrNotRunning)
}

func TestStatus_StalePID(t *testing.T) {
	dir := t.TempDir()

	err := WritePID(dir, 99999999)
	require.NoError(t, err)

	running, _, err := Status(dir)
	assert.False(t, running)
	assert.ErrorIs(t, err, ErrNotRunning)
}

func TestWriteReadPID(t *testing.T) {
	dir := t.TempDir()

	err := WritePID(dir, 12345)
	require.NoError(t, err)

	pid, err := ReadPID(dir)
	assert.NoError(t, err)
	assert.Equal(t, 12345, pid)
}

func TestRemovePID(t *testing.T) {
	dir := t.TempDir()

	err := WritePID(dir, 12345)
	require.NoError(t, err)

	err = RemovePID(dir)
	assert.NoError(t, err)

	_, err = ReadPID(dir)
	assert.Error(t, err)
}

func TestGetLogPath(t *testing.T) {
	path := GetLogPath("/tmp/test")
	assert.Contains(t, path, "broker.log")
}

func TestGetPIDPath(t *testing.T) {
	path := GetPIDPath("/tmp/test")
	assert.Contains(t, path, "broker.pid")
}
