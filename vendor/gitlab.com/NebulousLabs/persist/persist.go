package persist

import (
	"gitlab.com/NebulousLabs/errors"
	"sync"
)

const (
	// defaultDirPermissions is the default permissions when creating dirs.
	defaultDirPermissions = 0700

	// defaultFilePermissions is the default permissions when creating files.
	defaultFilePermissions = 0600

	// persistDir defines the folder that is used for testing the persist
	// package.
	persistDir = "persist"

	// tempSuffix is the suffix that is applied to the temporary/backup versions
	// of the files being persisted.
	tempSuffix = "_temp"
)

var (
	// ErrBadHeader indicates that the file opened is not the file that was
	// expected.
	ErrBadHeader = errors.New("wrong header")

	// ErrBadVersion indicates that the version number of the file is not
	// compatible with the current codebase.
	ErrBadVersion = errors.New("incompatible version")

	// activeFiles is a map tracking which filenames are currently being used
	// for saving and loading. There should never be a situation where the same
	// file is being called twice from different threads, as the persist package
	// has no way to tell what order they were intended to be called.
	activeFiles   = make(map[string]struct{})
	activeFilesMu sync.Mutex
)

// Metadata contains the header and version of the data being stored.
type Metadata struct {
	Header  string
	Version string
}
