package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const maxBackupBytes int64 = 256 * 1024 * 1024

type BackupResult struct {
	Path string `json:"path"`
	Size int64  `json:"size"`
}

// Teleporter is binary data. Consume the authenticated response directly in Go,
// rather than passing ZIP bytes through the WebView's JSON/string transport.
func (a *App) BackupServer(id string) (BackupResult, error) {
	if _, err := a.server(id); err != nil {
		return BackupResult{}, err
	}
	response, err := a.Request(id, "/teleporter", "GET", "", true)
	if err != nil {
		return BackupResult{}, err
	}
	if err := responseError(response); err != nil {
		return BackupResult{}, err
	}
	defer a.CancelStream(response.Stream)
	a.mu.Lock()
	stream := a.streams[response.Stream]
	a.mu.Unlock()
	if stream == nil {
		return BackupResult{}, errors.New("backup download was cancelled")
	}
	stream.mu.Lock()
	defer stream.mu.Unlock()
	result, err := writeBackup(filepath.Join(filepath.Dir(a.config.path), "backups"), "pihole-server-"+id, ".zip", stream.body, maxBackupBytes, true)
	if err != nil {
		a.log.Warn("server backup failed", "server", id)
		return BackupResult{}, err
	}
	a.log.Info("server backup saved", "server", id, "bytes", result.Size)
	return result, nil
}

func (a *App) BackupConfig() (BackupResult, error) {
	data, err := json.MarshalIndent(a.config.Get(), "", "  ")
	if err != nil {
		return BackupResult{}, err
	}
	result, err := writeBackup(filepath.Join(filepath.Dir(a.config.path), "backups"), "pi-manager-config", ".json", bytes.NewReader(append(data, '\n')), maxBackupBytes, false)
	if err != nil {
		a.log.Warn("app configuration backup failed")
		return BackupResult{}, err
	}
	a.log.Info("app configuration backup saved", "bytes", result.Size)
	return result, nil
}

func writeBackup(dir, prefix, extension string, source io.Reader, limit int64, validateZIP bool) (BackupResult, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return BackupResult{}, fmt.Errorf("cannot create backup directory: %w", err)
	}
	file, err := os.CreateTemp(dir, prefix+"-"+time.Now().UTC().Format("20060102T150405Z")+"-*"+extension+".part")
	if err != nil {
		return BackupResult{}, fmt.Errorf("cannot create backup file: %w", err)
	}
	defer func() { file.Close(); os.Remove(file.Name()) }()
	size, err := io.Copy(file, io.LimitReader(source, limit+1))
	if err != nil {
		return BackupResult{}, errors.New("backup download or write was incomplete")
	}
	if size > limit {
		return BackupResult{}, errors.New("backup exceeds the download size limit")
	}
	if err := file.Sync(); err != nil {
		return BackupResult{}, fmt.Errorf("cannot flush backup file: %w", err)
	}
	if err := file.Close(); err != nil {
		return BackupResult{}, fmt.Errorf("cannot close backup file: %w", err)
	}
	if validateZIP {
		if err := validateBackupZIP(file.Name()); err != nil {
			return BackupResult{}, err
		}
	}
	path := strings.TrimSuffix(file.Name(), ".part")
	if err := os.Rename(file.Name(), path); err != nil {
		return BackupResult{}, fmt.Errorf("cannot save backup file: %w", err)
	}
	return BackupResult{Path: path, Size: size}, nil
}

func validateBackupZIP(path string) error {
	archive, err := zip.OpenReader(path)
	if err != nil {
		return errors.New("server returned an invalid backup ZIP archive")
	}
	defer archive.Close()
	return validateZIPContents(&archive.Reader)
}

func validateZIPContents(archive *zip.Reader) error {
	if len(archive.File) == 0 {
		return errors.New("server returned an empty backup ZIP archive")
	}
	// Check contents and CRCs without extracting files or trusting archive paths.
	remaining := int64(1024 * 1024 * 1024)
	for _, entry := range archive.File {
		if entry.UncompressedSize64 > uint64(remaining) {
			return errors.New("uncompressed backup exceeds the validation size limit")
		}
		reader, err := entry.Open()
		if err != nil {
			return errors.New("server returned an invalid backup ZIP entry")
		}
		size, readErr := io.Copy(io.Discard, io.LimitReader(reader, remaining+1))
		reader.Close()
		if readErr != nil || size > remaining {
			return errors.New("server returned a corrupt backup ZIP archive")
		}
		remaining -= size
	}
	return nil
}
