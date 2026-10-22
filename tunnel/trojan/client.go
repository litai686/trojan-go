package trojan

import (
	"bytes"
	"context"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/p4gefau1t/trojan-go/api"
	"github.com/p4gefau1t/trojan-go/common"
	"github.com/p4gefau1t/trojan-go/config"
	"github.com/p4gefau1t/trojan-go/log"
	"github.com/p4gefau1t/trojan-go/statistic"
	"github.com/p4gefau1t/trojan-go/statistic/memory"
	"github.com/p4gefau1t/trojan-go/tunnel"
	"github.com/p4gefau1t/trojan-go/tunnel/mux"
)

const (
	MaxPacketSize = 1024 * 8
)

const (
	Connect   tunnel.Command = 1
	Associate tunnel.Command = 3
	Mux       tunnel.Command = 0x7f
)

// headerFlushDelay gives the first payload a short window to be coalesced with
// the trojan header into a single tls record, while still letting protocols
// that wait for the server to speak first start without a long stall
const headerFlushDelay = time.Millisecond * 20

type OutboundConn struct {
	// WARNING: do not change the order of these fields.
	// 64-bit fields that use `sync/atomic` package functions
	// must be 64-bit aligned on 32-bit systems.
	// Reference: https://github.com/golang/go/issues/599
	// Solution: https://github.com/golang/go/issues/11891#issuecomment-433623786
	sent uint64
	recv uint64

	metadata          *tunnel.Metadata
	user              statistic.User
	headerMutex       sync.Mutex
	headerWritten     bool
	headerTimer       *time.Timer
	net.Conn
}

func (c *OutboundConn) Metadata() *tunnel.Metadata {
	return c.metadata
}

// writeHeader must be called with headerMutex held so that the header is
// always written before any payload on the wire
func (c *OutboundConn) writeHeader(payload []byte) (bool, error) {
	if c.headerWritten {
		return false, nil
	}
	hash := c.user.Hash()
	buf := bytes.NewBuffer(make([]byte, 0, MaxPacketSize))
	crlf := []byte{0x0d, 0x0a}
	buf.Write([]byte(hash))
	buf.Write(crlf)
	c.metadata.WriteTo(buf)
	buf.Write(crlf)
	if payload != nil {
		buf.Write(payload)
	}
	if _, err := c.Conn.Write(buf.Bytes()); err != nil {
		return false, err
	}
	c.headerWritten = true
	return true, nil
}

func (c *OutboundConn) WriteHeader(payload []byte) (bool, error) {
	c.headerMutex.Lock()
	defer c.headerMutex.Unlock()
	return c.writeHeader(payload)
}

func (c *OutboundConn) Write(p []byte) (int, error) {
	c.headerMutex.Lock()
	defer c.headerMutex.Unlock()
	written, err := c.writeHeader(p)
	if err != nil {
		return 0, common.NewError("trojan failed to flush header with payload").Base(err)
	}
	if written {
		if c.headerTimer != nil {
			c.headerTimer.Stop()
		}
		c.user.AddTraffic(len(p), 0)
		atomic.AddUint64(&c.sent, uint64(len(p)))
		return len(p), nil
	}
	n, err := c.Conn.Write(p)
	c.user.AddTraffic(n, 0)
	atomic.AddUint64(&c.sent, uint64(n))
	return n, err
}

func (c *OutboundConn) Read(p []byte) (int, error) {
	n, err := c.Conn.Read(p)
	c.user.AddTraffic(0, n)
	atomic.AddUint64(&c.recv, uint64(n))
	return n, err
}

// CloseWrite flushes a pending trojan header and propagates the half close
func (c *OutboundConn) CloseWrite() error {
	c.headerMutex.Lock()
	if !c.headerWritten {
		if _, err := c.writeHeader(nil); err != nil {
			c.headerMutex.Unlock()
			return err
		}
	}
	c.headerMutex.Unlock()
	if cw, ok := c.Conn.(interface{ CloseWrite() error }); ok {
		return cw.CloseWrite()
	}
	return common.ErrCloseWriteNotSupported
}

func (c *OutboundConn) Close() error {
	if c.headerTimer != nil {
		c.headerTimer.Stop()
	}
	log.Info("connection to", c.metadata, "closed", "sent:", common.HumanFriendlyTraffic(atomic.LoadUint64(&c.sent)), "recv:", common.HumanFriendlyTraffic(atomic.LoadUint64(&c.recv)))
	return c.Conn.Close()
}

type Client struct {
	underlay tunnel.Client
	user     statistic.User
	ctx      context.Context
	cancel   context.CancelFunc
}

func (c *Client) Close() error {
	c.cancel()
	return c.underlay.Close()
}

func (c *Client) DialConn(addr *tunnel.Address, overlay tunnel.Tunnel) (tunnel.Conn, error) {
	conn, err := c.underlay.DialConn(addr, &Tunnel{})
	if err != nil {
		return nil, err
	}
	newConn := &OutboundConn{
		Conn: conn,
		user: c.user,
		metadata: &tunnel.Metadata{
			Command: Connect,
			Address: addr,
		},
	}
	if _, ok := overlay.(*mux.Tunnel); ok {
		newConn.metadata.Command = Mux
	}

	// if the trojan header has not been flushed together with the first payload
	// after a short delay, the peer may expect the server to speak first
	newConn.headerTimer = time.AfterFunc(headerFlushDelay, func() {
		newConn.WriteHeader(nil)
	})
	return newConn, nil
}

func (c *Client) DialPacket(tunnel.Tunnel) (tunnel.PacketConn, error) {
	fakeAddr := &tunnel.Address{
		DomainName:  "UDP_CONN",
		AddressType: tunnel.DomainName,
	}
	conn, err := c.underlay.DialConn(fakeAddr, &Tunnel{})
	if err != nil {
		return nil, err
	}
	return &PacketConn{
		Conn: &OutboundConn{
			Conn: conn,
			user: c.user,
			metadata: &tunnel.Metadata{
				Command: Associate,
				Address: fakeAddr,
			},
		},
	}, nil
}

func NewClient(ctx context.Context, client tunnel.Client) (*Client, error) {
	ctx, cancel := context.WithCancel(ctx)
	auth, err := statistic.NewAuthenticator(ctx, memory.Name)
	if err != nil {
		cancel()
		return nil, err
	}

	cfg := config.FromContext(ctx, Name).(*Config)
	if cfg.API.Enabled {
		go api.RunService(ctx, Name+"_CLIENT", auth)
	}

	var user statistic.User
	for _, u := range auth.ListUsers() {
		user = u
		break
	}
	if user == nil {
		cancel()
		return nil, common.NewError("no valid user found")
	}

	log.Debug("trojan client created")
	return &Client{
		underlay: client,
		ctx:      ctx,
		user:     user,
		cancel:   cancel,
	}, nil
}
