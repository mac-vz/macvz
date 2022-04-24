package types

import (
	"net"
	"strconv"
)

type Kind = string

const (
	PortMessage        Kind = "port-event"
	InfoMessage        Kind = "info-event"
	DNSMessage         Kind = "dns-event"
	DNSResponseMessage Kind = "dns-event-response"
)

type Event struct {
	Kind Kind `json:"kind"`
}

type InfoEvent struct {
	Event
	LocalPorts []IPPort `json:"localPorts"`
}

type PortEvent struct {
	Event
	Time              string   `json:"time,omitempty"`
	LocalPortsAdded   []IPPort `json:"localPortsAdded,omitempty"`
	LocalPortsRemoved []IPPort `json:"localPortsRemoved,omitempty"`
	Errors            []string `json:"errors,omitempty"`
}

type DNSEvent struct {
	Event
	GatewayIP string `json:"gatewayIP"`
	Msg       []byte `json:"msg"`
}

type DNSEventResponse struct {
	Event
	Msg []byte `json:"msg"`
}

type IPPort struct {
	IP   net.IP `json:"ip"`
	Port int    `json:"port"`
}

func (x *IPPort) String() string {
	return net.JoinHostPort(x.IP.String(), strconv.Itoa(x.Port))
}
