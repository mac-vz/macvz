package httpclientutil

import (
	"net"
	"time"
)

type NonClosableConn struct {
	conn net.Conn
}

// Read reads data from connection of the vsock protocol.
func (v *NonClosableConn) Read(b []byte) (n int, err error) { return v.conn.Read(b) }

// Write writes data to the connection of the vsock protocol.
func (v *NonClosableConn) Write(b []byte) (n int, err error) { return v.conn.Write(b) }

// Close will be called when caused something error in socket.
func (v *NonClosableConn) Close() error {
	//Treat as no-op as we wan't to keep this open
	return nil
}

// LocalAddr returns the local network address.
func (v *NonClosableConn) LocalAddr() net.Addr { return v.conn.LocalAddr() }

// RemoteAddr returns the remote network address.
func (v *NonClosableConn) RemoteAddr() net.Addr { return v.conn.RemoteAddr() }

// SetDeadline sets the read and write deadlines associated
// with the connection. It is equivalent to calling both
// SetReadDeadline and SetWriteDeadline.
func (v *NonClosableConn) SetDeadline(t time.Time) error { return v.conn.SetDeadline(t) }

// SetReadDeadline sets the deadline for future Read calls
// and any currently-blocked Read call.
// A zero value for t means Read will not time out.
func (v *NonClosableConn) SetReadDeadline(t time.Time) error {
	return v.conn.SetReadDeadline(t)
}

func (v *NonClosableConn) SetWriteDeadline(t time.Time) error {
	return v.conn.SetWriteDeadline(t)
}
