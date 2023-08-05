package data

import (
	"bytes"
	"encoding/binary"
	"github.com/stretchr/testify/assert"
	"os"
	"testing"
)

const chunkSize = 35

func TestMessageLoad(t *testing.T) {
	var m Message
	var message []byte
	var err error
	var buffer = new(bytes.Buffer)
	var chunk []byte

	// Create a test file.
	for i := 0; i < 2; i++ {
		for j := 0; j < chunkSize; j++ {
			message = append(message, 'A'+byte(i))
		}
	}
	err = os.WriteFile(messageFile, message, 0644)
	assert.Nil(t, err)

	// Load the message.
	err = m.Load(messageFile, chunkSize)
	assert.Nil(t, err)

	assert.Len(t, m, 3)

	err = binary.Write(buffer, binary.LittleEndian, uint16(len(message)))
	assert.Nil(t, err)

	// Chunk 1
	chunk = append(chunk, buffer.Bytes()...)
	for i := 0; i < chunkSize-2; i++ {
		chunk = append(chunk, 'A')
	}
	assert.Equal(t, chunk, m[0])

	// Chunk 2
	chunk = []byte{'A', 'A'}
	for i := 0; i < chunkSize-2; i++ {
		chunk = append(chunk, 'B')
	}
	assert.Equal(t, chunk, m[1])

	// Chunk 3
	chunk = []byte{'B', 'B'}
	for i := 0; i < chunkSize-2; i++ {
		chunk = append(chunk, 0)
	}
	assert.Equal(t, chunk, m[2])
}
