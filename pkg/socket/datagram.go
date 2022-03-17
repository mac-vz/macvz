package socket

import (
	"github.com/sirupsen/logrus"
	"net"
	"os"
	"reflect"
)

func GetFdFromConn(l net.Conn) int {
	v := reflect.ValueOf(l)
	netFD := reflect.Indirect(reflect.Indirect(v).FieldByName("fd"))
	netFD = netFD.FieldByName("pfd")
	fd := int(netFD.FieldByName("Sysfd").Int())
	return fd
}

func ListenUnixGram(path string) *net.UnixConn {
	if _, err := os.Stat(path); err == nil {
		err := os.Remove(path)
		if err != nil {
			logrus.Fatal("Error during delete of unixgram", path, err)
		}
	}
	unixgram, err := net.ListenUnixgram("unixgram", &net.UnixAddr{Name: path, Net: "unixgram"})
	if err != nil {
		logrus.Fatal("Error listening to unixgram", path, err)
	}
	return unixgram
}

func DialUnixGram(clientPath string, serverPath string) *net.UnixConn {
	if _, err := os.Stat(clientPath); err == nil {
		err := os.Remove(clientPath)
		if err != nil {
			logrus.Fatal("Error during delete of client unixgram", clientPath, err)
		}
	}
	unixgram, err := net.DialUnix("unixgram", &net.UnixAddr{Name: clientPath, Net: "unixgram"},
		&net.UnixAddr{Name: serverPath, Net: "unixgram"})
	if err != nil {
		logrus.Fatal("Error listening to unixgram", serverPath, err)
	}
	return unixgram
}
