// This file has been adapted from https://github.com/norouter/norouter/blob/v0.6.4/pkg/agent/dns/dns.go

package dns

import (
	"fmt"
	"github.com/mac-vz/macvz/pkg/yaml"
	"github.com/miekg/dns"
	"github.com/sirupsen/logrus"
	"net"
	"strings"
)

// Truncate for avoiding "Parse error" from `busybox nslookup`
// https://github.com/lima-vm/lima/issues/380
const truncateSize = 512

type Handler struct {
	clientConfig *dns.ClientConfig
	clients      []*dns.Client
	IPv6         bool
	cname        map[string]string
	ip           map[string]net.IP
}

func newStaticClientConfig(ips []net.IP) (*dns.ClientConfig, error) {
	s := ``
	for _, ip := range ips {
		s += fmt.Sprintf("nameserver %s\n", ip.String())
	}
	r := strings.NewReader(s)
	return dns.ClientConfigFromReader(r)
}

func CreateHandler(IPv6 bool) (*Handler, error) {
	cc, err := dns.ClientConfigFromFile("/etc/resolv.conf")
	if err != nil {
		fallbackIPs := []net.IP{net.ParseIP("8.8.8.8"), net.ParseIP("1.1.1.1")}
		logrus.WithError(err).Warnf("failed to detect system DNS, falling back to %v", fallbackIPs)
		cc, err = newStaticClientConfig(fallbackIPs)
		if err != nil {
			return nil, err
		}
	}
	clients := []*dns.Client{
		{}, // UDP
		{Net: "tcp"},
	}
	h := &Handler{
		clientConfig: cc,
		clients:      clients,
		IPv6:         IPv6,
		cname:        make(map[string]string),
		ip:           make(map[string]net.IP),
	}
	return h, nil
}

func (h *Handler) handleQuery(req *dns.Msg) *dns.Msg {
	var (
		reply   dns.Msg
		handled bool
	)
	reply.SetReply(req)
	for _, q := range req.Question {
		hdr := dns.RR_Header{
			Name:   q.Name,
			Rrtype: q.Qtype,
			Class:  q.Qclass,
			Ttl:    5,
		}
		qtype := q.Qtype
		switch qtype {
		case dns.TypeAAAA:
			if !h.IPv6 {
				// A "correct" answer would be to set `handled = true` and return a NODATA response.
				// Unfortunately some older resolvers use a slow random source to set the transaction id.
				// This creates a problem on M1 computers, which are too fast for that implementation:
				// Both the A and AAAA queries might end up with the same id. Returning NODATA for AAAA
				// is faster, so would arrive first, and be treated as the response to the A query.
				// To avoid this, we will treat an AAAA query as an A query when IPv6 has been disabled.
				// This way it is either a valid response for an A query, or the A records will be discarded
				// by a genuine AAAA query, resulting in the desired NODATA response.
				qtype = dns.TypeA
			}
			fallthrough
		case dns.TypeCNAME, dns.TypeA:
			cname := q.Name
			seen := make(map[string]bool)
			for {
				// break cyclic definition
				if seen[cname] {
					break
				}
				if _, ok := h.cname[cname]; ok {
					seen[cname] = true
					cname = h.cname[cname]
					continue
				}
				break
			}
			var err error
			if _, ok := h.ip[cname]; !ok {
				cname, err = net.LookupCNAME(cname)
				if err != nil {
					break
				}
			}
			if cname != "" && cname != q.Name {
				hdr.Rrtype = dns.TypeCNAME
				a := &dns.CNAME{
					Hdr:    hdr,
					Target: cname,
				}
				reply.Answer = append(reply.Answer, a)
				handled = true
			}
			if qtype == dns.TypeCNAME {
				break
			}
			hdr.Name = cname
			var addrs []net.IP
			if _, ok := h.ip[cname]; ok {
				addrs = []net.IP{h.ip[cname]}
				err = nil
			} else {
				addrs, err = net.LookupIP(cname)
			}
			if err == nil && len(addrs) > 0 {
				for _, ip := range addrs {
					var a dns.RR
					ipv6 := ip.To4() == nil
					if qtype == dns.TypeA && !ipv6 {
						hdr.Rrtype = dns.TypeA
						a = &dns.A{
							Hdr: hdr,
							A:   ip.To4(),
						}
					} else if qtype == dns.TypeAAAA && ipv6 {
						hdr.Rrtype = dns.TypeAAAA
						a = &dns.AAAA{
							Hdr:  hdr,
							AAAA: ip.To16(),
						}
					} else {
						continue
					}
					reply.Answer = append(reply.Answer, a)
					handled = true
				}
			}
		case dns.TypeTXT:
			txt, err := net.LookupTXT(q.Name)
			if err == nil && len(txt) > 0 {
				a := &dns.TXT{
					Hdr: hdr,
					Txt: txt,
				}
				reply.Answer = append(reply.Answer, a)
				handled = true
			}
		case dns.TypeNS:
			ns, err := net.LookupNS(q.Name)
			if err == nil && len(ns) > 0 {
				for _, s := range ns {
					if s.Host != "" {
						a := &dns.NS{
							Hdr: hdr,
							Ns:  s.Host,
						}
						reply.Answer = append(reply.Answer, a)
						handled = true
					}
				}
			}
		case dns.TypeMX:
			mx, err := net.LookupMX(q.Name)
			if err == nil && len(mx) > 0 {
				for _, s := range mx {
					if s.Host != "" {
						a := &dns.MX{
							Hdr:        hdr,
							Mx:         s.Host,
							Preference: s.Pref,
						}
						reply.Answer = append(reply.Answer, a)
						handled = true
					}
				}
			}
		case dns.TypeSRV:
			_, addrs, err := net.LookupSRV("", "", q.Name)
			if err == nil {
				hdr.Rrtype = dns.TypeSRV
				for _, addr := range addrs {
					a := &dns.SRV{
						Hdr:      hdr,
						Target:   addr.Target,
						Port:     addr.Port,
						Priority: addr.Priority,
						Weight:   addr.Weight,
					}
					reply.Answer = append(reply.Answer, a)
					handled = true
				}
			}
		}
	}
	if handled {
		reply.Truncate(truncateSize)
		return &reply
	}
	return h.handleDefault(req)
}

func (h *Handler) handleDefault(req *dns.Msg) *dns.Msg {
	for _, client := range h.clients {
		for _, srv := range h.clientConfig.Servers {
			addr := fmt.Sprintf("%s:%s", srv, h.clientConfig.Port)
			reply, _, err := client.Exchange(req, addr)
			if err == nil {
				reply.Truncate(truncateSize)
				return reply
			}
		}
	}
	var reply dns.Msg
	reply.SetReply(req)
	reply.Truncate(truncateSize)
	return &reply
}

func (h *Handler) HandleDNSRequest(req []byte) *dns.Msg {
	var (
		original dns.Msg
	)
	_ = original.Unpack(req)
	switch original.Opcode {
	case dns.OpcodeQuery:
		return h.handleQuery(&original)
	default:
		return h.handleDefault(&original)
	}
}

func (h *Handler) UpdateDefaults(hosts map[string]string) {
	for host, address := range hosts {
		if ip := net.ParseIP(address); ip != nil {
			h.ip[host] = ip
		} else {
			h.cname[host] = yaml.Cname(address)
		}
	}
}
