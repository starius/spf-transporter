package siamux

import (
	"os"
	"path/filepath"

	"gitlab.com/NebulousLabs/errors"
	"gitlab.com/NebulousLabs/persist"
	"gitlab.com/NebulousLabs/siamux/mux"
)

const (
	// persistDirPerms are the permissions used when creating the persist dir of
	// the SiaMux.
	persistDirPerms = 0700
	// persistFilePerms are the permission used when creating the metadata
	// persist file.
	persistFilePerms = 0600
	// settingsName is the name of the file which stores the siamux settings
	// a.k.a. the persistence struct.
	settingsName = "siamux.json"
)

var (
	// persistMetadata is the metadata written to disk when persisting the
	// SiaMux.
	persistMetadata = persist.Metadata{
		Header:  "SiaMux",
		Version: "1.4.2.1",
	}
)

type (
	// persistence is the data persisted to disk by the SiaMux.
	persistence struct {
		PubKey  mux.ED25519PublicKey `json:"pubkey"`
		PrivKey mux.ED25519SecretKey `json:"privkey"`
	}
)

// initPersist loads the persistence of the SiaMux or creates a new one with
// fresh keys in case it doesn't exist yet.
func (sm *SiaMux) initPersist() error {
	// Create the persist dir.
	if err := os.MkdirAll(sm.staticPersistDir, persistDirPerms); err != nil {
		return errors.AddContext(err, "failed to create SiaMux persist dir")
	}
	// Get the filepath.
	path := filepath.Join(sm.staticPersistDir, settingsName)
	// Load the persistence object
	var data persistence
	err := persist.LoadJSON(persistMetadata, &data, path)
	if os.IsNotExist(err) {
		// If the data isn't persisted yet we create new keys and persist them.
		privKey, pubKey := mux.GenerateED25519KeyPair()
		data.PrivKey = privKey
		data.PubKey = pubKey
		if err = persist.SaveJSON(persistMetadata, data, path); err != nil {
			return errors.AddContext(err, "failed to initialize fresh persistence")
		}
		if err := os.Chmod(path, persistFilePerms); err != nil {
			return errors.AddContext(err, "failed to set the mode of the SiaMux metadata persist file")
		}
	}
	if err != nil {
		return errors.AddContext(err, "failed to load persistence data from disk")
	}
	// Set the fields in the SiaMux
	sm.staticPrivKey = data.PrivKey
	sm.staticPubKey = data.PubKey
	return nil
}

// persistData creates a persistence object from the SiaMux.
func (sm *SiaMux) persistData() persistence {
	return persistence{
		PubKey:  sm.staticPubKey,
		PrivKey: sm.staticPrivKey,
	}
}

// savePersist writes the persisted fields of the SiaMux to disk.
func (sm *SiaMux) savePersist() error {
	path := filepath.Join(sm.staticPersistDir, settingsName)
	return errors.AddContext(persist.SaveJSON(persistMetadata, sm.persistData(), path), "failed to save persistence data")
}
