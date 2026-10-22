package router

import (
	"context"
	"encoding/binary"
	"net"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	v2router "github.com/v2fly/v2ray-core/v4/app/router"

	"github.com/p4gefau1t/trojan-go/common"
	"github.com/p4gefau1t/trojan-go/common/geodata"
	"github.com/p4gefau1t/trojan-go/config"
	"github.com/p4gefau1t/trojan-go/log"
	"github.com/p4gefau1t/trojan-go/tunnel"
	"github.com/p4gefau1t/trojan-go/tunnel/freedom"
	"github.com/p4gefau1t/trojan-go/tunnel/transport"
)

const (
	Block  = 0
	Bypass = 1
	Proxy  = 2
)

const (
	AsIs         = 0
	IPIfNonMatch = 1
	IPOnDemand   = 2
)

const MaxPacketSize = 1024 * 8

// resolveTimeout bounds the dns lookup performed during routing
const resolveTimeout = time.Second * 3

// domainMatcher indexes routing rules so a lookup costs a few map reads
// instead of a scan over every loaded rule
//
// Rules of one strategy list are equivalent: any hit selects the strategy, so
// the original order between rule types carries no meaning and they can be
// split by type
type domainMatcher struct {
	full    map[string]struct{}
	suffix  map[string]struct{}
	keyword []string
	regex   []*regexp.Regexp
}

func newDomainMatcher(list []*v2router.Domain) *domainMatcher {
	m := &domainMatcher{
		full:   make(map[string]struct{}, len(list)),
		suffix: make(map[string]struct{}, len(list)),
	}
	for _, d := range list {
		switch d.GetType() {
		case v2router.Domain_Full:
			m.full[d.GetValue()] = struct{}{}
		case v2router.Domain_Domain:
			m.suffix[d.GetValue()] = struct{}{}
		case v2router.Domain_Plain:
			m.keyword = append(m.keyword, d.GetValue())
		case v2router.Domain_Regex:
			re, err := regexp.Compile(d.GetValue())
			if err != nil {
				log.Error(common.NewError("invalid regular expression: " + d.GetValue()).Base(err))
				continue
			}
			m.regex = append(m.regex, re)
		default:
			log.Debug("unknown rule type:", d.GetType().String())
		}
	}
	return m
}

func (m *domainMatcher) match(target string) bool {
	if _, ok := m.full[target]; ok {
		log.Tracef("domain %s hit domain(full) rule", target)
		return true
	}
	if _, ok := m.suffix[target]; ok {
		log.Tracef("domain %s hit domain rule: %s", target, target)
		return true
	}
	offset := 0
	for i := 0; i < len(target); i++ {
		if target[i] != '.' {
			continue
		}
		offset = i + 1
		if _, ok := m.suffix[target[offset:]]; ok {
			log.Tracef("domain %s hit domain rule: %s", target, target[offset:])
			return true
		}
	}
	for _, name := range m.keyword {
		if strings.Contains(target, name) {
			log.Tracef("domain %s hit keyword rule: %s", target, name)
			return true
		}
	}
	for _, re := range m.regex {
		if re.MatchString(target) {
			log.Tracef("domain %s hit regex rule: %s", target, re.String())
			return true
		}
	}
	return false
}

// ipMatcher groups CIDRs by prefix length and masks the target once per
// distinct length, so a lookup costs a few map reads instead of a scan
type ipMatcher struct {
	v4 map[int]map[uint32]struct{}
	v6 map[int]map[[16]byte]struct{}
}

func maskV4(bits int) uint32 {
	if bits <= 0 {
		return 0
	}
	return ^uint32(0) << (32 - bits)
}

func maskBytes(b []byte, bits int) {
	for i := 0; i < len(b); i++ {
		if bits >= 8 {
			bits -= 8
			continue
		}
		if bits <= 0 {
			b[i] = 0
			continue
		}
		b[i] &= byte(0xff << (8 - bits))
		bits = 0
	}
}

func newIPMatcher(list []*v2router.CIDR) *ipMatcher {
	m := &ipMatcher{
		v4: make(map[int]map[uint32]struct{}),
		v6: make(map[int]map[[16]byte]struct{}),
	}
	for _, c := range list {
		bits := int(c.GetPrefix())
		ip := net.IP(c.GetIp())
		if ip4 := ip.To4(); ip4 != nil {
			if bits < 0 || bits > 32 {
				continue
			}
			set, ok := m.v4[bits]
			if !ok {
				set = make(map[uint32]struct{})
				m.v4[bits] = set
			}
			set[binary.BigEndian.Uint32(ip4)&maskV4(bits)] = struct{}{}
			continue
		}
		ip16 := ip.To16()
		if ip16 == nil || bits < 0 || bits > 128 {
			continue
		}
		set, ok := m.v6[bits]
		if !ok {
			set = make(map[[16]byte]struct{})
			m.v6[bits] = set
		}
		var key [16]byte
		copy(key[:], ip16)
		maskBytes(key[:], bits)
		set[key] = struct{}{}
	}
	return m
}

func (m *ipMatcher) match(target net.IP) bool {
	if ip4 := target.To4(); ip4 != nil {
		value := binary.BigEndian.Uint32(ip4)
		for bits, set := range m.v4 {
			if _, ok := set[value&maskV4(bits)]; ok {
				return true
			}
		}
		return false
	}
	ip16 := target.To16()
	if ip16 == nil {
		return false
	}
	var key [16]byte
	copy(key[:], ip16)
	for bits, set := range m.v6 {
		masked := key
		maskBytes(masked[:], bits)
		if _, ok := set[masked]; ok {
			return true
		}
	}
	return false
}

func newIPAddress(address *tunnel.Address) (*tunnel.Address, error) {
	if address.AddressType != tunnel.DomainName {
		return address, nil
	}
	if address.IP == nil {
		ctx, cancel := context.WithTimeout(context.Background(), resolveTimeout)
		defer cancel()
		ips, err := net.DefaultResolver.LookupIPAddr(ctx, address.DomainName)
		if err != nil {
			return nil, common.NewError("router failed to resolve ip").Base(err)
		}
		if len(ips) == 0 {
			return nil, common.NewError("router failed to resolve ip: no record")
		}
		// cache the result on the address, so udp sessions do not resolve the
		// same domain for every packet
		address.IP = ips[0].IP
	}
	newAddress := &tunnel.Address{
		IP:   address.IP,
		Port: address.Port,
	}
	if newAddress.IP.To4() != nil {
		newAddress.AddressType = tunnel.IPv4
	} else {
		newAddress.AddressType = tunnel.IPv6
	}
	return newAddress, nil
}

type Client struct {
	domains        [3]*domainMatcher
	ips            [3]*ipMatcher
	defaultPolicy  int
	domainStrategy int
	underlay       tunnel.Client
	direct         *freedom.Client
	ctx            context.Context
	cancel         context.CancelFunc
}

func (c *Client) Route(address *tunnel.Address) int {
	if address.AddressType == tunnel.DomainName {
		if c.domainStrategy == IPOnDemand {
			if resolvedIP, err := newIPAddress(address); err == nil {
				for i := Block; i <= Proxy; i++ {
					if c.ips[i].match(resolvedIP.IP) {
						return i
					}
				}
			}
		}
		for i := Block; i <= Proxy; i++ {
			if c.domains[i].match(address.DomainName) {
				return i
			}
		}
		if c.domainStrategy == IPIfNonMatch {
			if resolvedIP, err := newIPAddress(address); err == nil {
				for i := Block; i <= Proxy; i++ {
					if c.ips[i].match(resolvedIP.IP) {
						return i
					}
				}
			}
		}
	} else {
		for i := Block; i <= Proxy; i++ {
			if c.ips[i].match(address.IP) {
				return i
			}
		}
	}
	return c.defaultPolicy
}

func (c *Client) DialConn(address *tunnel.Address, overlay tunnel.Tunnel) (tunnel.Conn, error) {
	policy := c.Route(address)
	switch policy {
	case Proxy:
		return c.underlay.DialConn(address, overlay)
	case Block:
		return nil, common.NewError("router blocked address: " + address.String())
	case Bypass:
		conn, err := c.direct.DialConn(address, &Tunnel{})
		if err != nil {
			return nil, common.NewError("router dial error").Base(err)
		}
		return &transport.Conn{
			Conn: conn,
		}, nil
	}
	panic("unknown policy")
}

func (c *Client) DialPacket(overlay tunnel.Tunnel) (tunnel.PacketConn, error) {
	directConn, err := net.ListenPacket("udp", "")
	if err != nil {
		return nil, common.NewError("router failed to dial udp (direct)").Base(err)
	}
	proxy, err := c.underlay.DialPacket(overlay)
	if err != nil {
		return nil, common.NewError("router failed to dial udp (proxy)").Base(err)
	}
	ctx, cancel := context.WithCancel(c.ctx)
	conn := &PacketConn{
		Client:     c,
		PacketConn: directConn,
		proxy:      proxy,
		cancel:     cancel,
		ctx:        ctx,
		packetChan: make(chan *packetInfo, 16),
	}
	go conn.packetLoop()
	return conn, nil
}

func (c *Client) Close() error {
	c.cancel()
	return c.underlay.Close()
}

type codeInfo struct {
	code     string
	strategy int
}

func loadCode(cfg *Config, prefix string) []codeInfo {
	codes := []codeInfo{}
	for _, s := range cfg.Router.Proxy {
		if strings.HasPrefix(s, prefix) {
			if left := s[len(prefix):]; len(left) > 0 {
				codes = append(codes, codeInfo{
					code:     left,
					strategy: Proxy,
				})
			} else {
				log.Warn("invalid empty rule:", s)
			}
		}
	}
	for _, s := range cfg.Router.Bypass {
		if strings.HasPrefix(s, prefix) {
			if left := s[len(prefix):]; len(left) > 0 {
				codes = append(codes, codeInfo{
					code:     left,
					strategy: Bypass,
				})
			} else {
				log.Warn("invalid empty rule:", s)
			}
		}
	}
	for _, s := range cfg.Router.Block {
		if strings.HasPrefix(s, prefix) {
			if left := s[len(prefix):]; len(left) > 0 {
				codes = append(codes, codeInfo{
					code:     left,
					strategy: Block,
				})
			} else {
				log.Warn("invalid empty rule:", s)
			}
		}
	}
	return codes
}

func NewClient(ctx context.Context, underlay tunnel.Client) (*Client, error) {
	m1 := runtime.MemStats{}
	m2 := runtime.MemStats{}
	m3 := runtime.MemStats{}
	m4 := runtime.MemStats{}

	cfg := config.FromContext(ctx, Name).(*Config)
	var cancel context.CancelFunc
	ctx, cancel = context.WithCancel(ctx)

	direct, err := freedom.NewClient(ctx, nil)
	if err != nil {
		cancel()
		return nil, common.NewError("router failed to initialize raw client").Base(err)
	}

	var rawDomains [3][]*v2router.Domain
	var rawCIDRs [3][]*v2router.CIDR
	client := &Client{
		underlay: underlay,
		direct:   direct,
		ctx:      ctx,
		cancel:   cancel,
	}
	switch strings.ToLower(cfg.Router.DomainStrategy) {
	case "as_is", "as-is", "asis":
		client.domainStrategy = AsIs
	case "ip_if_non_match", "ip-if-non-match", "ipifnonmatch":
		client.domainStrategy = IPIfNonMatch
	case "ip_on_demand", "ip-on-demand", "ipondemand":
		client.domainStrategy = IPOnDemand
	default:
		return nil, common.NewError("unknown strategy: " + cfg.Router.DomainStrategy)
	}

	switch strings.ToLower(cfg.Router.DefaultPolicy) {
	case "proxy":
		client.defaultPolicy = Proxy
	case "bypass":
		client.defaultPolicy = Bypass
	case "block":
		client.defaultPolicy = Block
	default:
		return nil, common.NewError("unknown strategy: " + cfg.Router.DomainStrategy)
	}

	runtime.ReadMemStats(&m1)

	geodataLoader := geodata.NewGeodataLoader()

	ipCode := loadCode(cfg, "geoip:")
	for _, c := range ipCode {
		code := c.code
		cidrs, err := geodataLoader.LoadIP(cfg.Router.GeoIPFilename, code)
		if err != nil {
			log.Error(err)
		} else {
			log.Infof("geoip:%s loaded", code)
			rawCIDRs[c.strategy] = append(rawCIDRs[c.strategy], cidrs...)
		}
	}

	runtime.ReadMemStats(&m2)

	siteCode := loadCode(cfg, "geosite:")
	for _, c := range siteCode {
		code := c.code
		attrWanted := ""
		// Test if user wants domains that have an attribute
		if attrIdx := strings.Index(code, "@"); attrIdx > 0 {
			if !strings.HasSuffix(code, "@") {
				code = c.code[:attrIdx]
				attrWanted = c.code[attrIdx+1:]
			} else { // "geosite:google@" is invalid
				log.Warnf("geosite:%s invalid", code)
				continue
			}
		} else if attrIdx == 0 { // "geosite:@cn" is invalid
			log.Warnf("geosite:%s invalid", code)
			continue
		}

		domainList, err := geodataLoader.LoadSite(cfg.Router.GeoSiteFilename, code)
		if err != nil {
			log.Error(err)
		} else {
			found := false
			if attrWanted != "" {
				for _, domain := range domainList {
					for _, attr := range domain.GetAttribute() {
						if strings.EqualFold(attrWanted, attr.GetKey()) {
							rawDomains[c.strategy] = append(rawDomains[c.strategy], domain)
							found = true
						}
					}
				}
			} else {
				rawDomains[c.strategy] = append(rawDomains[c.strategy], domainList...)
				found = true
			}
			if found {
				log.Infof("geosite:%s loaded", c.code)
			} else {
				log.Errorf("geosite:%s not found", c.code)
			}
		}
	}

	runtime.ReadMemStats(&m3)

	domainInfo := loadCode(cfg, "domain:")
	for _, info := range domainInfo {
		rawDomains[info.strategy] = append(rawDomains[info.strategy], &v2router.Domain{
			Type:      v2router.Domain_Domain,
			Value:     strings.ToLower(info.code),
			Attribute: nil,
		})
	}

	keywordInfo := loadCode(cfg, "keyword:")
	for _, info := range keywordInfo {
		rawDomains[info.strategy] = append(rawDomains[info.strategy], &v2router.Domain{
			Type:      v2router.Domain_Plain,
			Value:     strings.ToLower(info.code),
			Attribute: nil,
		})
	}

	regexInfo := loadCode(cfg, "regex:")
	for _, info := range regexInfo {
		if _, err := regexp.Compile(info.code); err != nil {
			return nil, common.NewError("invalid regular expression: " + info.code).Base(err)
		}
		rawDomains[info.strategy] = append(rawDomains[info.strategy], &v2router.Domain{
			Type:      v2router.Domain_Regex,
			Value:     info.code,
			Attribute: nil,
		})
	}

	// Just for compatibility with V2Ray rule type `regexp`
	regexpInfo := loadCode(cfg, "regexp:")
	for _, info := range regexpInfo {
		if _, err := regexp.Compile(info.code); err != nil {
			return nil, common.NewError("invalid regular expression: " + info.code).Base(err)
		}
		rawDomains[info.strategy] = append(rawDomains[info.strategy], &v2router.Domain{
			Type:      v2router.Domain_Regex,
			Value:     info.code,
			Attribute: nil,
		})
	}

	fullInfo := loadCode(cfg, "full:")
	for _, info := range fullInfo {
		rawDomains[info.strategy] = append(rawDomains[info.strategy], &v2router.Domain{
			Type:      v2router.Domain_Full,
			Value:     strings.ToLower(info.code),
			Attribute: nil,
		})
	}

	cidrInfo := loadCode(cfg, "cidr:")
	for _, info := range cidrInfo {
		tmp := strings.Split(info.code, "/")
		if len(tmp) != 2 {
			return nil, common.NewError("invalid cidr: " + info.code)
		}
		ip := net.ParseIP(tmp[0])
		if ip == nil {
			return nil, common.NewError("invalid cidr ip: " + info.code)
		}
		prefix, err := strconv.ParseInt(tmp[1], 10, 32)
		if err != nil {
			return nil, common.NewError("invalid prefix").Base(err)
		}
		rawCIDRs[info.strategy] = append(rawCIDRs[info.strategy], &v2router.CIDR{
			Ip:     ip,
			Prefix: uint32(prefix),
		})
	}

	for i := range rawDomains {
		client.domains[i] = newDomainMatcher(rawDomains[i])
		client.ips[i] = newIPMatcher(rawCIDRs[i])
	}

	log.Info("router client created")

	runtime.ReadMemStats(&m4)

	log.Debugf("GeoIP rules -> Alloc: %s; TotalAlloc: %s", common.HumanFriendlyTraffic(m2.Alloc-m1.Alloc), common.HumanFriendlyTraffic(m2.TotalAlloc-m1.TotalAlloc))
	log.Debugf("GeoSite rules -> Alloc: %s; TotalAlloc: %s", common.HumanFriendlyTraffic(m3.Alloc-m2.Alloc), common.HumanFriendlyTraffic(m3.TotalAlloc-m2.TotalAlloc))
	log.Debugf("Plaintext rules -> Alloc: %s; TotalAlloc: %s", common.HumanFriendlyTraffic(m4.Alloc-m3.Alloc), common.HumanFriendlyTraffic(m4.TotalAlloc-m3.TotalAlloc))
	log.Debugf("Total(router) -> Alloc: %s; TotalAlloc: %s", common.HumanFriendlyTraffic(m4.Alloc-m1.Alloc), common.HumanFriendlyTraffic(m4.TotalAlloc-m1.TotalAlloc))

	return client, nil
}
