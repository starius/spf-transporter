package siamux

import (
	"context"
	"math"

	"gitlab.com/NebulousLabs/errors"
	"gitlab.com/NebulousLabs/fastrand"
	"gitlab.com/NebulousLabs/log"
	"gitlab.com/NebulousLabs/siamux/mux"
)

// CompatV1421NewWithKeyPair is like New but will use the provided keys instead
// of generated or existing ones. For safety this should only be called the
// first time after upgrading a node since it will always use the provided keys
// instead of the existing ones.
func CompatV1421NewWithKeyPair(tcpAddr, wsAddr string, log *log.Logger, persistDir string, privKey mux.ED25519SecretKey, pubKey mux.ED25519PublicKey) (*SiaMux, error) {
	// Create a client since it's the same as creating a server.
	ctx, cancel := context.WithCancel(context.Background())
	sm := &SiaMux{
		staticAppSeed:    appSeed(fastrand.Uint64n(math.MaxUint64)),
		staticLog:        log,
		handlers:         make(map[string]*Handler),
		muxs:             make(map[appSeed]*mux.Mux),
		outgoingMuxs:     make(map[string]*outgoingMux),
		muxSet:           make(map[*mux.Mux]muxInfo),
		staticPersistDir: persistDir,
		staticCtx:        ctx,
		staticCtxCancel:  cancel,
	}
	// Init the persistence.
	if err := sm.initPersist(); err != nil {
		return nil, errors.AddContext(err, "failed to initialize SiaMux persistence")
	}
	// Overwrite the keys.
	sm.staticPubKey = pubKey
	sm.staticPrivKey = privKey
	if err := sm.savePersist(); err != nil {
		return nil, errors.AddContext(err, "failed to override keys")
	}
	// Spawn the listening thread.
	err := sm.spawnListeners(tcpAddr, wsAddr)
	if err != nil {
		return nil, errors.AddContext(err, "unable to spawn listener for compat siamux startup")
	}
	return sm, nil
}
