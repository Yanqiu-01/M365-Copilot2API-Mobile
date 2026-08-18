package web

import "os"

// writeFileAtomic persists b to path via a temp file + rename so a crash
// mid-write can never leave a truncated store file that fails to load.
func writeFileAtomic(path string, b []byte, perm os.FileMode) error {
	if path == "" {
		return nil
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, perm); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
