package main

import (
	"github.com/mdlayher/vsock"
	"log"
	"net/http"
)

// Easier to get running with CORS. Thanks for help @Vindexus and @erkie
var _ = func(r *http.Request) bool {
	return true
}

func SocketListener(vsock *vsock.Conn) error {
	log.Println("Serving at vsock...")
	_, err := vsock.Write([]byte("Connected\n"))

	if err != nil {
		return err
	}

	////Reader go routine
	//go func() {
	//	for {
	//		accept, err := server.Accept()
	//		if err != nil {
	//			logrus.Fatal("Error connecting server", err)
	//		}
	//		reader := bufio.NewReader(accept)
	//		data, err := reader.ReadString('\n')
	//		if err != nil {
	//			log.Fatal("Error in reading from socket", err)
	//		}
	//		log.Printf("Data received", data)
	//	}
	//}()

	//Writer go routine
	for {
		s := "Write check"
		writeSize, err := vsock.Write([]byte(s))
		if err != nil {
			log.Fatal("Error in reading from socket", err)
		}
		if writeSize != len(s) {
			log.Fatal("Not all bytes are written", err)
		}
	}
}
