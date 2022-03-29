package socket

import (
	"bytes"
	"github.com/sirupsen/logrus"
	"net"
	"strings"
)

type VsockConnection struct {
	Conn net.Conn
}

func readAsync(conn net.Conn) chan []byte {
	c := make(chan []byte)

	go func() {
		b := make([]byte, 1024)

		for {
			n, err := conn.Read(b)
			if n > 0 {
				res := make([]byte, n)
				// Copy the buffer so it doesn't get changed while read by the recipient.
				copy(res, b[:n])
				c <- res
			}
			if err != nil {
				logrus.Error("Error reading data", err)
				c <- nil
				break
			}
		}
	}()

	return c
}

func (sock VsockConnection) WriteEvents(event string) {
	_, err := sock.Conn.Write([]byte(strings.TrimSpace(event) + "<<EOF>>"))
	if err != nil {
		logrus.Warn(err)
		return
	}
}

func (sock VsockConnection) ReadEvents(onData func(string)) {
	var buffer bytes.Buffer

	charCh := readAsync(sock.Conn)

	for {
		select {
		case b1 := <-charCh:
			if b1 != nil && len(b1) > 0 {
				_, err := buffer.Write(b1)
				fullStr := buffer.String()
				if strings.Contains(fullStr, "<<EOF>>") {
					parts := strings.Split(fullStr, "<<EOF>>")
					onData(parts[0])
					buffer.Reset()
					for i := range parts {
						if i != 0 {
							buffer.WriteString(parts[i])
						}
					}
				}
				if err != nil {
					logrus.Error("Error writing data from b1", err)
				}
			}
		}
	}
}
