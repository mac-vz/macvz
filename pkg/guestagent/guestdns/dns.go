// This file has been adapted from https://github.com/norouter/norouter/blob/v0.6.4/pkg/agent/dns/dns.go

package guestdns

import (
	"fmt"
	"github.com/hashicorp/yamux"
	"github.com/joho/godotenv"
	"github.com/mac-vz/macvz/pkg/socket"
	"github.com/mac-vz/macvz/pkg/types"
	"github.com/miekg/dns"
	"github.com/sirupsen/logrus"
)

type Handler struct {
	yamux     *yamux.Session
	gatewayIp string
}

type Server struct {
	udp *dns.Server
	tcp *dns.Server
}

func (s *Server) Shutdown() {
	if s.udp != nil {
		_ = s.udp.Shutdown()
	}
	if s.tcp != nil {
		_ = s.tcp.Shutdown()
	}
}

func newHandler(yamux *yamux.Session) (dns.Handler, error) {
	ips, err := godotenv.Read("/etc/macvz_hosts")
	if err != nil {
		logrus.Error("Unable to fetch predefined hosts")
	}
	h := &Handler{
		yamux:     yamux,
		gatewayIp: ips["GATEWAY_IPADDR"],
	}
	return h, nil
}

func (h *Handler) ServeDNS(w dns.ResponseWriter, req *dns.Msg) {
	encoder, decoder := socket.GetIO(h.yamux)
	if encoder != nil && decoder != nil {
		//Construct DNSEvent and send request to host
		event := types.DNSEvent{}
		event.Kind = types.DNSMessage
		pack, _ := req.Pack()
		event.Msg = pack
		event.GatewayIP = h.gatewayIp
		socket.Write(encoder, &event)

		//Read DNS response from host
		var reply dns.Msg
		dnsRes := types.DNSEventResponse{}
		socket.Read(decoder, &dnsRes)
		_ = reply.Unpack(dnsRes.Msg)

		//Write the response back to dns writer
		_ = w.WriteMsg(&reply)
	}
}

func Start(udpLocalPort, tcpLocalPort int, yamux *yamux.Session) (*Server, error) {
	h, err := newHandler(yamux)
	if err != nil {
		return nil, err
	}
	server := &Server{}
	if udpLocalPort > 0 {
		addr := fmt.Sprintf("0.0.0.0:%d", udpLocalPort)
		s := &dns.Server{Net: "udp", Addr: addr, Handler: h}
		server.udp = s
		go func() {
			if e := s.ListenAndServe(); e != nil {
				panic(e)
			}
		}()
	}
	if tcpLocalPort > 0 {
		addr := fmt.Sprintf("0.0.0.0:%d", tcpLocalPort)
		s := &dns.Server{Net: "tcp", Addr: addr, Handler: h}
		server.tcp = s
		go func() {
			if e := s.ListenAndServe(); e != nil {
				panic(e)
			}
		}()
	}
	return server, nil
}
