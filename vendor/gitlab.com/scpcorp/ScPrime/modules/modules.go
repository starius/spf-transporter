// Package modules contains definitions for all of the major modules of ScPrime, as
// well as some helper functions for performing actions that are common to
// multiple modules.
package modules

import (
	"fmt"
	"math"
	"time"

	"gitlab.com/scpcorp/ScPrime/build"
)

var (
	// SafeMutexDelay is the recommended timeout for the deadlock detecting
	// mutex. This value is DEPRECATED, as safe mutexes are no longer
	// recommended. Instead, the locking conventions should be followed and a
	// traditional mutex or a demote mutex should be used.
	SafeMutexDelay time.Duration
)

func init() {
	if build.Release == "dev" {
		SafeMutexDelay = 60 * time.Second
	} else if build.Release == "standard" {
		SafeMutexDelay = 90 * time.Second
	} else if build.Release == "testing" {
		SafeMutexDelay = 30 * time.Second
	}
}

// PeekErr checks if a chan error has an error waiting to be returned. If it has
// it will return that error. Otherwise it returns 'nil'.
func PeekErr(errChan <-chan error) (err error) {
	select {
	case err = <-errChan:
	default:
	}
	return
}

// FilesizeUnits returns a string that displays a filesize in human-readable units.
func FilesizeUnits(size uint64) string {
	if size == 0 {
		return "0  B"
	}
	sizes := []string{" B", "KB", "MB", "GB", "TB", "PB", "EB", "ZB", "YB"}
	i := int(math.Log10(float64(size)) / 3)
	return fmt.Sprintf("%.*f %s", i, float64(size)/math.Pow10(3*i), sizes[i])
}
