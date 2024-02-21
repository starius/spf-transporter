package siamux

import (
	"bytes"
	"container/list"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"sync"
	"time"

	"gitlab.com/NebulousLabs/errors"
	"gitlab.com/NebulousLabs/log"
	"gitlab.com/NebulousLabs/siamux/build"
	"gitlab.com/NebulousLabs/siamux/mux"
	"gitlab.com/NebulousLabs/threadgroup"
)

type (
	// Handler is a type containing a method that gets called whenever a new
	// stream is accepted and a waitgroup that keeps track of handlers currently
	// being executed.
	Handler struct {
		staticFunc HandlerFunc
		staticLog  *log.Logger
		staticTG   threadgroup.ThreadGroup

		// staticQueue is used to guarantee the order the streams are executed
		// in. It has its own mutex.
		staticQueue *list.List
		queueMu     sync.Mutex

		// staticSerial indicates whether streams should be handled in series or
		// in parallel.
		staticSerial bool

		// serialMu is used to ensure that serial streams are executed in
		// series. It doesn't protect any specific fields.
		serialMu sync.Mutex
	}

	// HandlerFunc is a function that gets called whenever a stream is accepted.
	HandlerFunc func(stream Stream)
)

// managedQueueStream adds the stream to the handler's internal queue.
func (h *Handler) managedQueueStream(stream *mux.Stream) {
	h.queueMu.Lock()
	h.staticQueue.PushBack(stream)
	h.queueMu.Unlock()
}

// threadedHandleNext fetches the next stream from the queue and handles it.
func (h *Handler) threadedHandleNext() {
	if err := h.staticTG.Add(); err != nil {
		return
	}
	defer h.staticTG.Done()

	// If we want to handle these streams in series, grab the lock.
	if h.staticSerial {
		h.serialMu.Lock()
		defer h.serialMu.Unlock()
	}

	// Get the stream to handle.
	h.queueMu.Lock()
	next := h.staticQueue.Front()
	if next == nil {
		h.queueMu.Unlock()
		build.Critical("no stream to handle - did you forget to call managedQueueStream?")
		return
	}
	_ = h.staticQueue.Remove(next)
	h.queueMu.Unlock()
	// Cast stream.
	stream, ok := next.Value.(*mux.Stream)
	if !ok {
		build.Critical("failed to case stream from queue")
		return
	}
	h.staticFunc(stream)
}

// staticStopped returns 'true' if Close has been called on the SiaMux before.
func (sm *SiaMux) staticStopped() bool {
	select {
	case <-sm.staticCtx.Done():
		return true
	default:
		return false
	}
}

// NewListener returns a new listener for a given subscriber name.
func (sm *SiaMux) NewListener(subscriber string, handler HandlerFunc) error {
	return sm.managedNewListener(subscriber, handler, false)
}

// NewListenerSerial returns a new listener for a given subscriber name which
// guarantees that handlers are called in series instead of in parallel.
func (sm *SiaMux) NewListenerSerial(subscriber string, handler HandlerFunc) error {
	return sm.managedNewListener(subscriber, handler, true)
}

// managedNewListener returns a new listener for a given subscriber name.
func (sm *SiaMux) managedNewListener(subscriber string, handler HandlerFunc, serial bool) error {
	if sm.staticStopped() {
		return errors.New("SiaMux has already been closed")
	}
	sm.handlersMu.Lock()
	defer sm.handlersMu.Unlock()
	// Check if handler already exists.
	_, exists := sm.handlers[subscriber]
	if exists {
		return fmt.Errorf("handler for subscriber %v already registered", subscriber)
	}
	// Register the handler.
	sm.handlers[subscriber] = &Handler{
		staticFunc:   handler,
		staticQueue:  list.New(),
		staticLog:    sm.staticLog,
		staticSerial: serial,
	}
	return nil
}

// CloseListener will close a previously registered listener, causing incoming
// streams for that listener to be dropped with an error.
func (sm *SiaMux) CloseListener(subscriber string) error {
	sm.handlersMu.Lock()
	handler, exists := sm.handlers[subscriber]
	if !exists {
		sm.handlersMu.Unlock()
		return fmt.Errorf("handler for subscriber %v doesn't exist", subscriber)
	}
	// Remove handler.
	delete(sm.handlers, subscriber)
	sm.handlersMu.Unlock()
	// Wait for running handlers to return.
	return handler.staticTG.Stop()
}

// spawnListeners will spawn a tcp listener and a minimal http webserver which
// listens for websocket connections. Both types of connections are then
// upgraded using the mux.Mux.
func (sm *SiaMux) spawnListeners(tcpAddr, wsAddr string) error {
	err1 := sm.spawnListenerTCP(tcpAddr)
	err2 := sm.spawnListenerWS(wsAddr)
	return errors.Compose(err1, err2)
}

// spawnListenerTCP spawns a listener which listens for raw TCP connections and
// upgrades them using the mux.Mux.
func (sm *SiaMux) spawnListenerTCP(address string) error {
	// Listen on the specified address for incoming connections.
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return errors.AddContext(err, "unable to create listener")
	}
	sm.staticListener = listener
	// Spawn the listening thread.
	sm.staticWG.Add(1)
	go func() {
		sm.threadedHandleTCP(listener)
		sm.staticWG.Done()
	}()
	// Spawn a thread to close the listener.
	sm.staticWG.Add(1)
	go func() {
		<-sm.staticCtx.Done()
		err := sm.staticListener.Close()
		if err != nil {
			sm.staticLog.Print("failed to close listener", err)
		}
		sm.staticWG.Done()
	}()
	return nil
}

// spawnListenerWS spawns a minimal web server to listen for incoming websocket
// connections. These are then upgraded to mux.Mux.
func (sm *SiaMux) spawnListenerWS(address string) error {
	// Listen on the specified address for incoming connections.
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return errors.AddContext(err, "unable to create listener")
	}
	// Declare a http mux for the http server.
	httpMux := http.NewServeMux()
	httpMux.HandleFunc("/", sm.handleWS)
	// Create the http.Server
	server := &http.Server{
		Addr:    listener.Addr().String(),
		Handler: httpMux,
	}
	// Start serving the websocket endpoint.
	sm.staticWG.Add(1)
	go func() {
		defer sm.staticWG.Done()
		err := server.Serve(listener)
		if err != http.ErrServerClosed {
			sm.staticLog.Print("WARNING: websocket server reported error on shutdown:", err)
		}
	}()
	// Spawn a thread to shutdown the server.
	sm.staticWG.Add(1)
	go func() {
		defer sm.staticWG.Done()
		<-sm.staticCtx.Done()
		ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
		defer cancel()
		err := server.Shutdown(ctx)
		if err != nil {
			sm.staticLog.Print("WARNING: shutting down the websocket server returned an error:", err)
		}
	}()
	// Set the URL the SiaMux is listening on.
	sm.staticURL = fmt.Sprintf("ws://%v", listener.Addr().String())
	return nil
}

// threadedAccept is spawned for every open connection wrapped in a multiplexer.
// It will constantly accept streams on that multiplexer, discard the ones that
// refer to unknown subscribers and forward the other ones to the corresponding
// listener.
func (sm *SiaMux) threadedAccept(mux *mux.Mux) {
	defer func() {
		// Remove the mux when we are not accepting streams anymore.
		sm.managedRemoveMux(mux)
		err := mux.Close()
		if err != nil {
			sm.staticLog.Logger.Print("threadedAccept: failed to close mux", err)
		}
	}()
	// Start accepting streams.
	for {
		select {
		case <-sm.staticCtx.Done():
			return // SiaMux closed
		default:
		}
		// Accept a stream.
		stream, err := mux.AcceptStream()
		if errors.Contains(err, io.EOF) {
			return
		} else if err != nil {
			sm.staticLog.Print("SiaMux: failed to accept stream", err)
			continue
		}
		// Read the subscriber.
		subscriber, err := readSubscriber(stream)
		if err != nil {
			sm.staticLog.Print("SiaMux: failed to read subscriber", errors.Compose(err, stream.Close()))
			return
		}
		// Check if a handler exists for the subscriber.
		sm.handlersMu.RLock()
		handler, exists := sm.handlers[subscriber]
		sm.handlersMu.RUnlock()
		if !exists {
			err = writeSubscriberResponse(stream, errUnknownSubscriber)
			sm.staticLog.Print("SiaMux: unknown subscriber", subscriber, errors.Compose(err, stream.Close()))
			continue
		}
		// Send the 'ok' response.
		srb := bytes.NewBuffer(nil)
		if err := writeSubscriberResponse(srb, nil); err != nil {
			sm.staticLog.Print("SiaMux: failed to send subscriber response", errors.Compose(err, stream.Close()))
			continue
		}
		stream.LazyWrite(srb.Bytes())

		// Queue the stream in the same thread to preserve the execution order.
		handler.managedQueueStream(stream)

		// But handle it in a different one.
		sm.staticWG.Add(1)
		go func() {
			defer sm.staticWG.Done()
			handler.threadedHandleNext()
		}()
	}
}
