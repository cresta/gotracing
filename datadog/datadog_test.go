package datadog

import (
	"io/ioutil"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var (
	tmpDir = "/tmp"
)

func generateTestFileName(t *testing.T) string {
	for {
		file, err := ioutil.TempFile(tmpDir, "datadog-file-exists")
		defer os.Remove(file.Name())
		assert.NoError(t, err)
		return file.Name()
	}
}

func TestFileExistsFailed(t *testing.T) {
	fileExistsCheckInterval = time.Millisecond * 100
	filename := generateTestFileName(t)
	require.False(t, fileExists(filename))
}

func TestFileExistsSuccess(t *testing.T) {
	fileExistsCheckInterval = time.Millisecond * 100
	filename := generateTestFileName(t)
	_, err := os.Create(filename)
	assert.NoError(t, err)
	defer os.Remove(filename)
	require.True(t, fileExists(filename))
}

func TestFileExistsAfterSeveralAttemptSuccess(t *testing.T) {
	fileExistsCheckInterval = time.Millisecond * 100
	filename := generateTestFileName(t)
	testFinished := make(chan struct{})
	goRoutingFinished := make(chan struct{})
	go func() {
		time.Sleep(fileExistsCheckInterval)
		f, err := os.Create(filename)
		assert.NoError(t, f.Close())
		defer os.Remove(filename)
		require.True(t, fileExists(filename))
		assert.NoError(t, err)
		<-testFinished
		close(goRoutingFinished)
	}()
	require.True(t, fileExists(filename))
	close(testFinished)
	<-goRoutingFinished
}

func TestFileExistsAfterSeveralAttemptTimeout(t *testing.T) {
	fileExistsCheckInterval = time.Millisecond * 100
	filename := generateTestFileName(t)
	testFinished := make(chan struct{})
	goRoutingFinished := make(chan struct{})
	go func() {
		time.Sleep(time.Second)
		f, err := os.Create(filename)
		assert.NoError(t, f.Close())
		defer os.Remove(filename)
		require.True(t, fileExists(filename))
		assert.NoError(t, err)
		<-testFinished
		close(goRoutingFinished)
	}()
	require.False(t, fileExists(filename))
	close(testFinished)
	<-goRoutingFinished
}
