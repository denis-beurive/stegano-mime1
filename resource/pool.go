package resource

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"os"
)

const positionTypeLength = 8 // the size, in bytes, of "int64"

type Pool struct {
	Path     string
	fd       *os.File
	Position int64
}

// PoolOpen Opens an existing pool identified by its Path.
func PoolOpen(filePath string) (*Pool, error) {
	var err error
	var fd *os.File
	var p Pool
	var position *int64

	if fd, err = os.OpenFile(filePath, os.O_RDWR, 0644); err != nil {
		return nil, err
	}
	p = Pool{Path: filePath, fd: fd, Position: 0}
	// Retrieve the Position of the Position pointer from the underlying file.
	if position, err = p.GetPositionFromFile(true); err != nil {
		return nil, err
	}
	p.Position = *position
	return &Pool{Path: filePath, fd: fd, Position: *position}, nil
}

// PoolCreate Creates a new pool from the content of a file.
func PoolCreate(poolPath string, filePath string) (*Pool, error) {
	const bufferLength = 1024
	var err error
	var fdFile *os.File
	var fdPool *os.File
	var position = make([]byte, positionTypeLength)
	var count int

	if fdFile, err = os.Open(filePath); err != nil {
		return nil, err
	}
	defer fdFile.Close()
	if fdPool, err = os.OpenFile(poolPath, os.O_CREATE, 0644); err != nil {
		return nil, err
	}

	// Initialise the Position of the Position pointer.
	if _, err = fdPool.Write(position); err != nil {
		return nil, err
	}

	for {
		var buffer = make([]byte, bufferLength)
		if count, err = fdFile.Read(buffer); err != nil && err != io.EOF {
			fdPool.Close()
			return nil, err
		}
		if count > 0 {
			if _, err = fdPool.Write(buffer[0:count]); err != nil {
				fdPool.Close()
				return nil, err
			}
		}
		if count < bufferLength {
			break
		}
	}

	// Create the new pool.
	pool := Pool{Path: poolPath, fd: fdPool, Position: 0}
	if err = pool.seek(pool.Position); err != nil {
		return nil, err
	}
	return &pool, nil
}

func (p *Pool) Close() error {
	return p.fd.Close()
}

// GetBytes Retrieves `count` bytes from the pool, starting as the current position pointer's position.
// Please note that this method does *NOT* set the position of the position pointer prior retrieving bytes.
// The position pointer should have been moved to its current position while the pool has been opened (by calling
// `PoolOpen`)
func (p *Pool) GetBytes(count int64) (*[]byte, error) {
	var err error
	var buffer = make([]byte, count)
	var newPosition = p.Position + count

	if count <= 0 {
		panic(fmt.Errorf(`invalid number of bytes (%d)`, count))
	}
	if _, err = io.ReadFull(p.fd, buffer); err != nil {
		return nil, fmt.Errorf(`cannot extract %d bytes from pool "%s", from Position %d: %s`, count, p.Path, p.Position, err.Error())
	}
	if err = p.SetPositionToFile(newPosition); err != nil {
		return nil, err
	}
	if err = p.seek(newPosition); err != nil {
		return nil, err
	}
	p.Position = newPosition
	return &buffer, nil
}

func (p *Pool) GetBytesAsChunks(chunkCount int64, chunkLength int64) (*[][]byte, error) {
	var err error
	var buffer *[]byte
	var result [][]byte

	if buffer, err = p.GetBytes(chunkCount * chunkLength); err != nil {
		return nil, err
	}
	for i := int64(0); i < chunkCount; i++ {
		result = append(result, (*buffer)[i*chunkLength:(i+1)*chunkLength])
	}
	return &result, nil
}

// GetPositionFromFile Retrieves the value of position pointer's position from the underlying file.
// Please note that a call to this method:
// - does *NOT* (re)Position the Position pointer, unless `seek` is set to `true`.
// - does *NOT* modify the value of `p.Position`.
func (p *Pool) GetPositionFromFile(seek bool) (*int64, error) {
	var err error
	var buffer = make([]byte, positionTypeLength)
	var n int
	var position int64

	if _, err = p.fd.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	if n, err = p.fd.Read(buffer); err != nil && err != io.EOF {
		return nil, err
	}
	if n != positionTypeLength {
		return nil, fmt.Errorf(`invalid pool "%s": no Position found`, p.Path)
	}
	if err = binary.Read(bytes.NewReader(buffer), binary.LittleEndian, &position); err != nil {
		return nil, err
	}
	if position < 0 {
		return nil, fmt.Errorf(`invalid pool "%s": invalid pool Position (%d)`, p.Path, position)
	}
	if seek {
		if err = p.seek(position); err != nil {
			return nil, err
		}
	}

	return &position, nil
}

// SetPositionToFile Sets the value of the position pointer's position to `Position` within the underlying file.
// Please note that a call to this method:
// - does *NOT* (re)Position the Position pointer. To (re)Position the Position pointer, you must use `seek()`.
// - does *NOT* modify the value of `p.Position`.
func (p *Pool) SetPositionToFile(position int64) error {
	var err error
	var positionBuffer = new(bytes.Buffer)

	if err = binary.Write(positionBuffer, binary.LittleEndian, position); err != nil {
		return err
	}
	if _, err = p.fd.Seek(0, io.SeekStart); err != nil {
		return err
	}
	_, err = p.fd.Write(positionBuffer.Bytes())
	return err
}

// seek Sets the Position pointer to `Position`.
// Please keep in mind that this method does not modify the value of `p.Position`.
func (p *Pool) seek(position int64) error {
	_, err := p.fd.Seek(position+positionTypeLength, io.SeekStart)
	return err
}
