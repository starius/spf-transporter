package build

import (
	"encoding/hex"
	"io/ioutil"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"gitlab.com/NebulousLabs/fastrand"
)

// APIPassword returns the API Password either from the environment variable
// or from the password file. If no environment variable is set and no file
// exists, a password file is created and that password is returned
func APIPassword() (string, error) {
	// Check the environment variable.
	pw := os.Getenv(EnvvarAPIPassword)
	if pw != "" {
		return pw, nil
	}

	// Try to read the password from disk.
	path := apiPasswordFilePath()
	pwFile, err := ioutil.ReadFile(path)
	if err == nil {
		// This is the "normal" case, so don't print anything.
		return strings.TrimSpace(string(pwFile)), nil
	} else if !os.IsNotExist(err) {
		return "", err
	}

	// No password file; generate a secure one.
	// Generate a password file.
	pw, err = createAPIPasswordFile()
	if err != nil {
		return "", err
	}
	return pw, nil
}

// SiadDataDir returns the data directory from the
// environment variable. If there is no environment variable it returns an empty
// string, instructing spd to store the consensus in the current directory.
func SiadDataDir() string {
	// instead of return os.Getenv(EnvvarDaemonDataDir)
	return SiaDir()
}

// SiaDir returns the ScPrime data directory either from the environment variable or
// the default.
func SiaDir() string {
	dataDir := os.Getenv(EnvvarMetaDataDir)
	if dataDir == "" {
		dataDir = DefaultMetadataDir()
	}
	return dataDir
}

// WalletPassword returns the SiaWalletPassword environment variable.
func WalletPassword() string {
	return os.Getenv(EnvvarWalletPassword)
}

// apiPasswordFilePath returns the path to the API's password file. The password
// file is stored in the ScPrime data directory.
func apiPasswordFilePath() string {
	return filepath.Join(SiaDir(), "apipassword")
}

// createAPIPasswordFile creates an api password file in the ScPrime data directory
// and returns the newly created password
func createAPIPasswordFile() (string, error) {
	err := os.MkdirAll(SiaDir(), 0700)
	if err != nil {
		return "", err
	}
	// Ensure SiaDir has the correct mode as MkdirAll won't change the mode of
	// an existent directory. We specifically use 0700 in order to prevent
	// potential attackers from accessing the sensitive information inside, both
	// by reading the contents of the directory and/or by creating files with
	// specific names which spd would later on read from and/or write to.
	err = os.Chmod(SiaDir(), 0700)
	if err != nil {
		return "", err
	}
	pw := hex.EncodeToString(fastrand.Bytes(16))
	err = ioutil.WriteFile(apiPasswordFilePath(), []byte(pw+"\n"), 0600)
	if err != nil {
		return "", err
	}
	return pw, nil
}

// DefaultMetadataDir returns the default data directory of spd. The values for
// supported operating systems are:
//
// Linux:   $HOME/.scprime
// MacOS:   $HOME/Library/Application Support/ScPrime
// Windows: %LOCALAPPDATA%\ScPrime
func DefaultMetadataDir() string {
	switch runtime.GOOS {
	case "windows":
		return filepath.Join(os.Getenv("LOCALAPPDATA"), "ScPrime")
	case "darwin":
		return filepath.Join(os.Getenv("HOME"), "Library", "Application Support", "ScPrime")
	default:
		return filepath.Join(os.Getenv("HOME"), ".scprime")
	}
}

// DefaultSiaPrimeDir returns the default data directory of older ScPrime nodes.
// This method is used to migrate the metadata to the new default location.
// The values for supported operating systems are:
//
// Linux:   $HOME/.siaprime
// MacOS:   $HOME/Library/Application Support/SiaPrime
// Windows: %LOCALAPPDATA%\SiaPrime
func DefaultSiaPrimeDir() string {
	switch runtime.GOOS {
	case "windows":
		return filepath.Join(os.Getenv("LOCALAPPDATA"), "SiaPrime")
	case "darwin":
		return filepath.Join(os.Getenv("HOME"), "Library", "Application Support", "SiaPrime")
	default:
		return filepath.Join(os.Getenv("HOME"), ".siaprime")
	}
}
