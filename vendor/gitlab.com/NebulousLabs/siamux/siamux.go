package siamux

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"math"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"gitlab.com/NebulousLabs/errors"
	"gitlab.com/NebulousLabs/fastrand"
	"gitlab.com/NebulousLabs/log"
	"gitlab.com/NebulousLabs/siamux/mux"
)

const (
	// DefaultNewStreamTimeout defines the amount of time that a stream is given
	// as a default timeout.
	DefaultNewStreamTimeout = time.Minute * 5
)

// ErrAlreadyShuttingDown is returned if the caller is trying to establish a new
// connection on a siamux that was already shut down.
var ErrAlreadyShuttingDown = errors.New("can't establish a new siamux connection since it's shutting down")

// TODO: Some features/tests that came to mind which we need
// - auto connect on siamux creation
type (
	// SiaMux needs a docstring
	SiaMux struct {
		handlers   map[string]*Handler // registered handlers for incoming connections
		handlersMu sync.RWMutex

		outgoingMuxs  map[string]*outgoingMux // maps addresses of outgoing connection to the mux that is wrapping them
		muxs          map[appSeed]*mux.Mux    // all incoming and outgoing muxs
		muxSet        map[*mux.Mux]muxInfo    // control set to confirm that every mux only has a single seed associated to it
		staticAppSeed appSeed                 // random appSeed to uniquely identify application
		staticPubKey  mux.ED25519PublicKey
		staticPrivKey mux.ED25519SecretKey

		// fields related to TCP
		staticListener net.Listener // listener used to listen for incoming SiaMux requests

		// fields related to Websocket
		staticURL string

		// utilities
		staticLog        *log.Logger
		staticMu         sync.Mutex
		staticPersistDir string
		staticWG         sync.WaitGroup

		staticCtx          context.Context
		staticCtxCancel    context.CancelFunc
		staticShutdownChan chan struct{}
	}
	// Stream is the interface for a Stream returned by the SiaMux. It's a
	// net.Conn with a few exceptions.
	Stream interface {
		// A stream implements net.Conn.
		net.Conn

		// Limit gets the limit on the stream.
		Limit() mux.BandwidthLimit

		// Mux returns the stream's underlying mux.
		Mux() *mux.Mux

		// SetLimit sets a custom limit on the stream and initiates it to continue from
		// where the last limit left off.
		SetLimit(limit mux.BandwidthLimit) error

		// SetPriority allows for prioritizing individual streams over others.
		SetPriority(int) error
	}

	muxInfo struct {
		addresses []string
		appSeed   appSeed
	}

	// blockedStream is a wrapper around the mux.Stream which blocks Read
	// operations until the underlying channel is closed. It is used in
	// NewStream to block the user from reading from the stream before the
	// subscription response is received.
	blockedStream struct {
		// The underlying stream.
		*mux.Stream

		// errAvail is a channel which blocks until the error has been written
		// by the background process. For the blockedStream, all read calls are
		// blocked until errAvail has been closed.
		//
		// externErr is an error which can only be set by the background thread
		// waiting for the response from the peer on whether this stream has a
		// valid subscriber. If the err is 'nil' after errAvail is closed, this
		// means the stream is good.
		once      sync.Once
		errAvail  chan struct{}
		externErr error

		// The mux and SiaMux that spawned the blockedStream.
		m  *outgoingMux
		sm *SiaMux
	}

	// outgoingMux is a wrapper for a mux.Mux which contains additional
	// information for outgoing muxs.
	outgoingMux struct {
		// ephemeral indicates whether the mux is supposed to be closed when all
		// of its streams are closed.
		// It's managed by the SiaMux mutex.
		ephemeral bool
		streams   int64

		*mux.Mux
	}
)

// newOutgoingMux creates a new outgoingMux.
func newOutgoingMux(nStreams int64, ephemeral bool, m *mux.Mux) *outgoingMux {
	return &outgoingMux{
		streams:   nStreams,
		ephemeral: ephemeral,
		Mux:       m,
	}
}

// Close overrides the stream's Close by also closing the corresponding mux if
// applicable.
func (bs *blockedStream) Close() error {
	// Close the stream.
	err := bs.Stream.Close()

	// Close the mux in case it's ephemeral.
	err = errors.Compose(err, bs.sm.managedCloseEphemeralMux(bs.m))

	// Unblock the initial read.
	bs.once.Do(func() {
		close(bs.errAvail)
	})
	return err
}

// Read will block until `errAvail` is closed. The first time Read is called
// after creating the stream it will return the result of the subscription
// response or an error which occurred during the opening of the stream. If that
// err is != nil the underlying stream is already closed.
func (bs *blockedStream) Read(b []byte) (int, error) {
	// Block all reads until the errAvail chan has been closed.
	<-bs.errAvail
	if bs.externErr != nil {
		return 0, bs.externErr
	}

	// Pass the read through to the stream.
	return bs.Stream.Read(b)
}

// Write will pass calls through to the underlying stream's Write method. If
// writing to the stream fails, it will also return externErr which is set by
// the first call to Read on the blockedStream.
func (bs *blockedStream) Write(b []byte) (int, error) {
	n, err := bs.Stream.Write(b)
	if err == nil {
		return n, nil
	}
	// Extend the error with externErr if possible.
	select {
	case <-bs.errAvail:
		return n, errors.Compose(err, bs.externErr)
	default:
	}
	return n, err
}

// trackStream updates the ephemeral field of the outgoing mux. A mux can
// go from ephemeral to not ephemeral but not the other way round. Since
// mux.ephemeral is protected by the SiaMux mutex, trackStream needs to be
// called while sm.mu is being held. It also increments the connection counter.
func (mux *outgoingMux) trackStream(ephemeral bool) {
	mux.ephemeral = mux.ephemeral && ephemeral
	mux.streams++
}

// New creates a new SiaMux which listens on 'address' for incoming connections.
// pubKey and privKey will be used to sign the message during the initial
// handshake for incoming connections.
func New(tcpAddr, wsAddr string, log *log.Logger, persistDir string) (*SiaMux, error) {
	// Create a client since it's the same as creating a server.
	ctx, cancel := context.WithCancel(context.Background())
	sm := &SiaMux{
		staticAppSeed:      appSeed(fastrand.Uint64n(math.MaxUint64)),
		staticLog:          log,
		handlers:           make(map[string]*Handler),
		muxs:               make(map[appSeed]*mux.Mux),
		outgoingMuxs:       make(map[string]*outgoingMux),
		muxSet:             make(map[*mux.Mux]muxInfo),
		staticPersistDir:   persistDir,
		staticCtx:          ctx,
		staticCtxCancel:    cancel,
		staticShutdownChan: make(chan struct{}),
	}
	// Init the persistence.
	if err := sm.initPersist(); err != nil {
		return nil, errors.AddContext(err, "failed to initialize SiaMux persistence")
	}
	// Spawn the listening thread.
	err := sm.spawnListeners(tcpAddr, wsAddr)
	if err != nil {
		return nil, errors.AddContext(err, "unable to spawn listener for siamux")
	}
	// Open the ports using UPnP.
	sm.managedForwardPorts(tcpAddr, wsAddr)

	sm.staticLog.Printf("SiaMux creation successful: PubKey '%v'", sm.staticPubKey)
	return sm, nil
}

// Address returns the underlying TCP listener's address.
func (sm *SiaMux) Address() net.Addr {
	return sm.staticListener.Addr()
}

// URL returns the underlying webserver's URL.
func (sm *SiaMux) URL() string {
	return sm.staticURL
}

// PublicKey returns the siamux's public key.
func (sm *SiaMux) PublicKey() mux.ED25519PublicKey {
	return sm.staticPubKey
}

// PrivateKey returns the siamux's private key.
func (sm *SiaMux) PrivateKey() mux.ED25519SecretKey {
	return sm.staticPrivKey
}

// Close closes the mux and waits for background threads to finish.
func (sm *SiaMux) Close() error {
	sm.managedClearPorts()
	sm.staticCtxCancel()
	sm.staticWG.Wait()
	return nil
}

// Shutdown will inititate a graceful shutdown on the siamux. This means it will
// prevent new streams from being established. So the caller can call Shutdown
// first, wait for an appropriate amount of time and finally call Close which
// actually interrupts the SiaMux and closes all the network connections.
func (sm *SiaMux) Shutdown() {
	close(sm.staticShutdownChan)
}

// NewStream connects to an address if not yet connected and opens a new
// stream to the given subscriber.
func (sm *SiaMux) NewStream(subscriber, address string, expectedPubKey mux.ED25519PublicKey) (Stream, error) {
	return sm.NewStreamTimeout(subscriber, address, DefaultNewStreamTimeout, expectedPubKey)
}

// NewStreamTimeout connects to an address if not yet connected and opens a new
// stream to the given subscriber. In case a new TCP connection needs to be
// established, the dialing process will time out after the specified timeout.
// The timeout is also used as the initial deadline for the stream. That means
// after stream creation, a new timeout should be set.
func (sm *SiaMux) NewStreamTimeout(subscriber, address string, timeout time.Duration, expectedPubKey mux.ED25519PublicKey) (Stream, error) {
	return sm.managedNewStreamTimeout(subscriber, address, timeout, expectedPubKey, false)
}

// NewEphemeralStream has the same behaviour as NewStreamTimeout, but it will
// return a special type of Stream which tries to close its underlying mux when
// its closed. The mux will only be closed if it's not in use by other streams
// right now.
func (sm *SiaMux) NewEphemeralStream(subscriber, address string, timeout time.Duration, expectedPubKey mux.ED25519PublicKey) (Stream, error) {
	return sm.managedNewStreamTimeout(subscriber, address, timeout, expectedPubKey, true)
}

// NewResponseStream creates a new stream using the provided incoming stream.
func (sm *SiaMux) NewResponseStream(subscriber string, timeout time.Duration, stream Stream) (Stream, error) {
	om := newOutgoingMux(1, false, stream.Mux()) // 1 stream, not ephemeral
	return sm.managedNewStreamFromMux(subscriber, timeout, om)
}

// managedNewStreamFromMux opens a new stream to the given subscriber using the
// provided mux.
func (sm *SiaMux) managedNewStreamFromMux(subscriber string, timeout time.Duration, m *outgoingMux) (Stream, error) {
	// Check if the subscriber is an empty string. It should be valid to
	// subscribe to the empty string but to avoid developer errors we check for
	// it here on the "client" side.
	if subscriber == "" {
		return nil, errors.New("subscriber can't be empty string")
	}

	// Create a new stream to subscribe.
	stream, err := m.NewStream()
	if err != nil {
		return nil, errors.AddContext(err, "unable to make a new outgoing stream")
	}
	// Set the timeout on the stream as a deadline. That way all the
	// initialization we do on the stream can time out.
	if timeout > 0 {
		err = stream.SetDeadline(time.Now().Add(timeout))
		if err != nil {
			return nil, errors.AddContext(err, "unable to set stream deadline")
		}
	}
	// Send the subscriber request to tell the other SiaMux what name we want to
	// subscribe to.
	srb := bytes.NewBuffer(nil)
	err = writeSubscriberRequest(srb, subscriber)
	if err != nil {
		err = errors.AddContext(err, "unable to write subscriber request")
		return nil, errors.Compose(err, stream.Close())
	}
	stream.LazyWrite(srb.Bytes())
	// Read the response from the other SiaMux.
	bs := &blockedStream{
		Stream: stream,

		errAvail: make(chan struct{}),
		m:        m,
		sm:       sm,
	}
	go func() {
		defer bs.once.Do(func() { close(bs.errAvail) })
		// Even if an error occurs here, we rely on the caller to close the
		// stream. That's because `managedNewStreamTimeout` will return a nil
		// error regardless and the caller is responsible for closing a resource
		// if the opening method returns no error.
		subscribeErr, err := readSubscriberResponse(stream)
		if err != nil {
			bs.externErr = errors.AddContext(err, "unable to read subscriber response")
			return
		}
		if subscribeErr != nil {
			bs.externErr = errors.AddContext(subscribeErr, "subscriber error reported by peer")
			return
		}
	}()
	return bs, nil
}

// managedNewStreamTimeout connects to an address if not yet connected and opens
// a new stream to the given subscriber. In case a new TCP connection needs to
// be established, the dialing process will time out after the specified
// timeout.
func (sm *SiaMux) managedNewStreamTimeout(subscriber, address string, timeout time.Duration, expectedPubKey mux.ED25519PublicKey, ephemeral bool) (Stream, error) {
	if sm.staticIsShutDown() {
		return nil, ErrAlreadyShuttingDown
	}
	// Check if the subscriber is an empty string. It should be valid to
	// subscribe to the empty string but to avoid developer errors we check for
	// it here on the "client" side.
	if subscriber == "" {
		return nil, errors.New("subscriber can't be empty string")
	}

	// Check if an outgoing mux exists already.
	sm.staticMu.Lock()
	m, exists := sm.outgoingMuxs[address]
	// If it does we update its ephemeral status.
	if exists {
		m.trackStream(ephemeral)
	}
	sm.staticMu.Unlock()
	// If it doesn't we create a new one.
	var err error
	if !exists {
		m, err = sm.managedNewOutgoingMux(address, timeout, expectedPubKey, ephemeral)
		if err != nil {
			return nil, errors.AddContext(err, "unable to make a new outgoing mux")
		}
	}
	return sm.managedNewStreamFromMux(subscriber, timeout, m)
}

// addMux adds a mux to all of the SiaMuxs data structures.
func (sm *SiaMux) addMux(mux *mux.Mux, appSeed appSeed, addresses []string) {
	sm.muxSet[mux] = muxInfo{
		addresses: addresses,
		appSeed:   appSeed,
	}
	sm.muxs[appSeed] = mux
}

// addOutgoingMux adds a mux to all of the SiaMuxs data structures.
func (sm *SiaMux) addOutgoingMux(mux *outgoingMux, appSeed appSeed, addresses []string) {
	// Add the mux to the outgoing mux map.
	for _, address := range addresses {
		sm.outgoingMuxs[address] = mux
	}
	// Add it to the remaining fields.
	sm.addMux(mux.Mux, appSeed, addresses)
}

// managedCloseEphemeralMux closes a mux if its idle and ephemeral.
func (sm *SiaMux) managedCloseEphemeralMux(mux *outgoingMux) error {
	sm.staticMu.Lock()

	// Decrement the number of connections.
	mux.streams--

	// If the mux is not ephemeral or idle don't close it.
	if !mux.ephemeral || mux.streams > 0 || !mux.Idle() {
		sm.staticMu.Unlock()
		return nil
	}

	// We want to close it. Remove it from the SiaMux first.
	sm.removeMux(mux.Mux)
	sm.staticMu.Unlock()

	return errors.AddContext(mux.Close(), "failed to close ephemeral mux")
}

// managedMuxCallback is called whenever one of the SiaMux's muxs is closed for
// any reason. It will handle necessary cleanup for the SiaMux.
func (sm *SiaMux) managedMuxCallback(mux *mux.Mux) {
	sm.managedRemoveMux(mux)
}

// managedTimeoutCallback is called whenever one of the SiaMux's muxs is close
// to timing out. This should check if a mux is worth keeping alive and then
// send a manual keepalive signal.
func (sm *SiaMux) managedTimeoutCallback(mux *mux.Mux) {
	sm.staticMu.Lock()
	mi, exists := sm.muxSet[mux]
	sm.staticMu.Unlock()
	if !exists {
		return // mux was removed in meantime
	}
	if len(mi.addresses) == 0 {
		return // not outgoing mux
	}
	if err := mux.Keepalive(); err != nil {
		err = errors.Compose(err, mux.Close())
		sm.staticLog.Print("managedTimeoutCallback: failed to send keepalive", err)
		return
	}
}

// managedNewOutgoingMux creates a new mux which is connected to address. For
// the sake of atomicity, the mux already starts with the internal connections
// counter set to 1 if a new mux is returned and otherwise increments the
// counter by 1 before returning an existing mux.
func (sm *SiaMux) managedNewOutgoingMux(address string, timeout time.Duration, expectedPubKey mux.ED25519PublicKey, ephemeral bool) (_ *outgoingMux, err error) {
	// If not, establish a new connection and wrap it in a mux.
	var conn net.Conn
	conn, err = dial(sm.staticCtx, address, timeout)
	if err != nil {
		return nil, errors.AddContext(err, "unable to dial peer")
	}
	defer func() {
		if err != nil {
			err = errors.Compose(err, conn.Close())
		}
	}()
	// Wrap the connection in a mux.
	var m *mux.Mux
	m, err = mux.NewClientMux(sm.staticCtx, conn, expectedPubKey, sm.staticLog, sm.managedMuxCallback, sm.managedTimeoutCallback, sm.staticShutdownChan)
	if err != nil {
		return nil, errors.AddContext(err, "unable to create new client mux")
	}
	defer func() {
		if err != nil {
			err = errors.Compose(err, m.Close())
		}
	}()
	// Create a new stream to send the seed.
	stream, err := m.NewStream()
	if err != nil {
		return nil, errors.AddContext(err, "unable to create new stream")
	}
	defer func() {
		err = errors.Compose(err, stream.Close())
	}()
	// Set the timeout on the stream as a deadline. That way all the
	// initialization we do on the stream can time out.
	if timeout > 0 {
		err = stream.SetDeadline(time.Now().Add(timeout))
		if err != nil {
			return nil, errors.AddContext(err, "unable to set stream deadline")
		}
	}
	// Derive an ephemeral seed for the peer.
	ephemeralSeed, err := deriveEphemeralAppSeed(sm.staticAppSeed, conn.RemoteAddr())
	if err != nil {
		return nil, errors.AddContext(err, "unable to derive ephemeral app seed")
	}
	// Send the seed request.
	err = writeSeedRequest(stream, ephemeralSeed)
	if err != nil {
		return nil, errors.AddContext(err, "unable to write seed request")
	}
	// Receive the seed response.
	peerSeed, err := readSeedResponse(stream)
	if errors.Contains(err, io.ErrClosedPipe) {
		// If the pipe was closed this could mean that the mux was rejected for
		// an existing one. Check if we have an existing one we can return.
		om, exists := sm.managedExistingMux(address, ephemeral)
		if exists {
			_ = m.Close()
			return om, nil
		}
		return nil, err
	}
	if err != nil {
		return nil, errors.AddContext(err, "unable to read seed response")
	}
	appSeed := ephemeralSeed + peerSeed

	// Wrap the mux in an outgoingMux.
	om := newOutgoingMux(1, ephemeral, m)

	// To prevent a race, check if another thread was faster at creating an
	// outgoing mux.
	sm.staticMu.Lock()
	om2, exists := sm.existingMux(address, ephemeral)
	if exists {
		// If another thread was faster, return the existing mux and close the
		// newly created one. Update the returned ones stream count.
		sm.staticMu.Unlock()
		_ = om.Close()
		return om2, nil
	}

	// If no other thread was faster, add the mux.
	sm.addOutgoingMux(om, appSeed, []string{address})
	sm.staticMu.Unlock()

	// Start accepting streams on that mux.
	sm.staticWG.Add(1)
	go func() {
		sm.threadedAccept(m)
		sm.staticWG.Done()
	}()
	return om, nil
}

// managedRemoveMux removes a mux from all of the SiaMuxs data structures.
func (sm *SiaMux) managedRemoveMux(mux *mux.Mux) {
	sm.staticMu.Lock()
	defer sm.staticMu.Unlock()
	sm.removeMux(mux)
}

// removeMux removes a mux from all of the SiaMuxs data structures.
func (sm *SiaMux) removeMux(mux *mux.Mux) {
	muxInfo, exists := sm.muxSet[mux]
	if !exists {
		sm.staticLog.Debug("WARN: tried to remove non-existent mux")
		return // mux wasn't added to the map yet.
	}
	delete(sm.muxSet, mux)
	delete(sm.muxs, muxInfo.appSeed)
	for _, address := range muxInfo.addresses {
		delete(sm.outgoingMuxs, address)
	}
}

// managedUpgradeConn upgrades an incoming net.Conn to a mux and adds it to the
// SiaMux.
func (sm *SiaMux) managedUpgradeConn(conn net.Conn) (err error) {
	// Upgrade the connection using a mux.
	mux, err := mux.NewServerMux(sm.staticCtx, conn, sm.staticPubKey, sm.staticPrivKey, sm.staticLog, sm.managedMuxCallback, sm.managedTimeoutCallback, sm.staticShutdownChan)
	if err != nil {
		sm.staticLog.Print("threadedListen: failed to create server mux", err)
		return
	}
	defer func() {
		if err != nil {
			err = errors.Compose(err, mux.Close())
		}
	}()
	// Get the first stream which contains the seed.
	seedStream, err := mux.AcceptStream()
	if err != nil {
		sm.staticLog.Print("threadedListen: failed to accept seed stream", err)
		return
	}
	defer func() {
		err = errors.Compose(err, seedStream.Close())
	}()
	// Read the seed request and derive an ephemeral seed.
	peerSeed, err := readSeedRequest(seedStream)
	if err != nil {
		sm.staticLog.Print("threadedListen: failed to read seed request", err)
		return
	}
	ephemeralSeed, err := deriveEphemeralAppSeed(sm.staticAppSeed, conn.RemoteAddr())
	if err != nil {
		sm.staticLog.Print("threadedListen: failed to derive shared seed", err)
		return
	}
	appSeed := ephemeralSeed + peerSeed

	// Check if the seed exists already. If it does, we reject the connection.
	sm.staticMu.Lock()
	existingMux, exists := sm.muxs[appSeed]
	if exists {
		println("exists")
		sm.staticLog.Print("threadedListen: existing mux with same appSeed found, replacing it with new one")
		sm.removeMux(existingMux)

		// Since we are holding a lock and since closing the existing
		// mux is blocking, do it in a separate goroutine.
		go func() {
			if err := existingMux.Close(); err != nil {
				sm.staticLog.Print("threadedListen: failed to close existing mux", err)
			}
		}()
	}
	sm.addMux(mux, appSeed, nil)
	sm.staticMu.Unlock()

	// Write response.
	err = writeSeedResponse(seedStream, ephemeralSeed)
	if err != nil {
		sm.staticLog.Print("threadedListen: failed to write seed response", err)
		return
	}

	// Spawn a thread to start listening on the mux.
	sm.staticWG.Add(1)
	go func() {
		sm.threadedAccept(mux)
		sm.staticWG.Done()
	}()
	return
}

// threadedHandleTCP starts listening for incoming TCP connections which are
// then upgraded using the mux.Mux.
func (sm *SiaMux) threadedHandleTCP(listener net.Listener) {
	for {
		// Accept new connection.
		conn, err := listener.Accept()
		if err != nil {
			// shutdown
			break
		}

		// If we are shutting down we don't accept new connections. We
		// still don't close the listener because we want to give
		// existing connections some time to finish.
		if sm.staticIsShutDown() {
			// shutdown
			_ = conn.Close()
			break
		}

		// Upgrade the connection using a mux.
		err = sm.managedUpgradeConn(conn)
		if err != nil {
			err = errors.Compose(err, conn.Close())
			sm.staticLog.Printf("failed to upgrade conn: %v", err)
			continue
		}
	}
}

// handleWS handles incoming websocket connections by upgrading them to a
// mux.Mux.
func (sm *SiaMux) handleWS(w http.ResponseWriter, req *http.Request) {
	upgrader := websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
		CheckOrigin: func(r *http.Request) bool {
			return true
		},
	}
	conn, err := upgrader.Upgrade(w, req, nil)
	if err != nil {
		return
	}

	// Upgrade the connection using a mux.
	err = sm.managedUpgradeConn(mux.NewWSConn(conn))
	if err != nil {
		err = errors.Compose(err, conn.Close())
		sm.staticLog.Printf("failed to upgrade wsconn: %v", err)
		return
	}
}

// deriveEphemeralAppSeed derives an ephemeral appSeed for a peer.
func deriveEphemeralAppSeed(seed appSeed, address net.Addr) (appSeed, error) {
	host, _, err := net.SplitHostPort(address.String())
	if err != nil {
		return 0, err
	}
	rawSeed := HashAll(seed, host)
	return appSeed(binary.LittleEndian.Uint64(rawSeed[:])), nil
}

// dial is a helper method to dial a peer. It supports dialing websockets or raw
// tcp ports.
func dial(ctx context.Context, address string, timeout time.Duration) (net.Conn, error) {
	// Derive timeout context if necessary.
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	// Check for Websocket url.
	if strings.HasPrefix(address, "ws://") || strings.HasPrefix(address, "wss://") {
		c, _, err := websocket.DefaultDialer.DialContext(ctx, address, nil)
		if err != nil {
			return nil, errors.AddContext(err, "failed to dial websocket")
		}
		return mux.NewWSConn(c), nil
	}
	// Default to TCP.
	var dialer net.Dialer
	conn, err := dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		return nil, errors.AddContext(err, "failed to dial tcp port")
	}
	return conn, nil
}

// existingMux retrieves an existing outgoing mux and increments the stream
// count atomically.
func (sm *SiaMux) existingMux(address string, ephemeral bool) (*outgoingMux, bool) {
	om, exists := sm.outgoingMuxs[address]
	if exists {
		om.trackStream(ephemeral)
		return om, true
	}
	return nil, false
}

// managedExistingMux retrieves an existing outgoing mux and increments the
// stream count atomically.
func (sm *SiaMux) managedExistingMux(address string, ephemeral bool) (*outgoingMux, bool) {
	sm.staticMu.Lock()
	defer sm.staticMu.Unlock()
	return sm.existingMux(address, ephemeral)
}

// staticIsShutDown returns true if a siamux shutdown is in progress.
func (sm *SiaMux) staticIsShutDown() bool {
	select {
	case <-sm.staticShutdownChan:
		return true
	default:
	}
	return false
}
