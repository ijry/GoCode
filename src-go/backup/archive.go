package backup

// ZIP construction / extraction + per-entry integrity, mirroring upstream
// archive.rs. Synchronous by design; the web layer runs it inline (backups here
// are small — the DB + a handful of sections), the desktop path could spawn it.

import (
	"archive/zip"
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const copyBuf = 64 * 1024

// maxManifestBytes bounds the decompressed manifest.jpg read from an untrusted
// archive (manifests are small; defeats a decompression bomb).
const maxManifestBytes = 16 * 1024 * 1024

// ArchiveBuilder incrementally builds a backup ZIP, recording a ManifestEntry
// (size + sha256) for every file, then writes the manifest as the final entry.
type ArchiveBuilder struct {
	w         *zip.Writer
	file      *os.File
	entries   []ManifestEntry
	processed uint64
}

// NewArchiveBuilder creates the ZIP at dest.
func NewArchiveBuilder(dest string) (*ArchiveBuilder, error) {
	f, err := os.Create(dest)
	if err != nil {
		return nil, err
	}
	return &ArchiveBuilder{w: zip.NewWriter(bufio.NewWriter(f)), file: f}, nil
}

// AddFile streams src into the archive under entryName, hashing as it go.
func (b *ArchiveBuilder) AddFile(entryName, src string, progress func(string, uint64)) error {
	hdr := &zip.FileHeader{Name: entryName, Method: zip.Deflate}
	f, err := os.Open(src)
	if err != nil {
		return err
	}
	defer f.Close()
	w, err := b.w.CreateHeader(hdr)
	if err != nil {
		return err
	}
	r := bufio.NewReader(f)
	h := sha256.New()
	buf := make([]byte, copyBuf)
	var size uint64
	for {
		n, rerr := r.Read(buf)
		if n > 0 {
			if _, werr := w.Write(buf[:n]); werr != nil {
				return werr
			}
			h.Write(buf[:n])
			size += uint64(n)
			b.processed += uint64(n)
			if progress != nil {
				progress(entryName, b.processed)
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			return rerr
		}
	}
	b.entries = append(b.entries, ManifestEntry{
		Path: entryName, Size: size, SHA256: hex.EncodeToString(h.Sum(nil)),
	})
	return nil
}

// AddDir recursively adds every regular file under root as prefix/<rel>,
// skipping symlinks (never followed) and exclude(rel). A missing root is a no-op.
func (b *ArchiveBuilder) AddDir(prefix, root string, exclude func(string) bool, progress func(string, uint64)) error {
	if _, err := os.Stat(root); err != nil {
		return nil // missing root is a no-op
	}
	return filepath.Walk(root, func(path string, info os.FileInfo, werr error) error {
		if werr != nil {
			return werr
		}
		if info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return nil
		}
		rel, rerr := filepath.Rel(root, path)
		if rerr != nil {
			return nil
		}
		slashRel := filepath.ToSlash(rel)
		if exclude != nil && exclude(slashRel) {
			return nil
		}
		return b.AddFile(prefix+"/"+slashRel, path, progress)
	})
}

// Finish writes manifest.json (uncompressed) as the last entry, closes the
// archive, and returns the manifest with entries populated.
func (b *ArchiveBuilder) Finish(m BackupManifest) (BackupManifest, error) {
	m.Entries = b.entries
	jsonB, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return m, err
	}
	hdr := &zip.FileHeader{Name: ManifestEntryName, Method: zip.Store}
	w, err := b.w.CreateHeader(hdr)
	if err != nil {
		return m, err
	}
	if _, err := w.Write(jsonB); err != nil {
		return m, err
	}
	if err := b.w.Close(); err != nil {
		return m, err
	}
	if err := b.file.Close(); err != nil {
		return m, err
	}
	return m, nil
}

// ReadManifest reads + validates just the manifest from a plaintext ZIP.
func ReadManifest(zipPath string) (BackupManifest, error) {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return BackupManifest{}, errUnknownFormat()
	}
	defer zr.Close()
	var mf zip.File
	for _, f := range zr.File {
		if f.Name == ManifestEntryName {
			mf = *f
			break
		}
	}
	if mf.Name == "" {
		return BackupManifest{}, errUnknownFormat()
	}
	rc, err := mf.Open()
	if err != nil {
		return BackupManifest{}, errUnknownFormat()
	}
	defer rc.Close()
	limited := io.LimitReader(rc, maxManifestBytes+1)
	raw, rerr := io.ReadAll(limited)
	if rerr != nil || uint64(len(raw)) > maxManifestBytes {
		return BackupManifest{}, errUnknownFormat()
	}
	var m BackupManifest
	if json.Unmarshal(raw, &m) != nil {
		return BackupManifest{}, errUnknownFormat()
	}
	if m.Kind != BackupKind {
		return BackupManifest{}, errUnknownFormat()
	}
	return m, nil
}

// ValidateManifest rejects a crafted manifest: every entry must be a safe
// relative path (no traversal / absolute / the manifest itself), unique, and a
// backup must carry the DB. Declared sections must cover every entry's top-level
// component.
func ValidateManifest(m *BackupManifest) error {
	seen := map[string]bool{}
	for _, e := range m.Entries {
		if !safeRelPath(e.Path) || e.Path == ManifestEntryName {
			return errCorrupted()
		}
		if seen[e.Path] {
			return errCorrupted()
		}
		seen[e.Path] = true
	}
	if !seen[DBEntry] {
		return errCorrupted()
	}
	if m.ManagedSections != nil {
		declared := map[string]bool{}
		for _, d := range m.ManagedSections {
			declared[d] = true
		}
		for _, e := range m.Entries {
			top := e.Path
			if i := strings.IndexByte(e.Path, '/'); i >= 0 {
				top = e.Path[:i]
			}
			if top == "db" || top == ExternalSectionPrefix || declared[top] {
				continue
			}
			return errCorrupted()
		}
	}
	return nil
}

// ExtractAll extracts manifest-listed entries into destRoot with zip-slip +
// manifest-bounding (an unlisted or oversized entry is rejected). Used by the
// restore engine and by tests to round-trip a produced archive.
func ExtractAll(zipPath, destRoot string, m *BackupManifest, progress func(string, uint64)) error {
	sizes := map[string]uint64{}
	for _, e := range m.Entries {
		sizes[e.Path] = e.Size
	}
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return errUnknownFormat()
	}
	defer zr.Close()
	seen := map[string]bool{}
	var processed uint64
	for _, f := range zr.File {
		rc, oerr := f.Open()
		if oerr != nil {
			return errCorrupted()
		}
		name := filepath.ToSlash(f.Name)
		if name == ManifestEntryName {
			rc.Close()
			continue
		}
		if f.FileInfo().IsDir() || strings.HasSuffix(name, "/") {
			rc.Close()
			continue
		}
		rel := enclosingRel(name)
		if rel == "" {
			rc.Close()
			return fmt.Errorf("backup contains an unsafe path entry: %s", name)
		}
		expected, listed := sizes[rel]
		if !listed {
			rc.Close()
			return errCorrupted()
		}
		if seen[rel] {
			rc.Close()
			return errCorrupted()
		}
		seen[rel] = true
		out := filepath.Join(destRoot, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
			rc.Close()
			return err
		}
		of, cerr := os.Create(out)
		if cerr != nil {
			rc.Close()
			return cerr
		}
		limited := io.LimitReader(rc, int64(expected)+1)
		n, cerr := io.Copy(of, limited)
		of.Close()
		rc.Close()
		if cerr != nil {
			return cerr
		}
		if uint64(n) > expected {
			return errCorrupted()
		}
		processed += uint64(n)
		if progress != nil {
			progress(rel, processed)
		}
	}
	return nil
}

// VerifyChecksums re-hashes extracted files against the manifest.
func VerifyChecksums(destRoot string, m *BackupManifest) error {
	for _, e := range m.Entries {
		p := filepath.Join(destRoot, filepath.FromSlash(e.Path))
		f, err := os.Open(p)
		if err != nil {
			return errCorrupted()
		}
		h := sha256.New()
		size, err := io.Copy(h, f)
		f.Close()
		if err != nil {
			return err
		}
		if uint64(size) != e.Size || hex.EncodeToString(h.Sum(nil)) != e.SHA256 {
			return errCorrupted()
		}
	}
	return nil
}

// private helpers

func safeRelPath(p string) bool {
	if p == "" {
		return false
	}
	if strings.HasPrefix(p, "/") || strings.HasPrefix(p, "\\") || filepath.IsAbs(p) {
		return false
	}
	for _, comp := range strings.Split(filepath.ToSlash(p), "/") {
		switch comp {
		case "", ".", "..":
			return false
		}
		if strings.ContainsAny(comp, "\\") {
			return false
		}
	}
	return true
}

// enclosingRel is a zip-slip guard mirroring enclosed_name: only safe relative
// components, else "".
func enclosingRel(name string) string {
	name = filepath.ToSlash(name)
	var parts []string
	for _, comp := range strings.Split(name, "/") {
		switch comp {
		case "", ".":
			continue
		case "..":
			return ""
		}
		if strings.HasPrefix(comp, "/") || strings.Contains(comp, "\\") {
			return ""
		}
		parts = append(parts, comp)
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, "/")
}

func errUnknownFormat() error { return fmt.Errorf("unknown backup format") }
func errCorrupted() error     { return fmt.Errorf("corrupted backup archive") }
