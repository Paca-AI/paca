package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/Paca-AI/api/internal/config"
	pluginrt "github.com/Paca-AI/api/internal/platform/plugin"
	"github.com/Paca-AI/api/internal/platform/storage"
)

// newStorageClient builds the object-storage client from cfg.Storage. It is
// shared by the server and the migrate-plugins command so both talk to the
// bucket the same way.
func newStorageClient(ctx context.Context, cfg *config.Config) (*storage.S3Client, error) {
	return storage.NewS3Client(ctx, storage.S3Config{
		Endpoint:        cfg.Storage.Endpoint,
		PublicURL:       cfg.Storage.PublicURL,
		Region:          cfg.Storage.Region,
		Bucket:          cfg.Storage.Bucket,
		AccessKeyID:     cfg.Storage.AccessKeyID,
		SecretAccessKey: cfg.Storage.SecretAccessKey,
		UseSSL:          cfg.Storage.UseSSL,
		ForcePathStyle:  cfg.Storage.Provider != "s3", // self-hosted S3-compatible stores require path-style
	})
}

// MigratePluginsToBucket copies the plugins installed in the local
// PLUGINS_*_DIR folders into the storage bucket under PLUGINS_S3_PREFIX, for
// switching PLUGINS_STORE from local to s3. It reads the same configuration as
// the server and prints a per-plugin report to out. The local folders are not
// modified, so the switch can be rolled back by setting PLUGINS_STORE=local.
func MigratePluginsToBucket(ctx context.Context, cfg *config.Config, dryRun bool, out io.Writer) error {
	client, err := newStorageClient(ctx, cfg)
	if err != nil {
		return fmt.Errorf("storage client: %w", err)
	}
	if cfg.Storage.Provider != "s3" && !dryRun {
		if err := client.EnsureBucket(ctx, cfg.Storage.Bucket); err != nil {
			return fmt.Errorf("ensure storage bucket: %w", err)
		}
	}
	dst, err := pluginrt.NewStore(ctx, pluginrt.StoreConfig{
		Store:    "s3",
		S3Bucket: cfg.Storage.Bucket,
		S3Prefix: cfg.Plugins.S3Prefix,
		S3Region: cfg.Storage.Region,
		S3Client: client.Raw(),
	})
	if err != nil {
		return err
	}
	src := pluginrt.StoreConfig{
		Store:       "local",
		WASMDir:     cfg.Plugins.WASMDir,
		FrontendDir: cfg.Plugins.FrontendDir,
		MCPDir:      cfg.Plugins.MCPDir,
		SkillsDir:   cfg.Plugins.SkillsDir,
	}

	report, migrateErr := pluginrt.MigrateLocalToStore(ctx, src, dst, dryRun)

	verb := "copied"
	if dryRun {
		verb = "would copy"
	}
	for _, p := range report.Plugins {
		if p.Err != nil {
			_, _ = fmt.Fprintf(out, "FAIL  %s: %v\n", p.Name, p.Err)
			continue
		}
		_, _ = fmt.Fprintf(out, "ok    %s: %s %v\n", p.Name, verb, p.Parts)
	}
	for _, s := range report.Skipped {
		_, _ = fmt.Fprintf(out, "skip  %s: not a valid plugin name\n", s)
	}
	_, _ = fmt.Fprintf(out, "%d plugin(s) in the local folders; bucket %q, prefix %q\n", len(report.Plugins), cfg.Storage.Bucket, cfg.Plugins.S3Prefix)
	if migrateErr != nil {
		return errors.New("some plugins failed to migrate (see above); fix and re-run, it is safe to repeat")
	}
	return nil
}
