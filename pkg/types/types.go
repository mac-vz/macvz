package types

import (
	"net"
	"strconv"
)

//Kind Enum that defines the type of message
type Kind = string

const (
	//PortMessage PortEvent kind
	PortMessage Kind = "port-event"
	//InfoMessage InfoEvent kind
	InfoMessage Kind = "info-event"
	//DNSMessage DNSEvent kind
	DNSMessage Kind = "dns-event"
	//DNSResponseMessage DNSEventResponse kind
	DNSResponseMessage Kind = "dns-event-response"
)

//Event base type for all event
type Event struct {
	Kind Kind `json:"kind"`
}

//InfoEvent used by guest to send negotitation request
type InfoEvent struct {
	Event
	LocalPorts []IPPort `json:"localPorts"`
}

//PortEvent used by guest to send port binding events
type PortEvent struct {
	Event
	Time              string   `json:"time,omitempty"`
	LocalPortsAdded   []IPPort `json:"localPortsAdded,omitempty"`
	LocalPortsRemoved []IPPort `json:"localPortsRemoved,omitempty"`
	Errors            []string `json:"errors,omitempty"`
}

//DNSEvent used by guest to send DNS request
type DNSEvent struct {
	Event
	GatewayIP string `json:"gatewayIP"`
	Msg       []byte `json:"msg"`
}

//DNSEventResponse used by host to send DNS response
type DNSEventResponse struct {
	Event
	Msg []byte `json:"msg"`
}

//IPPort Used by PortEvent for IP and Port representation
type IPPort struct {
	IP   net.IP `json:"ip"`
	Port int    `json:"port"`
}

func (x *IPPort) String() string {
	return net.JoinHostPort(x.IP.String(), strconv.Itoa(x.Port))
}
