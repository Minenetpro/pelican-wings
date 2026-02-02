package backup

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"emperror.dev/errors"

	"github.com/Minenetpro/pelican-wings/config"
	"github.com/Minenetpro/pelican-wings/remote"
	"github.com/Minenetpro/pelican-wings/server/filesystem"
)

type ResticBackup struct {
	Backup
}

var _ BackupInterface = (*ResticBackup)(nil)

// repoInitMu protects repository initialization to prevent concurrent init attempts.
var repoInitMu sync.Mutex

// resticSnapshot represents a snapshot from restic snapshots output.
type resticSnapshot struct {
	ID       string   `json:"id"`
	ShortID  string   `json:"short_id"`
	Time     string   `json:"time"`
	Hostname string   `json:"hostname"`
	Tags     []string `json:"tags"`
	Paths    []string `json:"paths"`
}

func NewRestic(client remote.Client, uuid string, suuid string, ignore string) *ResticBackup {
	return &ResticBackup{
		Backup{
			client:     client,
			Uuid:       uuid,
			ServerUuid: suuid,
			Ignore:     ignore,
			adapter:    ResticBackupAdapter,
		},
	}
}

// WithLogContext attaches additional context to the log output for this backup.
func (r *ResticBackup) WithLogContext(c map[string]interface{}) {
	r.logContext = c
}

// SkipPanelNotification returns true as restic backups are managed externally.
func (r *ResticBackup) SkipPanelNotification() bool {
	return true
}

// Remove removes a backup snapshot from the restic repository.
func (r *ResticBackup) Remove() error {
	ctx := context.Background()
	cfg := config.Get().System.Backups.Restic

	r.log().Info("removing backup snapshot from restic repository")

	// Find the snapshot ID first
	snapshotID, err := r.findSnapshotByTag(ctx)
	if err != nil {
		return errors.Wrap(err, "backup: failed to find snapshot to remove")
	}

	if snapshotID == "" {
		r.log().Warn("no snapshot found with the specified backup_uuid, nothing to remove")
		return nil
	}

	// Build forget command arguments with specific snapshot ID
	args := []string{"forget", snapshotID, "--prune"}

	// Add cache directory if configured
	if cfg.CacheDir != "" {
		args = append(args, "--cache-dir", cfg.CacheDir)
	}

	// Use forget with specific snapshot ID to remove only this snapshot
	// The --prune flag removes unreferenced data from the repository
	_, err = r.runRestic(ctx, args...)
	if err != nil {
		return errors.Wrap(err, "backup: failed to remove restic snapshot")
	}

	r.log().Info("successfully removed backup snapshot from restic repository")
	return nil
}

// Generate creates a backup of the server's files using restic.
func (r *ResticBackup) Generate(ctx context.Context, _ *filesystem.Filesystem, ignore string) (*ArchiveDetails, error) {
	cfg := config.Get().System.Backups.Restic

	// Build the source path for this server's data
	sourcePath := filepath.Join(config.Get().System.Data, r.ServerUuid)

	r.log().WithField("path", sourcePath).Info("creating restic backup for server")

	// Ensure the repository exists (auto-init if needed)
	if err := r.ensureRepository(ctx); err != nil {
		return nil, errors.Wrap(err, "backup: failed to ensure restic repository exists")
	}

	// Build restic backup command arguments
	args := []string{
		"backup",
		"--host", r.ServerUuid,
		"--tag", r.backupTag(),
		"--tag", r.serverTag(),
	}

	// Add exclusion patterns if provided
	if ignore != "" {
		for _, pattern := range strings.Split(ignore, "\n") {
			pattern = strings.TrimSpace(pattern)
			if pattern != "" && !strings.HasPrefix(pattern, "#") {
				args = append(args, "--exclude", pattern)
			}
		}
	}

	// Add cache directory if configured
	if cfg.CacheDir != "" {
		args = append(args, "--cache-dir", cfg.CacheDir)
	}

	// Add the source path
	args = append(args, sourcePath)

	// Execute the backup
	output, err := r.runRestic(ctx, args...)
	if err != nil {
		return nil, errors.Wrap(err, "backup: failed to create restic backup")
	}

	r.log().WithField("output", string(output)).Debug("restic backup output")
	r.log().Info("successfully created restic backup")

	return &ArchiveDetails{
		Checksum:     "",
		ChecksumType: "none",
		Size:         0,
		Parts:        nil,
	}, nil
}

// Restore restores a backup from the restic repository to the server's data directory.
func (r *ResticBackup) Restore(ctx context.Context, _ io.Reader, callback RestoreCallback) error {
	cfg := config.Get().System.Backups.Restic

	// Find the snapshot ID for this backup_uuid
	snapshotID, err := r.findSnapshotByTag(ctx)
	if err != nil {
		return errors.Wrap(err, "backup: failed to find restic snapshot")
	}

	if snapshotID == "" {
		return errors.New("backup: no snapshot found with the specified backup_uuid")
	}

	targetPath := filepath.Join(config.Get().System.Data, r.ServerUuid)

	r.log().WithField("snapshot", snapshotID).WithField("target", targetPath).Info("restoring restic backup")

	// Build restore command
	args := []string{
		"restore",
		snapshotID,
		"--target", "/",
	}

	// Add cache directory if configured
	if cfg.CacheDir != "" {
		args = append(args, "--cache-dir", cfg.CacheDir)
	}

	// Execute the restore
	output, err := r.runRestic(ctx, args...)
	if err != nil {
		return errors.Wrap(err, "backup: failed to restore restic backup")
	}

	r.log().WithField("output", string(output)).Debug("restic restore output")
	r.log().Info("successfully restored restic backup")

	return nil
}

// Path returns an empty string as restic backups are not stored locally.
func (r *ResticBackup) Path() string {
	return ""
}

// Checksum returns nil as restic handles checksums internally.
func (r *ResticBackup) Checksum() ([]byte, error) {
	return nil, nil
}

// Size returns 0 as the size is not tracked locally for restic backups.
func (r *ResticBackup) Size() (int64, error) {
	return 0, nil
}

// Details returns minimal archive details for restic backups.
func (r *ResticBackup) Details(ctx context.Context, parts []remote.BackupPart) (*ArchiveDetails, error) {
	return &ArchiveDetails{
		Checksum:     "",
		ChecksumType: "none",
		Size:         0,
		Parts:        parts,
	}, nil
}

// backupTag returns the tag used to identify this specific backup.
func (r *ResticBackup) backupTag() string {
	return fmt.Sprintf("backup_uuid:%s", r.Uuid)
}

// serverTag returns the tag used to identify the server.
func (r *ResticBackup) serverTag() string {
	return fmt.Sprintf("server_uuid:%s", r.ServerUuid)
}

// buildEnv builds the environment variables for restic commands.
func (r *ResticBackup) buildEnv() []string {
	cfg := config.Get().System.Backups.Restic

	env := os.Environ()
	env = append(env,
		fmt.Sprintf("RESTIC_REPOSITORY=%s", cfg.Repository),
		fmt.Sprintf("RESTIC_PASSWORD=%s", cfg.Password),
	)

	if cfg.AWSAccessKeyID != "" {
		env = append(env, fmt.Sprintf("AWS_ACCESS_KEY_ID=%s", cfg.AWSAccessKeyID))
	}
	if cfg.AWSSecretAccessKey != "" {
		env = append(env, fmt.Sprintf("AWS_SECRET_ACCESS_KEY=%s", cfg.AWSSecretAccessKey))
	}
	if cfg.AWSRegion != "" {
		env = append(env, fmt.Sprintf("AWS_DEFAULT_REGION=%s", cfg.AWSRegion))
	}

	return env
}

// runRestic executes a restic command with the appropriate environment.
func (r *ResticBackup) runRestic(ctx context.Context, args ...string) ([]byte, error) {
	cfg := config.Get().System.Backups.Restic

	cmd := exec.CommandContext(ctx, cfg.BinaryPath, args...)
	cmd.Env = r.buildEnv()

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	r.log().WithField("command", fmt.Sprintf("%s %s", cfg.BinaryPath, strings.Join(args, " "))).Debug("executing restic command")

	if err := cmd.Run(); err != nil {
		r.log().WithField("stderr", stderr.String()).WithField("stdout", stdout.String()).Error("restic command failed")
		return nil, errors.Wrap(err, stderr.String())
	}

	return stdout.Bytes(), nil
}

// ensureRepository checks if the repository exists and initializes it if needed.
func (r *ResticBackup) ensureRepository(ctx context.Context) error {
	cfg := config.Get().System.Backups.Restic

	// Use mutex to prevent concurrent initialization attempts
	repoInitMu.Lock()
	defer repoInitMu.Unlock()

	// Try to list snapshots to check if repo exists
	args := []string{"snapshots", "--json"}
	if cfg.CacheDir != "" {
		args = append(args, "--cache-dir", cfg.CacheDir)
	}

	_, err := r.runRestic(ctx, args...)
	if err == nil {
		// Repository exists
		return nil
	}

	// Check if the error indicates the repository doesn't exist
	errStr := err.Error()
	if !strings.Contains(errStr, "repository does not exist") &&
		!strings.Contains(errStr, "Is there a repository at") &&
		!strings.Contains(errStr, "unable to open config file") {
		// Some other error occurred
		return err
	}

	r.log().Info("restic repository does not exist, initializing...")

	// Initialize the repository
	initArgs := []string{"init"}
	if cfg.CacheDir != "" {
		initArgs = append(initArgs, "--cache-dir", cfg.CacheDir)
	}

	_, err = r.runRestic(ctx, initArgs...)
	if err != nil {
		return errors.Wrap(err, "backup: failed to initialize restic repository")
	}

	r.log().Info("successfully initialized restic repository")
	return nil
}

// SnapshotInfo represents snapshot data for API responses.
type SnapshotInfo struct {
	ID         string   `json:"id"`
	ShortID    string   `json:"short_id"`
	Time       string   `json:"time"`
	BackupUUID string   `json:"backup_uuid"`
	ServerUUID string   `json:"server_uuid"`
	Paths      []string `json:"paths"`
}

// parseSnapshotToInfo converts a resticSnapshot to SnapshotInfo, extracting UUIDs from tags.
func parseSnapshotToInfo(snapshot resticSnapshot) SnapshotInfo {
	info := SnapshotInfo{
		ID:      snapshot.ID,
		ShortID: snapshot.ShortID,
		Time:    snapshot.Time,
		Paths:   snapshot.Paths,
	}
	for _, tag := range snapshot.Tags {
		if strings.HasPrefix(tag, "backup_uuid:") {
			info.BackupUUID = strings.TrimPrefix(tag, "backup_uuid:")
		}
		if strings.HasPrefix(tag, "server_uuid:") {
			info.ServerUUID = strings.TrimPrefix(tag, "server_uuid:")
		}
	}
	return info
}

// ListSnapshots returns all snapshots for this server from the restic repository.
func (r *ResticBackup) ListSnapshots(ctx context.Context) ([]SnapshotInfo, error) {
	cfg := config.Get().System.Backups.Restic

	args := []string{"snapshots", "--json", "--tag", r.serverTag()}
	if cfg.CacheDir != "" {
		args = append(args, "--cache-dir", cfg.CacheDir)
	}

	output, err := r.runRestic(ctx, args...)
	if err != nil {
		return nil, errors.Wrap(err, "backup: failed to list restic snapshots")
	}

	var snapshots []resticSnapshot
	if err := json.Unmarshal(output, &snapshots); err != nil {
		return nil, errors.Wrap(err, "backup: failed to parse restic snapshots output")
	}

	result := make([]SnapshotInfo, 0, len(snapshots))
	for _, s := range snapshots {
		result = append(result, parseSnapshotToInfo(s))
	}

	return result, nil
}

// GetSnapshotStatus checks if a snapshot exists for this backup and returns its info.
func (r *ResticBackup) GetSnapshotStatus(ctx context.Context) (*SnapshotInfo, error) {
	cfg := config.Get().System.Backups.Restic

	args := []string{"snapshots", "--json", "--tag", r.backupTag()}
	if cfg.CacheDir != "" {
		args = append(args, "--cache-dir", cfg.CacheDir)
	}

	output, err := r.runRestic(ctx, args...)
	if err != nil {
		return nil, errors.Wrap(err, "backup: failed to get restic snapshot status")
	}

	var snapshots []resticSnapshot
	if err := json.Unmarshal(output, &snapshots); err != nil {
		return nil, errors.Wrap(err, "backup: failed to parse restic snapshots output")
	}

	if len(snapshots) == 0 {
		return nil, nil
	}

	info := parseSnapshotToInfo(snapshots[0])
	return &info, nil
}

// findSnapshotByTag finds a snapshot ID by its backup_uuid tag.
func (r *ResticBackup) findSnapshotByTag(ctx context.Context) (string, error) {
	cfg := config.Get().System.Backups.Restic

	args := []string{"snapshots", "--json", "--tag", r.backupTag()}
	if cfg.CacheDir != "" {
		args = append(args, "--cache-dir", cfg.CacheDir)
	}

	output, err := r.runRestic(ctx, args...)
	if err != nil {
		return "", err
	}

	var snapshots []resticSnapshot
	if err := json.Unmarshal(output, &snapshots); err != nil {
		return "", errors.Wrap(err, "backup: failed to parse restic snapshots output")
	}

	if len(snapshots) == 0 {
		return "", nil
	}

	// Return the first (and should be only) matching snapshot
	return snapshots[0].ID, nil
}
