package clickhouse

import (
	"bufio"
	"database/sql/driver"
	"net"
	"sync/atomic"
	"time"
)

var tick int32

type openStrategy int8

func (s openStrategy) String() string {
	switch s {
	case connOpenInOrder:
		return "in_order"
	}
	return "random"
}

const (
	connOpenRandom openStrategy = iota + 1
	connOpenInOrder
)

func dial(network string, hosts []string, noDelay bool, openStrategy openStrategy, logf func(string, ...interface{})) (*connect, error) {
	var (
		err error
		abs = func(v int) int {
			if v < 0 {
				return -1 * v
			}
			return v
		}
		conn  net.Conn
		ident = abs(int(atomic.AddInt32(&tick, 1)))
	)
	for i := 0; i <= len(hosts); i++ {
		var num int
		switch openStrategy {
		case connOpenInOrder:
			num = i
		case connOpenRandom:
			num = (ident + 1) % len(hosts)
		}
		if conn, err = net.DialTimeout(network, hosts[num], 20*time.Second); err == nil {
			logf("[dial] strategy=%s, ident=%d, server=%d -> %s", openStrategy, ident, num, conn.RemoteAddr())
			if tcp, ok := conn.(*net.TCPConn); ok {
				tcp.SetNoDelay(noDelay) // Disable or enable the Nagle Algorithm for this tcp socket
			}
			return &connect{
				Conn:   conn,
				logf:   logf,
				ident:  ident,
				buffer: bufio.NewReaderSize(conn, 4*1024*1024),
			}, nil
		}
	}
	return nil, err
}

type connect struct {
	net.Conn
	logf   func(string, ...interface{})
	ident  int
	buffer *bufio.Reader
	closed bool
}

func (conn *connect) Read(b []byte) (int, error) {
	var (
		n      int
		err    error
		total  int
		dstLen = len(b)
	)
	for total < dstLen {
		if n, err = conn.buffer.Read(b[total:]); err != nil {
			conn.logf("[connect] read error: %v", err)
			conn.closed = true
			return n, driver.ErrBadConn
		}
		total += n
	}
	return total, nil
}

func (conn *connect) Write(b []byte) (int, error) {
	var (
		n      int
		err    error
		total  int
		srcLen = len(b)
	)
	for total < srcLen {
		if n, err = conn.Conn.Write(b[total:]); err != nil {
			conn.logf("[connect] write error: %v", err)
			conn.closed = true
			return n, driver.ErrBadConn
		}
		total += n
	}
	return n, nil
}

func (conn *connect) Close() error {
	if !conn.closed {
		conn.closed = true
		return conn.Conn.Close()
	}
	return nil
}
