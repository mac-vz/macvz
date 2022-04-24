package socket

import (
	"github.com/fxamacker/cbor/v2"
	"github.com/hashicorp/yamux"
	"github.com/mitchellh/mapstructure"
	"github.com/sirupsen/logrus"
)

//GetIO Open a yamux connection and returns encoder and decoder
func GetIO(sess *yamux.Session) (*cbor.Encoder, *cbor.Decoder) {
	out, err := sess.Open()
	if err != nil {
		logrus.Error("error opening yamux session", err)
		return nil, nil
	}
	return cbor.NewEncoder(out), cbor.NewDecoder(out)
}

//GetStreamIO Creates encoder and decoder for yamux stream
func GetStreamIO(c *yamux.Stream) (*cbor.Encoder, *cbor.Decoder) {
	return cbor.NewEncoder(c), cbor.NewDecoder(c)
}

//Write encodes the given obj
func Write(enc *cbor.Encoder, obj interface{}) {
	err := enc.Encode(obj)
	if err != nil {
		logrus.Error("error encoding value", err)
	}
}

//Read decodes the value to obj
func Read(dec *cbor.Decoder, obj interface{}) {
	err := dec.Decode(obj)
	if err != nil {
		logrus.Error("error encoding value", err)
	}
}

//ReadMap decodes src to dest interface
func ReadMap(src map[string]interface{}, dest interface{}) {
	err := mapstructure.Decode(src, dest)
	if err != nil {
		logrus.Error("error encoding value", err)
	}
}
