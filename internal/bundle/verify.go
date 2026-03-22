package bundle

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// VerifyResult contains the results of bundle verification.
type VerifyResult struct {
	// Valid is true if all checks passed.
	Valid bool `json:"valid"`

	// ManifestValid indicates if the manifest was found and parseable.
	ManifestValid bool `json:"manifest_valid"`

	// SchemaValid indicates if the schema version is supported.
	SchemaValid bool `json:"schema_valid"`

	// FilesPresent indicates if all manifest files exist in the bundle.
	FilesPresent bool `json:"files_present"`

	// ChecksumsValid indicates if all checksums match.
	ChecksumsValid bool `json:"checksums_valid"`

	// Errors contains any validation errors.
	Errors []string `json:"errors,omitempty"`

	// Warnings contains non-fatal issues.
	Warnings []string `json:"warnings,omitempty"`

	// Details contains additional verification details.
	Details map[string]string `json:"details,omitempty"`

	// Manifest is the parsed manifest (nil if not found/invalid).
	Manifest *Manifest `json:"manifest,omitempty"`
}

// ManifestFileName is the expected manifest file name in bundles.
const ManifestFileName = "manifest.json"

// Verify validates a support bundle archive.
func Verify(bundlePath string) (*VerifyResult, error) {
	result := &VerifyResult{
		Valid:          true,
		ManifestValid:  true,
		SchemaValid:    true,
		FilesPresent:   true,
		ChecksumsValid: true,
		Errors:         []string{},
		Warnings:       []string{},
		Details:        make(map[string]string),
	}

	format := DetectFormat(bundlePath)
	result.Details["format"] = string(format)
	result.Details["path"] = bundlePath

	switch format {
	case FormatZip:
		return verifyZip(bundlePath, result)
	case FormatTarGz:
		return verifyTarGz(bundlePath, result)
	default:
		result.Valid = false
		result.ManifestValid = false
		result.Errors = append(result.Errors, "unknown or unsupported bundle format")
		return result, nil
	}
}

// verifyZip verifies a zip bundle.
func verifyZip(path string, result *VerifyResult) (_ *VerifyResult, err error) {
	r, err := zip.OpenReader(path)
	if err != nil {
		result.Valid = false
		result.ManifestValid = false
		result.Errors = append(result.Errors, fmt.Sprintf("failed to open zip: %v", err))
		return result, nil
	}
	defer func() {
		if closeErr := r.Close(); err == nil && closeErr != nil {
			err = fmt.Errorf("close zip reader: %w", closeErr)
		}
	}()

	// Build file map and find manifest
	files := make(map[string]*zip.File)
	var manifestFile *zip.File

	for _, f := range r.File {
		name := filepath.Clean(f.Name)
		files[name] = f
		if filepath.Base(name) == ManifestFileName {
			manifestFile = f
		}
	}

	result.Details["file_count"] = fmt.Sprintf("%d", len(files))

	if manifestFile == nil {
		result.Valid = false
		result.ManifestValid = false
		result.Errors = append(result.Errors, "manifest.json not found in bundle")
		return result, nil
	}

	// Parse manifest
	manifest, err := readManifestFromZip(manifestFile)
	if err != nil {
		result.Valid = false
		result.ManifestValid = false
		result.Errors = append(result.Errors, fmt.Sprintf("failed to parse manifest: %v", err))
		return result, nil
	}
	result.Manifest = manifest

	// Validate manifest schema
	if err := manifest.Validate(); err != nil {
		result.SchemaValid = false
		result.Errors = append(result.Errors, fmt.Sprintf("manifest validation failed: %v", err))
	}

	// Verify files and checksums
	verifyManifestFiles(result, manifest, func(path string) ([]byte, error) {
		f, ok := files[path]
		if !ok {
			return nil, os.ErrNotExist
		}
		rc, err := f.Open()
		if err != nil {
			return nil, err
		}
		defer func() {
			if closeErr := rc.Close(); err == nil && closeErr != nil {
				err = fmt.Errorf("close zip entry %s: %w", path, closeErr)
			}
		}()
		data, err := io.ReadAll(io.LimitReader(rc, 100<<20)) // 100MB max per entry
		if err != nil {
			return nil, err
		}
		return data, err
	})

	result.Valid = result.ManifestValid && result.SchemaValid && result.FilesPresent && result.ChecksumsValid

	return result, nil
}

// verifyTarGz verifies a tar.gz bundle.
func verifyTarGz(path string, result *VerifyResult) (_ *VerifyResult, err error) {
	f, err := os.Open(path)
	if err != nil {
		result.Valid = false
		result.ManifestValid = false
		result.Errors = append(result.Errors, fmt.Sprintf("failed to open file: %v", err))
		return result, nil
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		result.Valid = false
		result.ManifestValid = false
		result.Errors = append(result.Errors, fmt.Sprintf("failed to decompress gzip: %v", err))
		return result, nil
	}
	defer func() {
		if closeErr := gz.Close(); err == nil && closeErr != nil {
			err = fmt.Errorf("close gzip reader: %w", closeErr)
		}
	}()

	// Read all files into memory for verification
	files := make(map[string][]byte)
	tr := tar.NewReader(gz)

	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			result.Valid = false
			result.ManifestValid = false
			result.Errors = append(result.Errors, fmt.Sprintf("failed to read tar: %v", err))
			return result, nil
		}

		if hdr.Typeflag == tar.TypeReg {
			name := filepath.Clean(hdr.Name)
			data, err := io.ReadAll(io.LimitReader(tr, 100<<20)) // 100MB max per entry
			if err != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("failed to read %s: %v", name, err))
				continue
			}
			files[name] = data
		}
	}

	result.Details["file_count"] = fmt.Sprintf("%d", len(files))

	// Find and parse manifest
	var manifestData []byte
	for name, data := range files {
		if filepath.Base(name) == ManifestFileName {
			manifestData = data
			break
		}
	}

	if manifestData == nil {
		result.Valid = false
		result.ManifestValid = false
		result.Errors = append(result.Errors, "manifest.json not found in bundle")
		return result, nil
	}

	var manifest Manifest
	if err := json.Unmarshal(manifestData, &manifest); err != nil {
		result.Valid = false
		result.ManifestValid = false
		result.Errors = append(result.Errors, fmt.Sprintf("failed to parse manifest: %v", err))
		return result, nil
	}
	result.Manifest = &manifest

	// Validate manifest schema
	if err := manifest.Validate(); err != nil {
		result.SchemaValid = false
		result.Errors = append(result.Errors, fmt.Sprintf("manifest validation failed: %v", err))
	}

	// Verify files and checksums
	verifyManifestFiles(result, &manifest, func(path string) ([]byte, error) {
		data, ok := files[path]
		if !ok {
			return nil, os.ErrNotExist
		}
		return data, nil
	})

	result.Valid = result.ManifestValid && result.SchemaValid && result.FilesPresent && result.ChecksumsValid

	return result, nil
}

// verifyManifestFiles checks that all manifest files exist and have correct checksums.
func verifyManifestFiles(result *VerifyResult, manifest *Manifest, readFile func(string) ([]byte, error)) {
	verified := 0
	missing := 0
	mismatched := 0

	for _, entry := range manifest.Files {
		data, err := readFile(entry.Path)
		if err != nil {
			if os.IsNotExist(err) {
				result.FilesPresent = false
				result.Errors = append(result.Errors, fmt.Sprintf("missing file: %s", entry.Path))
				missing++
			} else {
				result.Errors = append(result.Errors, fmt.Sprintf("error reading %s: %v", entry.Path, err))
			}
			continue
		}

		actualHash := HashBytes(data)
		if actualHash != entry.SHA256 {
			result.ChecksumsValid = false
			result.Errors = append(result.Errors, fmt.Sprintf("checksum mismatch: %s (expected %s..., got %s...)",
				entry.Path, entry.SHA256[:16], actualHash[:16]))
			mismatched++
		} else {
			verified++
		}

		// Check size
		if int64(len(data)) != entry.SizeBytes {
			result.Warnings = append(result.Warnings, fmt.Sprintf("size mismatch: %s (expected %d, got %d)",
				entry.Path, entry.SizeBytes, len(data)))
		}
	}

	result.Details["verified"] = fmt.Sprintf("%d", verified)
	result.Details["missing"] = fmt.Sprintf("%d", missing)
	result.Details["mismatched"] = fmt.Sprintf("%d", mismatched)
}

// readManifestFromZip reads and parses the manifest from a zip file entry.
func readManifestFromZip(f *zip.File) (*Manifest, error) {
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()

	data, err := io.ReadAll(rc)
	if err != nil {
		return nil, err
	}

	var manifest Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, err
	}

	return &manifest, nil
}

// Format represents a bundle archive format.
type Format string

const (
	FormatZip     Format = "zip"
	FormatTarGz   Format = "tar.gz"
	FormatUnknown Format = "unknown"
)

// DetectFormat determines the bundle format from the file path.
func DetectFormat(path string) Format {
	lower := strings.ToLower(path)
	if strings.HasSuffix(lower, ".zip") {
		return FormatZip
	}
	if strings.HasSuffix(lower, ".tar.gz") || strings.HasSuffix(lower, ".tgz") {
		return FormatTarGz
	}
	return FormatUnknown
}

// DefaultFormat is the default bundle format.
const DefaultFormat = FormatZip

// Extension returns the file extension for a format.
func (f Format) Extension() string {
	switch f {
	case FormatZip:
		return ".zip"
	case FormatTarGz:
		return ".tar.gz"
	default:
		return ""
	}
}
