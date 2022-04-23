package types

import (
	"net"
	"strconv"
	"time"
)

type Message = string

const (
	PortMessage Message = "port-event"
	InfoMessage Message = "info-event"
)

type InfoEvent struct {
	Kind       Message  `json:"kind"`
	LocalPorts []IPPort `json:"localPorts"`
}

type PortEvent struct {
	Kind              Message   `json:"kind"`
	Time              time.Time `json:"time,omitempty"`
	LocalPortsAdded   []IPPort  `json:"localPortsAdded,omitempty"`
	LocalPortsRemoved []IPPort  `json:"localPortsRemoved,omitempty"`
	Errors            []string  `json:"errors,omitempty"`
}

type IPPort struct {
	IP   net.IP `json:"ip"`
	Port int    `json:"port"`
}

func (x *IPPort) String() string {
	return net.JoinHostPort(x.IP.String(), strconv.Itoa(x.Port))
}
