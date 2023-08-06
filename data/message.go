package data

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
	"os"
)

type Message [][]byte

func (m *Message) Load(filePath string, chunkSize int) error {
	var err error
	var raw []byte
	var rawLength int
	var message []byte
	var messageLength int
	var remainder int
	var buffer = new(bytes.Buffer)

	if raw, err = os.ReadFile(filePath); err != nil {
		return err
	}
	rawLength = len(raw)

	// Check the length of the message.
	if rawLength > math.MaxUint16 {
		return fmt.Errorf(`the given message is too long (%d bytes). The maximum length is %d`, rawLength, math.MaxUint16)
	}
	if err = binary.Write(buffer, binary.LittleEndian, uint16(rawLength)); err != nil {
		return err
	}
	message = append(buffer.Bytes(), raw...)
	messageLength = len(message)
	for i := 0; i < messageLength/chunkSize; i++ {
		*m = append(*m, message[i*chunkSize:(i+1)*chunkSize])
	}
	remainder = messageLength % chunkSize
	if remainder > 0 {
		var padding = make([]byte, chunkSize-remainder)
		*m = append(*m, append(message[chunkSize*len(*m):], padding...))
	}
	return nil
}

func (m *Message) BoundariesCount() int {
	return len(*m)
}
