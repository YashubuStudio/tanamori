// Package modelsetup installs and validates TANAMORI's local AI model runtime.
package modelsetup

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	bgebundle "github.com/YashubuStudio/tanamori/models/bge-reranker-v2-m3"
)

const markerSchemaVersion = 1

// Manifest describes the exact upstream model artifact embedded in TANAMORI.
type Manifest struct {
	SchemaVersion  int      `json:"schema_version"`
	ModelID        string   `json:"model_id"`
	Revision       string   `json:"revision"`
	SourceURL      string   `json:"source_url"`
	License        string   `json:"license"`
	WeightFile     string   `json:"weight_file"`
	WeightSize     int64    `json:"weight_size"`
	WeightSHA256   string   `json:"weight_sha256"`
	DownloadFiles  []string `json:"download_files"`
	PythonMinimum  string   `json:"python_minimum"`
	PythonPackages []string `json:"python_packages"`
}

// Paths are all per-user paths used by the model integration.
type Paths struct {
	Root       string `json:"root"`
	ModelDir   string `json:"model_dir"`
	RuntimeDir string `json:"runtime_dir"`
	VenvDir    string `json:"venv_dir"`
	Python     string `json:"python"`
	Runner     string `json:"runner"`
	Marker     string `json:"marker"`
	Weight     string `json:"weight"`
}

// Status is intentionally JSON-friendly so the CLI and a future GUI can use
// the same first-run setup service.
type Status struct {
	Ready         bool       `json:"ready"`
	Verified      bool       `json:"verified"`
	ModelID       string     `json:"model_id"`
	Revision      string     `json:"revision"`
	License       string     `json:"license"`
	SourceURL     string     `json:"source_url"`
	WeightBytes   int64      `json:"weight_bytes"`
	WeightSHA256  string     `json:"weight_sha256"`
	InstalledAt   *time.Time `json:"installed_at,omitempty"`
	RuntimePython string     `json:"runtime_python,omitempty"`
	PythonVersion string     `json:"python_version,omitempty"`
	Paths         Paths      `json:"paths"`
	Message       string     `json:"message,omitempty"`
}

type installMarker struct {
	SchemaVersion int       `json:"schema_version"`
	ModelID       string    `json:"model_id"`
	Revision      string    `json:"revision"`
	WeightSHA256  string    `json:"weight_sha256"`
	InstalledAt   time.Time `json:"installed_at"`
	PythonVersion string    `json:"python_version"`
}

type pythonDetails struct {
	Version string `json:"version"`
	Major   int    `json:"major"`
	Minor   int    `json:"minor"`
	Bits    string `json:"bits"`
}

// Installer manages a private virtual environment and pinned model snapshot.
type Installer struct {
	Manifest Manifest
	Paths    Paths
	Python   string
	Stdout   io.Writer
	Stderr   io.Writer
}

// DefaultRoot returns the per-user AI data directory.
func DefaultRoot() string {
	if local := strings.TrimSpace(os.Getenv("LOCALAPPDATA")); local != "" {
		return filepath.Join(local, "YashubuStudio", "TANAMORI", "ai")
	}
	if config, err := os.UserConfigDir(); err == nil {
		return filepath.Join(config, "YashubuStudio", "TANAMORI", "ai")
	}
	return filepath.Join("data", "ai")
}

// New creates an installer without touching the filesystem or network.
func New(root string) (*Installer, error) {
	var manifest Manifest
	if err := json.Unmarshal(bgebundle.Manifest(), &manifest); err != nil {
		return nil, fmt.Errorf("decode embedded model manifest: %w", err)
	}
	if err := validateManifest(manifest); err != nil {
		return nil, err
	}
	if strings.TrimSpace(root) == "" {
		root = DefaultRoot()
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve model root: %w", err)
	}
	modelDir := filepath.Join(absolute, "models", "bge-reranker-v2-m3", manifest.Revision)
	runtimeDir := filepath.Join(absolute, "runtime", "bge-reranker-v2-m3")
	venvDir := filepath.Join(runtimeDir, ".venv")
	venvPython := filepath.Join(venvDir, "bin", "python")
	if runtime.GOOS == "windows" {
		venvPython = filepath.Join(venvDir, "Scripts", "python.exe")
	}
	return &Installer{
		Manifest: manifest,
		Paths: Paths{
			Root: absolute, ModelDir: modelDir, RuntimeDir: runtimeDir, VenvDir: venvDir,
			Python: venvPython, Runner: filepath.Join(runtimeDir, "runtime.py"),
			Marker: filepath.Join(runtimeDir, "install.json"),
			Weight: filepath.Join(modelDir, manifest.WeightFile),
		},
		Stdout: io.Discard,
		Stderr: io.Discard,
	}, nil
}

func validateManifest(manifest Manifest) error {
	if manifest.SchemaVersion != 1 || manifest.ModelID == "" || len(manifest.Revision) != 40 {
		return errors.New("embedded model manifest is incomplete")
	}
	if manifest.WeightFile == "" || manifest.WeightSize <= 0 || len(manifest.WeightSHA256) != 64 {
		return errors.New("embedded model weight metadata is invalid")
	}
	if len(manifest.DownloadFiles) == 0 || len(manifest.PythonPackages) == 0 {
		return errors.New("embedded model runtime metadata is incomplete")
	}
	return nil
}

// Status performs a quick, non-networked readiness check. Full SHA-256
// validation is available through Verify and is always performed by Install.
func (installer *Installer) Status() (Status, error) {
	status := installer.baseStatus()
	markerBytes, err := os.ReadFile(installer.Paths.Marker)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			status.Message = "model is not installed; run `tanamori setup`"
			return status, nil
		}
		return status, fmt.Errorf("read model installation marker: %w", err)
	}
	var marker installMarker
	if err := json.Unmarshal(markerBytes, &marker); err != nil {
		status.Message = "model installation marker is invalid; run `tanamori setup --force`"
		return status, nil
	}
	status.InstalledAt = &marker.InstalledAt
	status.PythonVersion = marker.PythonVersion
	if marker.SchemaVersion != markerSchemaVersion || marker.ModelID != installer.Manifest.ModelID ||
		marker.Revision != installer.Manifest.Revision || !strings.EqualFold(marker.WeightSHA256, installer.Manifest.WeightSHA256) {
		status.Message = "installed model version does not match this TANAMORI build; run `tanamori setup --force`"
		return status, nil
	}
	for _, required := range []string{installer.Paths.Python, installer.Paths.Runner, installer.Paths.Weight} {
		info, statErr := os.Stat(required)
		if statErr != nil || !info.Mode().IsRegular() {
			status.Message = "model installation is incomplete; run `tanamori setup --force`"
			return status, nil
		}
	}
	runtimeBytes, err := os.ReadFile(installer.Paths.Runner)
	if err != nil {
		return status, fmt.Errorf("read model runtime: %w", err)
	}
	if !bytes.Equal(runtimeBytes, bgebundle.Runtime()) {
		status.Message = "model runtime differs from this TANAMORI build; run `tanamori setup`"
		return status, nil
	}
	weight, err := os.Stat(installer.Paths.Weight)
	if err != nil {
		return status, fmt.Errorf("inspect model weight: %w", err)
	}
	if weight.Size() != installer.Manifest.WeightSize {
		status.Message = "model weight size is invalid; run `tanamori setup --force`"
		return status, nil
	}
	status.Ready = true
	status.Verified = true
	status.Message = "ready"
	return status, nil
}

func (installer *Installer) baseStatus() Status {
	return Status{
		ModelID: installer.Manifest.ModelID, Revision: installer.Manifest.Revision,
		License: installer.Manifest.License, SourceURL: installer.Manifest.SourceURL,
		WeightBytes: installer.Manifest.WeightSize, WeightSHA256: installer.Manifest.WeightSHA256,
		RuntimePython: installer.Paths.Python, Paths: installer.Paths,
	}
}

// Install creates an isolated Python runtime, installs compatible packages,
// downloads the exact upstream revision, and verifies the large weight file.
func (installer *Installer) Install(ctx context.Context, force bool) (Status, error) {
	status, err := installer.Status()
	if err != nil {
		return status, err
	}
	if status.Ready && !force {
		return status, nil
	}
	basePython, details, err := installer.findPython(ctx)
	if err != nil {
		return status, err
	}
	if err := os.MkdirAll(installer.Paths.RuntimeDir, 0o755); err != nil {
		return status, fmt.Errorf("create model runtime directory: %w", err)
	}
	if err := os.MkdirAll(installer.Paths.ModelDir, 0o755); err != nil {
		return status, fmt.Errorf("create model directory: %w", err)
	}
	if err := os.WriteFile(installer.Paths.Runner, bgebundle.Runtime(), 0o644); err != nil {
		return status, fmt.Errorf("write embedded model runtime: %w", err)
	}
	if _, err := os.Stat(installer.Paths.Python); errors.Is(err, os.ErrNotExist) {
		if err := installer.run(ctx, "creating isolated Python environment", basePython, "-m", "venv", installer.Paths.VenvDir); err != nil {
			return status, err
		}
	} else if err != nil {
		return status, fmt.Errorf("inspect Python environment: %w", err)
	} else if force {
		if err := installer.run(ctx, "refreshing isolated Python environment", basePython, "-m", "venv", "--upgrade", installer.Paths.VenvDir); err != nil {
			return status, err
		}
	}
	if err := installer.run(ctx, "updating pip", installer.Paths.Python, "-m", "pip", "install", "--disable-pip-version-check", "--no-input", "--upgrade", "pip"); err != nil {
		return status, err
	}
	packageArgs := []string{"-m", "pip", "install", "--disable-pip-version-check", "--no-input", "--upgrade"}
	packageArgs = append(packageArgs, installer.Manifest.PythonPackages...)
	if err := installer.run(ctx, "installing model runtime packages", installer.Paths.Python, packageArgs...); err != nil {
		return status, err
	}
	setupArgs := []string{
		installer.Paths.Runner, "setup",
		"--model-id", installer.Manifest.ModelID,
		"--revision", installer.Manifest.Revision,
		"--model-dir", installer.Paths.ModelDir,
		"--weight-file", installer.Manifest.WeightFile,
		"--weight-size", strconv.FormatInt(installer.Manifest.WeightSize, 10),
		"--weight-sha256", installer.Manifest.WeightSHA256,
	}
	for _, file := range installer.Manifest.DownloadFiles {
		setupArgs = append(setupArgs, "--file", file)
	}
	if force {
		setupArgs = append(setupArgs, "--force-download")
	}
	if err := installer.run(ctx, "downloading and verifying BAAI/bge-reranker-v2-m3", installer.Paths.Python, setupArgs...); err != nil {
		return status, err
	}
	marker := installMarker{
		SchemaVersion: markerSchemaVersion, ModelID: installer.Manifest.ModelID,
		Revision: installer.Manifest.Revision, WeightSHA256: installer.Manifest.WeightSHA256,
		InstalledAt: time.Now().UTC(), PythonVersion: details.Version,
	}
	markerBytes, err := json.MarshalIndent(marker, "", "  ")
	if err != nil {
		return status, fmt.Errorf("encode model installation marker: %w", err)
	}
	markerBytes = append(markerBytes, '\n')
	if err := os.WriteFile(installer.Paths.Marker, markerBytes, 0o644); err != nil {
		return status, fmt.Errorf("write model installation marker: %w", err)
	}
	return installer.Status()
}

func (installer *Installer) findPython(ctx context.Context) (string, pythonDetails, error) {
	candidates := []string{}
	if candidate := strings.TrimSpace(installer.Python); candidate != "" {
		candidates = append(candidates, candidate)
	} else if configured := strings.TrimSpace(os.Getenv("TANAMORI_PYTHON")); configured != "" {
		candidates = append(candidates, configured)
	} else {
		candidates = append(candidates, "python3", "python")
	}
	var failures []string
	for _, candidate := range candidates {
		path, err := exec.LookPath(candidate)
		if err != nil {
			failures = append(failures, candidate+": not found")
			continue
		}
		details, err := inspectPython(ctx, path)
		if err != nil {
			failures = append(failures, candidate+": "+err.Error())
			continue
		}
		if details.Major != 3 || details.Minor < 10 {
			failures = append(failures, candidate+": Python 3.10 or newer is required")
			continue
		}
		if details.Bits != "64bit" {
			failures = append(failures, candidate+": 64-bit Python is required for the 2.27 GB model")
			continue
		}
		return path, details, nil
	}
	return "", pythonDetails{}, fmt.Errorf("compatible Python was not found (%s); install 64-bit Python 3.10+ or pass --python", strings.Join(failures, "; "))
}

func inspectPython(ctx context.Context, python string) (pythonDetails, error) {
	const script = `import json,platform,sys; print(json.dumps({"version": platform.python_version(), "major": sys.version_info.major, "minor": sys.version_info.minor, "bits": platform.architecture()[0]}))`
	output, err := exec.CommandContext(ctx, python, "-c", script).Output()
	if err != nil {
		return pythonDetails{}, err
	}
	var details pythonDetails
	if err := json.Unmarshal(output, &details); err != nil {
		return pythonDetails{}, fmt.Errorf("read version: %w", err)
	}
	return details, nil
}

func (installer *Installer) run(ctx context.Context, label, executable string, arguments ...string) error {
	if installer.Stderr != nil {
		fmt.Fprintf(installer.Stderr, "[TANAMORI setup] %s\n", label)
	}
	command := exec.CommandContext(ctx, executable, arguments...)
	command.Stdout = installer.Stdout
	command.Stderr = installer.Stderr
	if err := command.Run(); err != nil {
		return fmt.Errorf("%s: %w", label, err)
	}
	return nil
}

// Verify streams the complete model weight through SHA-256. It is deliberately
// explicit because reading 2.27 GB on every application start would be costly.
func (installer *Installer) Verify(ctx context.Context) (Status, error) {
	status, err := installer.Status()
	if err != nil {
		return status, err
	}
	if !status.Ready {
		return status, errors.New(status.Message)
	}
	file, err := os.Open(installer.Paths.Weight)
	if err != nil {
		return status, fmt.Errorf("open model weight: %w", err)
	}
	defer file.Close()
	digest := sha256.New()
	buffer := make([]byte, 8*1024*1024)
	for {
		if err := ctx.Err(); err != nil {
			return status, err
		}
		count, readErr := file.Read(buffer)
		if count > 0 {
			_, _ = digest.Write(buffer[:count])
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return status, fmt.Errorf("hash model weight: %w", readErr)
		}
	}
	actual := hex.EncodeToString(digest.Sum(nil))
	if !strings.EqualFold(actual, installer.Manifest.WeightSHA256) {
		status.Ready = false
		status.Verified = false
		status.Message = "model weight SHA-256 is invalid; run `tanamori setup --force`"
		return status, fmt.Errorf("model weight SHA-256 mismatch: got %s, want %s", actual, installer.Manifest.WeightSHA256)
	}
	status.Verified = true
	status.Message = "ready; full SHA-256 verification passed"
	return status, nil
}
