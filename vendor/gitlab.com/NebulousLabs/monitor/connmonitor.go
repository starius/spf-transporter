package connmonitor

import (
	"net"
	"sync/atomic"
	"time"
)

type (
	// Monitor is a simple byte counter for read and write operations on a
	// io.ReadWriter.
	Monitor struct {
		atomicReadBytes  uint64 // bytes that have been read.
		atomicWriteBytes uint64 // bytes that have been written.

		staticStartTime time.Time
	}

	// mConn is a simple counter wrapper for the net.Conn interface.
	mConn struct {
		net.Conn
		m *Monitor
	}
)

// NewMonitor creates a new Monitor object that can be used to initialize
// monitored readers and writers.
func NewMonitor() *Monitor {
	return &Monitor{
		atomicReadBytes:  0,
		atomicWriteBytes: 0,
		staticStartTime:  time.Now(),
	}
}

// NewMonitoredConn wraps a net.Conn into a mConn.
func NewMonitoredConn(conn net.Conn, m *Monitor) net.Conn {
	return &mConn{
		Conn: conn,
		m:    m,
	}
}

// Counts returns the total bytes written and read by the Monitor.
func (m *Monitor) Counts() (uint64, uint64) {
	readBytes := atomic.LoadUint64(&m.atomicReadBytes)
	writeBytes := atomic.LoadUint64(&m.atomicWriteBytes)
	return readBytes, writeBytes
}

// StartTime is the time at which the monitor was created to start monitoring
func (m *Monitor) StartTime() time.Time {
	return m.staticStartTime
}

// Read is a wrapper for the underlying Conn that counts the bytes to be read
// before calling Read on the Conn.
func (mc *mConn) Read(b []byte) (n int, err error) {
	n, err = mc.Conn.Read(b)
	atomic.AddUint64(&mc.m.atomicReadBytes, uint64(n))
	return
}

// Write is a wrapper for the underlying Conn that counts the bytes to be
// written before calling Write on the Conn.
func (mc *mConn) Write(b []byte) (n int, err error) {
	n, err = mc.Conn.Write(b)
	atomic.AddUint64(&mc.m.atomicWriteBytes, uint64(n))
	return
}
