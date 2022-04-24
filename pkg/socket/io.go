package socket

import (
	"github.com/fxamacker/cbor/v2"
	"github.com/hashicorp/yamux"
	"github.com/mitchellh/mapstructure"
	"github.com/sirupsen/logrus"
)

func GetIO(sess *yamux.Session) (*cbor.Encoder, *cbor.Decoder) {
	out, err := sess.Open()
	if err != nil {
		logrus.Error("error opening yamux session", err)
		return nil, nil
	}
	return cbor.NewEncoder(out), cbor.NewDecoder(out)
}

func GetStreamIO(c *yamux.Stream) (*cbor.Encoder, *cbor.Decoder) {
	return cbor.NewEncoder(c), cbor.NewDecoder(c)
}

func Write(enc *cbor.Encoder, obj interface{}) {
	err := enc.Encode(obj)
	if err != nil {
		logrus.Error("error encoding value", err)
	}
}

func Read(dec *cbor.Decoder, obj interface{}) {
	err := dec.Decode(obj)
	if err != nil {
		logrus.Error("error encoding value", err)
	}
}

func ReadMap(src map[string]interface{}, dest interface{}) {
	err := mapstructure.Decode(src, dest)
	if err != nil {
		logrus.Error("error encoding value", err)
	}
}
