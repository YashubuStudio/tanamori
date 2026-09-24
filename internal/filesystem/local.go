package filesystem

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"mime"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/YashubuStudio/tanamori/internal/app"
	"github.com/YashubuStudio/tanamori/internal/domain"
)

var ErrDestinationExists = errors.New("destination already exists")

type Local struct {
	// MaxFileBytes is a server-side safety limit. Zero keeps legacy CLI behavior.
	MaxFileBytes int64
}

func (local Local) Inspect(ctx context.Context, path string) (domain.FileObservation, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return domain.FileObservation{}, fmt.Errorf("resolve file path: %w", err)
	}
	info, err := os.Lstat(absolute)
	if err != nil {
		return domain.FileObservation{}, fmt.Errorf("inspect file: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return domain.FileObservation{}, fmt.Errorf("symbolic links are not followed: %s", absolute)
	}
	if !info.Mode().IsRegular() {
		return domain.FileObservation{}, fmt.Errorf("not a regular file: %s", absolute)
	}
	if local.MaxFileBytes > 0 && info.Size() > local.MaxFileBytes {
		return domain.FileObservation{}, fmt.Errorf("file exceeds %d byte server limit: %s", local.MaxFileBytes, absolute)
	}
	hash, err := hashFile(ctx, absolute)
	if err != nil {
		return domain.FileObservation{}, err
	}
	after, err := os.Lstat(absolute)
	if err != nil {
		return domain.FileObservation{}, fmt.Errorf("inspect after hashing: %w", err)
	}
	if !after.Mode().IsRegular() || after.Size() != info.Size() || !after.ModTime().Equal(info.ModTime()) {
		return domain.FileObservation{}, fmt.Errorf("file changed during hashing: %s", absolute)
	}
	return domain.FileObservation{
		Path: filepath.Clean(absolute), Name: info.Name(), SHA256: hash, Size: info.Size(),
		MIME: mime.TypeByExtension(strings.ToLower(filepath.Ext(info.Name()))), ModifiedAt: info.ModTime().UTC(),
	}, nil
}

func (Local) Move(ctx context.Context, sourcePath, destinationDirectory string, choices ...app.MoveOptions) (app.MoveResult, error) {
	choice := app.MoveOptions{OnConflict: "rename", RenamePattern: domain.DefaultSettings().RenamePattern}
	if len(choices) > 0 {
		choice = choices[0]
	}
	if choice.OnConflict != "rename" && choice.OnConflict != "overwrite" {
		return app.MoveResult{}, fmt.Errorf("on_conflict must be rename or overwrite")
	}
	source, err := filepath.Abs(sourcePath)
	if err != nil {
		return app.MoveResult{}, fmt.Errorf("resolve source: %w", err)
	}
	destinationDirectory, err = filepath.Abs(destinationDirectory)
	if err != nil {
		return app.MoveResult{}, fmt.Errorf("resolve destination: %w", err)
	}
	info, err := os.Stat(destinationDirectory)
	if err != nil {
		return app.MoveResult{}, fmt.Errorf("inspect destination: %w", err)
	}
	if !info.IsDir() {
		return app.MoveResult{}, fmt.Errorf("destination is not a directory: %s", destinationDirectory)
	}
	destination := filepath.Join(destinationDirectory, filepath.Base(source))
	if filepath.Clean(source) == filepath.Clean(destination) {
		hash, hashErr := hashFile(ctx, source)
		return app.MoveResult{FromPath: source, ToPath: destination, SHA256: hash}, hashErr
	}
	_, statErr := os.Lstat(destination)
	if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
		return app.MoveResult{}, fmt.Errorf("inspect destination file: %w", statErr)
	}
	if statErr == nil && choice.OnConflict == "rename" {
		destination, err = numberedDestination(destinationDirectory, filepath.Base(source), choice.RenamePattern)
		if err != nil {
			return app.MoveResult{}, err
		}
	}
	expectedHash, err := hashFile(ctx, source)
	if err != nil {
		return app.MoveResult{}, err
	}
	backup := ""
	if statErr == nil && choice.OnConflict == "overwrite" {
		oldInfo, err := os.Lstat(destination)
		if err != nil {
			return app.MoveResult{}, err
		}
		if !oldInfo.Mode().IsRegular() {
			return app.MoveResult{}, fmt.Errorf("destination is not a regular file")
		}
		old, createErr := os.CreateTemp(destinationDirectory, ".tanamori-backup-*")
		if createErr != nil {
			return app.MoveResult{}, createErr
		}
		backup = old.Name()
		_ = old.Close()
		if err := os.Remove(backup); err != nil {
			return app.MoveResult{}, err
		}
		if err := os.Rename(destination, backup); err != nil {
			return app.MoveResult{}, fmt.Errorf("stage overwritten file: %w", err)
		}
	}
	moveErr := moveNoReplace(ctx, source, destination, expectedHash)
	if moveErr != nil {
		if backup != "" {
			if _, err := os.Lstat(destination); errors.Is(err, os.ErrNotExist) {
				if restoreErr := os.Rename(backup, destination); restoreErr != nil {
					return app.MoveResult{}, fmt.Errorf("%w; restore original from %s: %v", moveErr, backup, restoreErr)
				}
			} else {
				return app.MoveResult{}, fmt.Errorf("%w; original retained at %s", moveErr, backup)
			}
		}
		return app.MoveResult{}, moveErr
	}
	if backup != "" {
		_ = os.Remove(backup)
	}
	return app.MoveResult{FromPath: source, ToPath: destination, SHA256: expectedHash}, nil
}

func numberedDestination(directory, original, pattern string) (string, error) {
	extension := filepath.Ext(original)
	stem := strings.TrimSuffix(original, extension)
	for number := 1; number <= 10000; number++ {
		name := strings.NewReplacer("{name}", stem, "{ext}", extension, "{n}", strconv.Itoa(number)).Replace(pattern)
		if !filepath.IsLocal(name) || filepath.Base(name) != name || len(name) > 255 || strings.HasSuffix(name, ".") || strings.HasSuffix(name, " ") {
			return "", fmt.Errorf("rename pattern produced invalid filename")
		}
		path := filepath.Join(directory, name)
		if _, err := os.Lstat(path); errors.Is(err, os.ErrNotExist) {
			return path, nil
		} else if err != nil {
			return "", err
		}
	}
	return "", fmt.Errorf("no available numbered filename")
}

func moveNoReplace(ctx context.Context, source, destination, expectedHash string) error {
	if sameVolume(source, destination) {
		if err := os.Link(source, destination); err == nil {
			actualHash, err := hashFile(ctx, destination)
			if err != nil || actualHash != expectedHash {
				_ = os.Remove(destination)
				return fmt.Errorf("linked file hash mismatch: %v", err)
			}
			if err := os.Remove(source); err != nil {
				_ = os.Remove(destination)
				return fmt.Errorf("source removal failed: %w", err)
			}
			return nil
		} else if _, statErr := os.Lstat(destination); statErr == nil {
			return fmt.Errorf("%w: %s", ErrDestinationExists, destination)
		}
	}
	return copyAcrossVolumes(ctx, source, destination, expectedHash)
}

func sameVolume(left, right string) bool {
	return strings.EqualFold(filepath.VolumeName(left), filepath.VolumeName(right))
}

func copyAcrossVolumes(ctx context.Context, source, destination, expectedHash string) error {
	input, err := os.Open(source)
	if err != nil {
		return fmt.Errorf("open source: %w", err)
	}
	defer input.Close()
	info, err := input.Stat()
	if err != nil {
		return fmt.Errorf("stat source: %w", err)
	}
	temporary, err := os.CreateTemp(filepath.Dir(destination), ".tanamori-moving-*")
	if err != nil {
		return fmt.Errorf("create temporary destination: %w", err)
	}
	temporaryPath := temporary.Name()
	committed := false
	defer func() {
		temporary.Close()
		if !committed {
			_ = os.Remove(temporaryPath)
		}
	}()
	if _, err := copyWithContext(ctx, temporary, input); err != nil {
		return fmt.Errorf("copy file: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		return fmt.Errorf("sync copied file: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close copied file: %w", err)
	}
	if err := os.Chmod(temporaryPath, info.Mode().Perm()); err != nil {
		return fmt.Errorf("preserve file mode: %w", err)
	}
	actualHash, err := hashFile(ctx, temporaryPath)
	if err != nil {
		return err
	}
	if actualHash != expectedHash {
		return fmt.Errorf("copied file hash mismatch")
	}
	if err := commitNoReplace(ctx, temporaryPath, destination); err != nil {
		return fmt.Errorf("commit copied file: %w", err)
	}
	committedHash, err := hashFile(ctx, destination)
	if err != nil || committedHash != expectedHash {
		_ = os.Remove(destination)
		return fmt.Errorf("committed file hash mismatch: %v", err)
	}
	committed = true
	if err := os.Remove(source); err != nil {
		return fmt.Errorf("destination verified at %s but source removal failed: %w", destination, err)
	}
	return nil
}

func commitNoReplace(ctx context.Context, source, destination string) error {
	if err := os.Link(source, destination); err == nil {
		_ = os.Remove(source)
		return nil
	} else if _, statErr := os.Lstat(destination); statErr == nil {
		return fmt.Errorf("%w: %s", ErrDestinationExists, destination)
	}
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		_ = output.Close()
		if !committed {
			_ = os.Remove(destination)
		}
	}()
	if _, err := copyWithContext(ctx, output, input); err != nil {
		return err
	}
	if err := output.Sync(); err != nil {
		return err
	}
	if err := output.Close(); err != nil {
		return err
	}
	committed = true
	_ = os.Remove(source)
	return nil
}

func copyWithContext(ctx context.Context, destination io.Writer, source io.Reader) (int64, error) {
	buffer := make([]byte, 128*1024)
	var total int64
	for {
		if err := ctx.Err(); err != nil {
			return total, err
		}
		read, readErr := source.Read(buffer)
		if read > 0 {
			written, writeErr := destination.Write(buffer[:read])
			total += int64(written)
			if writeErr != nil {
				return total, writeErr
			}
			if written != read {
				return total, io.ErrShortWrite
			}
		}
		if readErr == io.EOF {
			return total, nil
		}
		if readErr != nil {
			return total, readErr
		}
	}
}

func hashFile(ctx context.Context, path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open for hashing: %w", err)
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := copyWithContext(ctx, hash, file); err != nil {
		return "", fmt.Errorf("hash file: %w", err)
	}
	return fmt.Sprintf("%x", hash.Sum(nil)), nil
}
