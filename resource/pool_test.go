package resource

import (
	"github.com/stretchr/testify/assert"
	"os"
	"testing"
)

func TestPoolCreate(t *testing.T) {
	var err error
	var info os.FileInfo
	var content []byte
	var p *Pool

	p, err = PoolCreate(poolPath, sourcePath)
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

func TestPoolOpen(t *testing.T) {
	var err error
	var p *Pool
	var content []byte

	// PoolCreate a pool.
	p, err = PoolCreate(poolPath, sourcePath)
	assert.Nil(t, err)
	p.Close()

	p, err = PoolOpen(poolPath)
	assert.Nil(t, err)
	defer p.Close()

	// Make sure that the Position pointer is well set (the Position should be 0).
	content, err = os.ReadFile(poolPath)
	assert.Nil(t, err)
	for i := 0; i < positionTypeLength; i++ {
		assert.Equal(t, uint8(0), content[i])
	}
}

func TestPoolGetBytes(t *testing.T) {
	const sliceLength = 2
	var err error
	var p *Pool
	var content *[]byte

	// PoolCreate a pool.
	p, err = PoolCreate(poolPath, sourcePath)
	assert.Nil(t, err)
	p.Close()

	p, err = PoolOpen(poolPath)
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
	_, err = p.GetBytes(sliceLength)
	assert.NotNil(t, err)
}

func TestPoolGetBytesAsChunks(t *testing.T) {
	const sliceLength = 2
	var err error
	var p *Pool
	var chunks *[][]byte

	// PoolCreate a pool.
	p, err = PoolCreate(poolPath, sourcePath)
	assert.Nil(t, err)
	p.Close()

	p, err = PoolOpen(poolPath)
	assert.Nil(t, err)
	defer p.Close()

	chunks, err = p.GetBytesAsChunks(poolLength/sliceLength, sliceLength)
	assert.Nil(t, err)
	assert.Equal(t, poolLength/sliceLength, len(*chunks))
	for i := 0; i < poolLength/sliceLength; i++ {
		assert.Len(t, (*chunks)[i], 2)
		assert.Equal(t, byte(i*sliceLength), (*chunks)[i][0])
		assert.Equal(t, byte(i*sliceLength+1), (*chunks)[i][1])
	}

	// We'll get an error...
	_, err = p.GetBytes(sliceLength)
	assert.NotNil(t, err)
}
