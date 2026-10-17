package main

import (
	"archive/zip"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
)

// Leave space for multipart headers within Pi-hole's 50 MiB request limit.
const maxRestoreBytes = 49 * 1024 * 1024
const maxConfigRestoreBytes = 2 * 1024 * 1024

type RestoreOptions struct {
	Config     bool `json:"config"`
	DHCPLeases bool `json:"dhcp_leases"`
	Groups     bool `json:"groups"`
	Domains    bool `json:"domains"`
	Lists      bool `json:"lists"`
	Clients    bool `json:"clients"`
}

type RestoreResult struct {
	SafetyBackup BackupResult `json:"safetyBackup"`
}

type ConfigRestoreResult struct {
	Config       Config       `json:"config"`
	SafetyBackup BackupResult `json:"safetyBackup"`
}

func (a *App) RestoreServer(id, encoded string, options RestoreOptions) (RestoreResult, error) {
	if !options.Config && !options.DHCPLeases && !options.Groups && !options.Domains && !options.Lists && !options.Clients {
		return RestoreResult{}, errors.New("choose at least one section to restore")
	}
	data, err := decodeRestore(encoded, maxRestoreBytes)
	if err != nil {
		return RestoreResult{}, err
	}
	archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return RestoreResult{}, errors.New("choose a valid Pi-hole Teleporter ZIP archive")
	}
	if err := validateZIPContents(archive); err != nil {
		return RestoreResult{}, fmt.Errorf("invalid restore archive: %w", err)
	}
	// Keep server addresses and drift plans stable for the backup and import.
	a.operationMu.Lock()
	defer a.operationMu.Unlock()
	lock := a.authLock(id)
	lock.Lock()
	defer lock.Unlock()
	safety, err := a.BackupServer(id)
	if err != nil {
		return RestoreResult{}, fmt.Errorf("restore cancelled: could not back up current server: %w", err)
	}
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	// Send every supported flag explicitly. Omitting import would restore all.
	selection := map[string]any{
		"config": options.Config, "dhcp_leases": options.DHCPLeases,
		"gravity": map[string]bool{
			"group":      options.Groups,
			"domainlist": options.Domains, "domainlist_by_group": options.Domains,
			"adlist": options.Lists, "adlist_by_group": options.Lists,
			"client": options.Clients, "client_by_group": options.Clients,
		},
	}
	flags, _ := json.Marshal(selection)
	if err = form.WriteField("import", string(flags)); err == nil {
		var file io.Writer
		file, err = form.CreateFormFile("file", "teleporter.zip")
		if err == nil {
			_, err = file.Write(data)
		}
	}
	if err == nil {
		err = form.Close()
	}
	if err != nil {
		return RestoreResult{}, errors.New("could not prepare restore upload")
	}
	a.syncPlan = nil
	response, err := a.request(id, "/teleporter", "POST", &body, form.FormDataContentType(), false)
	if err == nil {
		err = responseError(response)
	}
	if err == nil {
		var imported struct {
			Files     []string `json:"files"`
			Processed []string `json:"processed"`
		}
		if json.Unmarshal([]byte(response.Body), &imported) != nil || len(imported.Files)+len(imported.Processed) == 0 {
			err = errors.New("Pi-hole did not report any restored files or tables")
		}
	}
	if response.Status >= 200 && response.Status < 300 {
		// Teleporter restarts FTL, even if the import list was empty.
		a.mu.Lock()
		delete(a.sessions, id)
		a.loggedOut[id] = true
		a.mu.Unlock()
	}
	if err != nil {
		a.log.Warn("server restore failed", "server", id)
		return RestoreResult{}, fmt.Errorf("restore did not complete successfully; check the server before retrying (pre-restore backup: %s): %w", safety.Path, err)
	}
	a.log.Info("server configuration restored", "server", id)
	return RestoreResult{SafetyBackup: safety}, nil
}

func (a *App) RestoreConfig(contents string) (ConfigRestoreResult, error) {
	if len(contents) > maxConfigRestoreBytes {
		return ConfigRestoreResult{}, errors.New("app settings file exceeds 2 MiB")
	}
	var cfg Config
	decoder := json.NewDecoder(bytes.NewBufferString(contents))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&cfg); err != nil {
		return ConfigRestoreResult{}, errors.New("choose a valid Pi-Manager settings JSON file")
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return ConfigRestoreResult{}, errors.New("settings file must contain one JSON object")
	}
	cfg, err := normalizeConfig(cfg)
	if err != nil {
		return ConfigRestoreResult{}, err
	}
	a.operationMu.Lock()
	defer a.operationMu.Unlock()
	// Wait for in-flight logins before replacing credentials and sessions.
	for i := 1; i <= 10; i++ {
		lock := a.authLock(fmt.Sprint(i))
		lock.Lock()
		defer lock.Unlock()
	}
	safety, err := a.BackupConfig()
	if err != nil {
		return ConfigRestoreResult{}, fmt.Errorf("restore cancelled: could not back up current app settings: %w", err)
	}
	if err := a.config.Save(cfg); err != nil {
		return ConfigRestoreResult{}, fmt.Errorf("could not save restored settings (pre-restore backup: %s): %w", safety.Path, err)
	}
	a.syncPlan = nil
	a.mu.Lock()
	a.sessions = map[string]session{}
	for i := 1; i <= 10; i++ {
		a.loggedOut[fmt.Sprint(i)] = true
	}
	a.mu.Unlock()
	a.log.Info("app configuration restored", "servers", len(cfg.Servers))
	return ConfigRestoreResult{Config: cfg, SafetyBackup: safety}, nil
}

func decodeRestore(encoded string, limit int) ([]byte, error) {
	if len(encoded) > base64.StdEncoding.EncodedLen(limit) {
		return nil, errors.New("restore archive exceeds 49 MiB")
	}
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || len(data) == 0 || len(data) > limit {
		return nil, errors.New("invalid or oversized restore file")
	}
	return data, nil
}
