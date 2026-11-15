package service

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/pkg/storage"
)

// backupDumper writes a database dump in pg_dump custom format to w.
type backupDumper interface {
	Dump(ctx context.Context, w io.Writer) error
}

// pgDumper runs pg_dump. The password travels in PGPASSWORD, never in the
// command line, and pg_dump's error output is returned for sanitizing.
type pgDumper struct {
	databaseURL string
}

func (d pgDumper) Dump(ctx context.Context, w io.Writer) error {
	conn, password, err := splitDatabasePassword(d.databaseURL)
	if err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, "pg_dump", "--format=custom", "--no-owner", "--dbname", conn)
	cmd.Env = append(os.Environ(), "PGPASSWORD="+password)
	cmd.Stdout = w
	var stderr limitedBuffer
	stderr.limit = 4096
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		message := strings.TrimSpace(stderr.String())
		if message == "" {
			return fmt.Errorf("pg_dump: %w", err)
		}
		return fmt.Errorf("pg_dump: %w: %s", err, message)
	}
	return nil
}

type limitedBuffer struct {
	bytes.Buffer
	limit int
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if room := b.limit - b.Len(); room > 0 {
		if len(p) > room {
			b.Buffer.Write(p[:room])
		} else {
			b.Buffer.Write(p)
		}
	}
	return len(p), nil
}

// BackupUploads describes where uploaded files live: a local directory, or
// an object store listed into a manifest.
type BackupUploads struct {
	LocalDir string
	Lister   storage.Lister
}

type archiveResult struct {
	Path     string
	Size     int64
	Checksum string
}

// writeBackupArchive writes the archive for run into dir and returns its
// final path, size and SHA-256. It writes to a .partial file, syncs it and
// renames it into place, then writes the <file>.sha256 sidecar; on error
// nothing is left behind.
func (s *BackupService) writeBackupArchive(ctx context.Context, run *domain.BackupRun, passphrase string) (archiveResult, error) {
	if err := os.MkdirAll(s.cfg.Dir, 0o700); err != nil {
		return archiveResult{}, fmt.Errorf("create backup directory: %w", err)
	}
	name := run.StartedAt.UTC().Format("20060102T150405Z") + "-" + run.ID.String() + ".tar.gz"
	if passphrase != "" {
		name += ".enc"
	}
	final := filepath.Join(s.cfg.Dir, name)
	partial := final + ".partial"
	dbTemp := final + ".database.partial"
	defer os.Remove(dbTemp)

	// The dump goes to a temporary file first: a tar entry needs its size.
	dbSize, dbSum, err := s.dumpDatabase(ctx, dbTemp)
	if err != nil {
		return archiveResult{}, err
	}

	file, err := os.OpenFile(partial, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return archiveResult{}, fmt.Errorf("create backup file: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			file.Close()
			os.Remove(partial)
		}
	}()
	hasher := sha256.New()
	counter := &countingWriter{}
	var sink io.Writer = io.MultiWriter(file, hasher, counter)
	var encrypter io.WriteCloser
	if passphrase != "" {
		if encrypter, err = newBackupEncryptWriter(sink, passphrase); err != nil {
			return archiveResult{}, fmt.Errorf("start encryption: %w", err)
		}
		sink = encrypter
	}
	gz := gzip.NewWriter(sink)
	tw := tar.NewWriter(gz)

	if err := addTarFile(tw, domain.BackupDatabaseFile, dbTemp, dbSize, run.StartedAt); err != nil {
		return archiveResult{}, err
	}
	uploads, err := s.addUploads(ctx, tw, run.StartedAt)
	if err != nil {
		return archiveResult{}, err
	}
	manifest := domain.BackupManifest{
		FormatVersion: domain.BackupFormatVersion,
		CreatedAt:     run.StartedAt.UTC(),
		Kind:          run.Kind,
		AppVersion:    s.cfg.AppVersion,
		Database:      domain.BackupManifestEntry{File: domain.BackupDatabaseFile, Size: dbSize, SHA256: dbSum},
		Uploads:       uploads,
	}
	manifestJSON, _ := json.MarshalIndent(manifest, "", "  ")
	if err := addTarBytes(tw, domain.BackupManifestFile, manifestJSON, run.StartedAt); err != nil {
		return archiveResult{}, err
	}
	if err := tw.Close(); err != nil {
		return archiveResult{}, err
	}
	if err := gz.Close(); err != nil {
		return archiveResult{}, err
	}
	if encrypter != nil {
		if err := encrypter.Close(); err != nil {
			return archiveResult{}, err
		}
	}
	if err := ctx.Err(); err != nil {
		return archiveResult{}, err
	}
	if err := file.Sync(); err != nil {
		return archiveResult{}, err
	}
	if err := file.Close(); err != nil {
		return archiveResult{}, err
	}
	if err := os.Rename(partial, final); err != nil {
		return archiveResult{}, err
	}
	committed = true
	checksum := hex.EncodeToString(hasher.Sum(nil))
	if err := os.WriteFile(final+".sha256", []byte(checksum+"  "+name+"\n"), 0o600); err != nil {
		os.Remove(final)
		return archiveResult{}, fmt.Errorf("write checksum file: %w", err)
	}
	syncDir(s.cfg.Dir)
	return archiveResult{Path: final, Size: counter.n, Checksum: checksum}, nil
}

func (s *BackupService) dumpDatabase(ctx context.Context, path string) (int64, string, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return 0, "", fmt.Errorf("create dump file: %w", err)
	}
	defer file.Close()
	hasher := sha256.New()
	counter := &countingWriter{}
	if err := s.dumper.Dump(ctx, io.MultiWriter(file, hasher, counter)); err != nil {
		return 0, "", err
	}
	if err := file.Close(); err != nil {
		return 0, "", err
	}
	return counter.n, hex.EncodeToString(hasher.Sum(nil)), nil
}

// addUploads adds the uploaded files under uploads/, or the object list as
// uploads-manifest.json when uploads live in object storage. Symlinks and
// other non-regular files are skipped.
func (s *BackupService) addUploads(ctx context.Context, tw *tar.Writer, at time.Time) (domain.BackupManifestUploads, error) {
	if s.uploads.Lister != nil {
		objects, err := s.uploads.Lister.List(ctx, "")
		if err != nil {
			return domain.BackupManifestUploads{}, fmt.Errorf("list uploads: %w", err)
		}
		list := make([]domain.BackupUploadObject, 0, len(objects))
		var total int64
		for _, object := range objects {
			list = append(list, domain.BackupUploadObject{Key: object.Key, Size: object.Size, ETag: object.ETag})
			total += object.Size
		}
		body, _ := json.MarshalIndent(list, "", "  ")
		if err := addTarBytes(tw, domain.BackupUploadsManifestFile, body, at); err != nil {
			return domain.BackupManifestUploads{}, err
		}
		return domain.BackupManifestUploads{Mode: domain.BackupUploadsModeS3Manifest, Count: len(list), Bytes: total, File: domain.BackupUploadsManifestFile}, nil
	}
	result := domain.BackupManifestUploads{Mode: domain.BackupUploadsModeFiles}
	root := s.uploads.LocalDir
	if root == "" {
		return result, nil
	}
	if _, err := os.Stat(root); errors.Is(err, fs.ErrNotExist) {
		return result, nil
	}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		if !entry.Type().IsRegular() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if err := addTarFile(tw, domain.BackupUploadsDir+filepath.ToSlash(rel), path, info.Size(), info.ModTime()); err != nil {
			return err
		}
		result.Count++
		result.Bytes += info.Size()
		return nil
	})
	if err != nil {
		return result, fmt.Errorf("archive uploads: %w", err)
	}
	return result, nil
}

func addTarFile(tw *tar.Writer, name, path string, size int64, modTime time.Time) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o600, Size: size, ModTime: modTime.UTC(), Typeflag: tar.TypeReg, Format: tar.FormatPAX}); err != nil {
		return err
	}
	// A file that grew while being archived is cut at the size recorded.
	copied, err := io.Copy(tw, io.LimitReader(file, size))
	if err != nil {
		return err
	}
	if copied != size {
		return fmt.Errorf("%s changed size while being archived", name)
	}
	return nil
}

func addTarBytes(tw *tar.Writer, name string, body []byte, modTime time.Time) error {
	if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o600, Size: int64(len(body)), ModTime: modTime.UTC(), Typeflag: tar.TypeReg, Format: tar.FormatPAX}); err != nil {
		return err
	}
	_, err := tw.Write(body)
	return err
}

type countingWriter struct{ n int64 }

func (c *countingWriter) Write(p []byte) (int, error) {
	c.n += int64(len(p))
	return len(p), nil
}

func syncDir(dir string) {
	if d, err := os.Open(dir); err == nil {
		_ = d.Sync()
		_ = d.Close()
	}
}

// pgDumpMagic starts every pg_dump custom-format file.
var pgDumpMagic = []byte("PGDMP")

// verifyBackupArchive reads a whole archive from r and checks that its
// SHA-256 matches checksum, that it decrypts (when passphrase is set) and
// decompresses, that manifest.json is present, and that database.dump is a
// pg_dump custom-format file whose size and SHA-256 match the manifest.
func verifyBackupArchive(r io.Reader, checksum, passphrase string) error {
	hasher := sha256.New()
	raw := io.TeeReader(r, hasher)
	var plain io.Reader = raw
	if passphrase != "" {
		decrypted, err := newBackupDecryptReader(raw, passphrase)
		if err != nil {
			return err
		}
		plain = decrypted
	}
	gz, err := gzip.NewReader(plain)
	if err != nil {
		return fmt.Errorf("not a gzip archive: %w", err)
	}
	tr := tar.NewReader(gz)
	var manifest *domain.BackupManifest
	var dbSize int64
	var dbSum string
	dbSeen, uploadsManifestSeen := false, false
	uploadFiles := 0
	for {
		header, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("reading the archive: %w", err)
		}
		switch {
		case header.Name == domain.BackupManifestFile:
			var m domain.BackupManifest
			if err := json.NewDecoder(io.LimitReader(tr, 1<<20)).Decode(&m); err != nil {
				return fmt.Errorf("manifest.json is unreadable: %w", err)
			}
			manifest = &m
		case header.Name == domain.BackupDatabaseFile:
			dbSeen = true
			h := sha256.New()
			head := &prefixCapture{want: len(pgDumpMagic)}
			n, err := io.Copy(io.MultiWriter(h, head), tr)
			if err != nil {
				return fmt.Errorf("reading database.dump: %w", err)
			}
			if !bytes.Equal(head.buf, pgDumpMagic) {
				return errors.New("database.dump is not a pg_dump custom-format file")
			}
			dbSize, dbSum = n, hex.EncodeToString(h.Sum(nil))
		case header.Name == domain.BackupUploadsManifestFile:
			uploadsManifestSeen = true
			var objects []domain.BackupUploadObject
			if err := json.NewDecoder(tr).Decode(&objects); err != nil {
				return fmt.Errorf("uploads-manifest.json is unreadable: %w", err)
			}
		case strings.HasPrefix(header.Name, domain.BackupUploadsDir) && header.Typeflag == tar.TypeReg:
			uploadFiles++
			if _, err := io.Copy(io.Discard, tr); err != nil {
				return fmt.Errorf("reading %s: %w", header.Name, err)
			}
		default:
			if _, err := io.Copy(io.Discard, tr); err != nil {
				return fmt.Errorf("reading %s: %w", header.Name, err)
			}
		}
	}
	// Drain gzip (checks its CRC and trailer) and anything after it.
	if _, err := io.Copy(io.Discard, gz); err != nil {
		return fmt.Errorf("reading the archive: %w", err)
	}
	if _, err := io.Copy(io.Discard, plain); err != nil {
		return fmt.Errorf("reading the archive: %w", err)
	}
	if got := hex.EncodeToString(hasher.Sum(nil)); checksum != "" && got != checksum {
		return errors.New("checksum mismatch: the file differs from the one recorded")
	}
	switch {
	case manifest == nil:
		return errors.New("manifest.json is missing")
	case !dbSeen:
		return errors.New("database.dump is missing")
	case manifest.Database.Size != dbSize || manifest.Database.SHA256 != dbSum:
		return errors.New("database.dump does not match the manifest")
	case manifest.Uploads.Mode == domain.BackupUploadsModeFiles && manifest.Uploads.Count != uploadFiles:
		return fmt.Errorf("the archive holds %d uploaded files, the manifest lists %d", uploadFiles, manifest.Uploads.Count)
	case manifest.Uploads.Mode == domain.BackupUploadsModeS3Manifest && !uploadsManifestSeen:
		return errors.New("uploads-manifest.json is missing")
	}
	return nil
}

type prefixCapture struct {
	want int
	buf  []byte
}

func (p *prefixCapture) Write(b []byte) (int, error) {
	if room := p.want - len(p.buf); room > 0 {
		if len(b) < room {
			room = len(b)
		}
		p.buf = append(p.buf, b[:room]...)
	}
	return len(b), nil
}
