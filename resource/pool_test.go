package resource

import (
	"github.com/stretchr/testify/assert"
	"os"
	"testing"
)

const poolLength = 256
const sourcePath = "source.dat"
const poolPath = "pool.dat"

func TestMain(m *testing.M) {
	setup()
	code := m.Run()
	cleanUp()
	os.Exit(code)
}

func cleanUp() {
	_ = os.Remove(sourcePath)
	_ = os.Remove(poolPath)
}

func setup() {
	var err error
	var data = make([]byte, poolLength) // 0x00, 0x01, 0x02, 0x03... 0xFF

	for i := 0; i < poolLength; i++ {
		data[i] = byte(i)
	}
	if err = os.WriteFile(sourcePath, data, 0644); err != nil {
		panic(err)
	}
}

func TestCreate(t *testing.T) {
	const poolLength = 256
	const sourcePath = "source.dat"
	const poolPath = "pool.dat"
	var err error
	var info os.FileInfo
	var content []byte
	var p *Pool

	p, err = Create(poolPath, sourcePath)
	assert.Nil(t, err)
	defer p.Close()

	// Check the length of the file.
	info, err = os.Stat(poolPath)
	assert.Nil(t, err)
	assert.Equal(t, int64(poolLength)+int64(positionTypeLength), info.Size())

	// Check the content of the file.
	content, err = os.ReadFile(poolPath)
	assert.Nil(t, err)
	for i := 0; i < positionTypeLength; i++ {
		assert.Equal(t, uint8(0), content[i])
	}
	for i := 0; i < poolLength; i++ {
		assert.Equal(t, uint8(i), content[i+positionTypeLength])
	}
}

func TestOpen(t *testing.T) {
	var err error
	var p *Pool
	var content []byte

	// Create a pool.
	p, err = Create(poolPath, sourcePath)
	assert.Nil(t, err)
	p.Close()

	p, err = Open(poolPath)
	assert.Nil(t, err)
	defer p.Close()

	// Make sure that the position pointer is well set (the position should be 0).
	content, err = os.ReadFile(poolPath)
	assert.Nil(t, err)
	for i := 0; i < positionTypeLength; i++ {
		assert.Equal(t, uint8(0), content[i])
	}
}

func TestGetBytes(t *testing.T) {
	const sliceLength = 2
	var err error
	var p *Pool
	var content *[]byte
	//var pos *int64

	// Create a pool.
	p, err = Create(poolPath, sourcePath)
	assert.Nil(t, err)
	p.Close()

	p, err = Open(poolPath)
	assert.Nil(t, err)
	defer p.Close()

	// Consume all the pool two bytes at a time.
	for i := 0; i < poolLength/sliceLength; i++ {
		content, err = p.GetBytes(sliceLength)
		assert.Nil(t, err)
		assert.Equal(t, sliceLength, len(*content))
		assert.Equal(t, uint8(2*i), (*content)[0])
		assert.Equal(t, uint8(2*i+1), (*content)[1])
	}

	// We'll get an error...
	content, err = p.GetBytes(sliceLength)
	assert.NotNil(t, err)
}
