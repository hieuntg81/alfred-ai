package tool

import "os"

// LocalFilesystemBackend performs file I/O on the local filesystem.
type LocalFilesystemBackend struct{}

// NewLocalFilesystemBackend creates a local filesystem backend.
func NewLocalFilesystemBackend() *LocalFilesystemBackend {
	return &LocalFilesystemBackend{}
}

func (b *LocalFilesystemBackend) Name() string { return "local" }

func (b *LocalFilesystemBackend) ReadFile(path string) ([]byte, error) {
	return os.ReadFile(path)
}

func (b *LocalFilesystemBackend) WriteFile(path string, data []byte, perm os.FileMode) error {
	return os.WriteFile(path, data, perm)
}

func (b *LocalFilesystemBackend) ReadDir(path string) ([]os.DirEntry, error) {
	return os.ReadDir(path)
}
