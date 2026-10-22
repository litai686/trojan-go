package http

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/p4gefau1t/trojan-go/common"
	"github.com/p4gefau1t/trojan-go/log"
	"github.com/p4gefau1t/trojan-go/tunnel"
)

type ConnectConn struct {
	net.Conn
	metadata *tunnel.Metadata
}

func (c *ConnectConn) Metadata() *tunnel.Metadata {
	return c.metadata
}

// CloseWrite propagates the half close to the underlying connection
func (c *ConnectConn) CloseWrite() error {
	if cw, ok := c.Conn.(interface{ CloseWrite() error }); ok {
		return cw.CloseWrite()
	}
	return common.ErrCloseWriteNotSupported
}

type OtherConn struct {
	net.Conn
	metadata   *tunnel.Metadata // fixed
	reqReader  *io.PipeReader
	respWriter *io.PipeWriter
	ctx        context.Context
	cancel     context.CancelFunc
}

func (c *OtherConn) Metadata() *tunnel.Metadata {
	return c.metadata
}

func (c *OtherConn) Read(p []byte) (int, error) {
	n, err := c.reqReader.Read(p)
	if err == io.EOF {
		if n != 0 {
			panic("non zero")
		}
		for range c.ctx.Done() {
			return 0, common.NewError("http conn closed")
		}
	}
	return n, err
}

func (c *OtherConn) Write(p []byte) (int, error) {
	return c.respWriter.Write(p)
}

func (c *OtherConn) Close() error {
	c.cancel()
	c.reqReader.Close()
	c.respWriter.Close()
	return nil
}

type Server struct {
	underlay tunnel.Server
	connChan chan tunnel.Conn
	ctx      context.Context
	cancel   context.CancelFunc
}

func (s *Server) acceptLoop() {
	for {
		conn, err := s.underlay.AcceptConn(&Tunnel{})
		if err != nil {
			if s.ctx.Err() != nil {
				log.Error(common.NewError("http closed"))
				return
			}
			log.Error(common.NewError("http failed to accept connection").Base(err))
			time.Sleep(time.Millisecond * 100)
			continue
		}

		go func(conn net.Conn) {
			reqBufReader := bufio.NewReader(ioutil.NopCloser(conn))
			req, err := http.ReadRequest(reqBufReader)
			if err != nil {
				log.Error(common.NewError("not a valid http request").Base(err))
				conn.Close()
				return
			}

			if strings.ToUpper(req.Method) == "CONNECT" { // CONNECT
				addr, err := tunnel.NewAddressFromAddr("tcp", req.Host)
				if err != nil {
					log.Error(common.NewError("invalid http dest address").Base(err))
					conn.Close()
					return
				}
				resp := fmt.Sprintf("HTTP/%d.%d 200 Connection established\r\n\r\n", req.ProtoMajor, req.ProtoMinor)
				_, err = conn.Write([]byte(resp))
				if err != nil {
					log.Error("http failed to respond connect request")
					conn.Close()
					return
				}
				s.connChan <- &ConnectConn{
					Conn: conn,
					metadata: &tunnel.Metadata{
						Address: addr,
					},
				}
			} else { // GET, POST, PUT...
				defer conn.Close()
				// reused across keep-alive requests to avoid per-request allocations
				respBufWriter := bufio.NewWriterSize(conn, 4096)
				for {
					reqReader, reqWriter := io.Pipe()
					respReader, respWriter := io.Pipe()
					var addr *tunnel.Address
					if addr, err = tunnel.NewAddressFromAddr("tcp", req.Host); err != nil {
						addr = tunnel.NewAddressFromHostPort("tcp", req.Host, 80)
					}
					log.Debug("http dest", addr)

					ctx, cancel := context.WithCancel(s.ctx)
					newConn := &OtherConn{
						Conn: conn,
						metadata: &tunnel.Metadata{
							Address: addr,
						},
						ctx:        ctx,
						cancel:     cancel,
						reqReader:  reqReader,
						respWriter: respWriter,
					}
					s.connChan <- newConn // pass this http session connection to proxy.RelayConn

					err = req.Write(reqWriter) // write request to the remote
					if err != nil {
						log.Error(common.NewError("http failed to write http request").Base(err))
						return
					}

					respBufReader := bufio.NewReader(ioutil.NopCloser(respReader)) // read response from the remote
					resp, err := http.ReadResponse(respBufReader, req)
					if err != nil {
						log.Error(common.NewError("http failed to read http response").Base(err))
						return
					}
					// batch the response headers and small writes instead of issuing
					// one write syscall per header line
					err = resp.Write(respBufWriter) // send the response back to the local
					if err == nil {
						err = respBufWriter.Flush()
					}
					if err != nil {
						log.Error(common.NewError("http failed to write the response back").Base(err))
						return
					}
					newConn.Close()
					req.Body.Close()
					resp.Body.Close()

					req, err = http.ReadRequest(reqBufReader) // read the next http request from local
					if err != nil {
						log.Error(common.NewError("http failed to the read request from local").Base(err))
						return
					}
				}
			}
		}(conn)
	}
}

func (s *Server) AcceptConn(tunnel.Tunnel) (tunnel.Conn, error) {
	select {
	case conn := <-s.connChan:
		return conn, nil
	case <-s.ctx.Done():
		return nil, common.NewError("http server closed")
	}
}

func (s *Server) AcceptPacket(tunnel.Tunnel) (tunnel.PacketConn, error) {
	<-s.ctx.Done()
	return nil, common.NewError("http server closed")
}

func (s *Server) Close() error {
	s.cancel()
	return s.underlay.Close()
}

func NewServer(ctx context.Context, underlay tunnel.Server) (*Server, error) {
	ctx, cancel := context.WithCancel(ctx)
	server := &Server{
		underlay: underlay,
		connChan: make(chan tunnel.Conn, 32),
		ctx:      ctx,
		cancel:   cancel,
	}
	go server.acceptLoop()
	return server, nil
}
