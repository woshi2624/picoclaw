package skills

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/sipeed/picoclaw/pkg/fileutil"
)

const lockfileName = ".skills_store_lock.json"

// LockfileEntry records install metadata for a single skill.
type LockfileEntry struct {
	Name    string `json:"name"`
	ZipURL  string `json:"zip_url,omitempty"`
	Source  string `json:"source"`
	Version string `json:"version"`
}

// Lockfile tracks all installed skills and their metadata.
type Lockfile struct {
	Version int                      `json:"version"`
	Skills  map[string]LockfileEntry `json:"skills"`
}

// LoadLockfile reads the lockfile from {workspaceDir}/skills/.skills_store_lock.json.
// If the file does not exist, an empty Lockfile is returned.
func LoadLockfile(workspaceDir string) (*Lockfile, error) {
	path := filepath.Join(workspaceDir, "skills", lockfileName)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &Lockfile{Version: 1, Skills: make(map[string]LockfileEntry)}, nil
		}
		return nil, err
	}

	var lf Lockfile
	if err := json.Unmarshal(data, &lf); err != nil {
		return nil, err
	}
	if lf.Skills == nil {
		lf.Skills = make(map[string]LockfileEntry)
	}
	return &lf, nil
}

// SaveLockfile atomically writes the lockfile to {workspaceDir}/skills/.skills_store_lock.json.
func SaveLockfile(workspaceDir string, lf *Lockfile) error {
	dir := filepath.Join(workspaceDir, "skills")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(lf, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')

	return fileutil.WriteFileAtomic(filepath.Join(dir, lockfileName), data, 0o644)
}

// SetEntry adds or updates an entry in the lockfile.
func (lf *Lockfile) SetEntry(slug string, entry LockfileEntry) {
	if lf.Skills == nil {
		lf.Skills = make(map[string]LockfileEntry)
	}
	lf.Skills[slug] = entry
}

// RemoveEntry removes an entry from the lockfile.
func (lf *Lockfile) RemoveEntry(slug string) {
	delete(lf.Skills, slug)
}
