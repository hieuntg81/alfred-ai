package tool

import "os"

// FilesystemBackend abstracts file I/O operations.
type FilesystemBackend interface {
	// ReadFile reads the named file and returns its contents.
	ReadFile(path string) ([]byte, error)
	// WriteFile writes data to the named file with the given permissions.
	WriteFile(path string, data []byte, perm os.FileMode) error
	// ReadDir reads the named directory and returns its directory entries.
	ReadDir(path string) ([]os.DirEntry, error)
	// Name returns the backend identifier (e.g. "local").
	Name() string
}
