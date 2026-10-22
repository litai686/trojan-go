package proxy

import (
	"context"
	"io"
	"math/rand"
	"net"
	"os"
	"strings"
	"time"

	"github.com/p4gefau1t/trojan-go/common"
	"github.com/p4gefau1t/trojan-go/config"
	"github.com/p4gefau1t/trojan-go/log"
	"github.com/p4gefau1t/trojan-go/tunnel"
)

const Name = "PROXY"

const (
	MaxPacketSize = 1024 * 8
	// relayDrainTimeout bounds how long a half-closed connection may keep
	// the opposite direction alive, so pending response data is not dropped
	relayDrainTimeout = time.Second * 30
)

// Proxy relay connections and packets
type Proxy struct {
	sources []tunnel.Server
	sink    tunnel.Client
	ctx     context.Context
	cancel  context.CancelFunc
}

func (p *Proxy) Run() error {
	p.relayConnLoop()
	p.relayPacketLoop()
	<-p.ctx.Done()
	return nil
}

func (p *Proxy) Close() error {
	p.cancel()
	p.sink.Close()
	for _, source := range p.sources {
		source.Close()
	}
	return nil
}

func (p *Proxy) relayConnLoop() {
	for _, source := range p.sources {
		go func(source tunnel.Server) {
			for {
				inbound, err := source.AcceptConn(nil)
				if err != nil {
					select {
					case <-p.ctx.Done():
						log.Debug("exiting")
						return
					default:
					}
					log.Error(common.NewError("failed to accept connection").Base(err))
					time.Sleep(time.Millisecond * 100)
					continue
				}
				go func(inbound tunnel.Conn) {
					defer inbound.Close()
					outbound, err := p.sink.DialConn(inbound.Metadata().Address, nil)
					if err != nil {
						log.Error(common.NewError("proxy failed to dial connection").Base(err))
						return
					}
					defer outbound.Close()

					type relayResult struct {
						err        error
						halfClosed bool
					}
					resultChan := make(chan relayResult, 2)
					relay := func(dst, src net.Conn) {
						_, err := io.Copy(dst, src)
						halfClosed := false
						if err == nil {
							// propagate EOF so the peer knows the request is complete
							// and can finish its response
							if cw, ok := dst.(interface{ CloseWrite() error }); ok {
								halfClosed = cw.CloseWrite() == nil
							}
						}
						resultChan <- relayResult{err: err, halfClosed: halfClosed}
					}
					go relay(outbound, inbound)
					go relay(inbound, outbound)

					var result relayResult
					select {
					case result = <-resultChan:
					case <-p.ctx.Done():
						log.Debug("shutting down conn relay")
						return
					}
					if result.err != nil {
						log.Debug(result.err)
					} else {
						if !result.halfClosed {
							// some protocols (mux, websocket) cannot deliver the half
							// close, but the peer may still answer on its own
							log.Debug("half close is not supported by this protocol")
						}
						// the request side finished cleanly, wait for the opposite one
						// so a pending response is not dropped
						select {
						case <-resultChan:
						case <-time.After(relayDrainTimeout):
						case <-p.ctx.Done():
							log.Debug("shutting down conn relay")
							return
						}
					}
					log.Debug("conn relay ends")
				}(inbound)
			}
		}(source)
	}
}

func (p *Proxy) relayPacketLoop() {
	for _, source := range p.sources {
		go func(source tunnel.Server) {
			for {
				inbound, err := source.AcceptPacket(nil)
				if err != nil {
					select {
					case <-p.ctx.Done():
						log.Debug("exiting")
						return
					default:
					}
					log.Error(common.NewError("failed to accept packet").Base(err))
					time.Sleep(time.Millisecond * 100)
					continue
				}
				go func(inbound tunnel.PacketConn) {
					defer inbound.Close()
					outbound, err := p.sink.DialPacket(nil)
					if err != nil {
						log.Error(common.NewError("proxy failed to dial packet").Base(err))
						return
					}
					defer outbound.Close()
					errChan := make(chan error, 2)
					copyPacket := func(a, b tunnel.PacketConn) {
						for {
							buf := make([]byte, MaxPacketSize)
							n, metadata, err := a.ReadWithMetadata(buf)
							if err != nil {
								errChan <- err
								return
							}
							if n == 0 {
								errChan <- nil
								return
							}
							_, err = b.WriteWithMetadata(buf[:n], metadata)
							if err != nil {
								errChan <- err
								return
							}
						}
					}
					go copyPacket(inbound, outbound)
					go copyPacket(outbound, inbound)
					select {
					case err = <-errChan:
						if err != nil {
							log.Error(err)
						}
					case <-p.ctx.Done():
						log.Debug("shutting down packet relay")
					}
					log.Debug("packet relay ends")
				}(inbound)
			}
		}(source)
	}
}

func NewProxy(ctx context.Context, cancel context.CancelFunc, sources []tunnel.Server, sink tunnel.Client) *Proxy {
	return &Proxy{
		sources: sources,
		sink:    sink,
		ctx:     ctx,
		cancel:  cancel,
	}
}

type Creator func(ctx context.Context) (*Proxy, error)

var creators = make(map[string]Creator)

func RegisterProxyCreator(name string, creator Creator) {
	creators[name] = creator
}

func NewProxyFromConfigData(data []byte, isJSON bool) (*Proxy, error) {
	// create a unique context for each proxy instance to avoid duplicated authenticator
	ctx := context.WithValue(context.Background(), Name+"_ID", rand.Int())
	var err error
	if isJSON {
		ctx, err = config.WithJSONConfig(ctx, data)
		if err != nil {
			return nil, err
		}
	} else {
		ctx, err = config.WithYAMLConfig(ctx, data)
		if err != nil {
			return nil, err
		}
	}
	cfg := config.FromContext(ctx, Name).(*Config)
	create, ok := creators[strings.ToUpper(cfg.RunType)]
	if !ok {
		return nil, common.NewError("unknown proxy type: " + cfg.RunType)
	}
	log.SetLogLevel(log.LogLevel(cfg.LogLevel))
	if cfg.LogFile != "" {
		file, err := os.OpenFile(cfg.LogFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			return nil, common.NewError("failed to open log file").Base(err)
		}
		log.SetOutput(file)
	}
	return create(ctx)
}
