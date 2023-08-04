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
	path     string
	fd       *os.File
	position int64
}

// Open Opens an existing pool identified by its path.
func Open(filePath string) (*Pool, error) {
	var err error
	var fd *os.File
	var p Pool
	var position *int64

	if fd, err = os.OpenFile(filePath, os.O_RDWR, 0644); err != nil {
		return nil, err
	}
	p = Pool{path: filePath, fd: fd, position: 0}
	// Retrieve the position of the position pointer from the underlying file.
	if position, err = p.getPositionFromFile(); err != nil {
		return nil, err
	}
	// Set the position pointer at `position`.
	if err = p.seek(*position); err != nil {
		return nil, err
	}
	p.position = *position
	return &Pool{path: filePath, fd: fd, position: *position}, nil
}

// Create Creates a new pool from the content of a file.
func Create(poolPath string, filePath string) (*Pool, error) {
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

	// Initialise the position of the position pointer.
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
	pool := Pool{path: poolPath, fd: fdPool, position: 0}
	if err = pool.seek(pool.position); err != nil {
		return nil, err
	}
	return &pool, nil
}

func (p *Pool) Close() error {
	return p.fd.Close()
}

// GetBytes Retrieves `count` bytes from the pool.
func (p *Pool) GetBytes(count int64) (*[]byte, error) {
	var err error
	var buffer = make([]byte, count)
	var newPosition = p.position + count

	if count <= 0 {
		panic(fmt.Errorf(`invalid number of bytes (%d)`, count))
	}
	if _, err = io.ReadFull(p.fd, buffer); err != nil {
		return nil, fmt.Errorf(`cannot extract %d bytes from pool "%s", from position %d: %s`, count, p.path, p.position, err.Error())
	}
	if err = p.setPositionToFile(newPosition); err != nil {
		return nil, err
	}
	if err = p.seek(newPosition); err != nil {
		return nil, err
	}
	p.position = newPosition
	return &buffer, nil
}

// getPositionFromFile Retrieves the current position of the position pointer from the underlying file.
// Please note that a call to this method:
// - does not (re)position the position pointer. To (re)position the position pointer, you must use `seek()`.
// - does not modify the value of `p.position`.
// Warning: if you call this method within a unit test, then keep in mind that the position pointer will be
// repositioned to the beginning of the sequence of bytes!
func (p *Pool) getPositionFromFile() (*int64, error) {
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
		return nil, fmt.Errorf(`invalid pool "%s": no position found`, p.path)
	}
	if err = binary.Read(bytes.NewReader(buffer), binary.LittleEndian, &position); err != nil {
		return nil, err
	}
	if position < 0 {
		return nil, fmt.Errorf(`invalid pool "%s": invalid pool position (%d)`, p.path, position)
	}
	return &position, nil
}

// setPositionToFile Sets the position pointer to `position` within the underlying file.
// Please note that a call to this method:
// - does not (re)position the position pointer. To (re)position the position pointer, you must use `seek()`.
// - does not modify the value of `p.position`.
// Warning: if you call this method within a unit test, then keep in mind that the position pointer will be
// repositioned to the beginning of the sequence of bytes!
func (p *Pool) setPositionToFile(position int64) error {
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

// seek Sets the position pointer to `position`.
// Please keep in mind that this method does not modify the value of `p.position`.
func (p *Pool) seek(position int64) error {
	_, err := p.fd.Seek(position+positionTypeLength, io.SeekStart)
	return err
}
