// Copyright 2012 SocialCode. All rights reserved.
// Use of this source code is governed by the MIT
// license that can be found in the LICENSE file.

package mlog

import (
	"net"
)

type writer interface {
	close() error
	write([]byte) (int, error)
	writeMessage(*message) error
}

// Writer implements io.Writer and is used to send both discrete
// messages to a graylog2 server, or data from a stream-oriented
// interface (like the functions in log).
type gelfWriter struct {
	addr     string
	conn     net.Conn
	hostname string
	Facility string // defaults to current process name
	proto    string
}

// Close connection and interrupt blocked Read or Write operations
func (w *gelfWriter) close() error {
	if w.conn == nil {
		return nil
	}
	return w.conn.Close()
}
