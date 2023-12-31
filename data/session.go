package data

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

type Session struct {
	PoolPointerPosition int64     `json:"pool-position"`
	PoolName            string    `json:"pool-name"`
	EmailIndex          int       `json:"email-index"`
	Boundaries          [][]uint8 `json:"boundaries"`
}

func (s *Session) MarshalJSON() ([]byte, error) {
	var boundaries string

	if s.Boundaries == nil {
		boundaries = "null"
	} else {
		var arrays []string
		for _, array := range s.Boundaries {
			var elements []string
			for _, v := range array {
				elements = append(elements, fmt.Sprintf("%d", v))
			}
			arrays = append(arrays, "["+strings.Join(elements, ",")+"]")
		}
		boundaries = "[" + strings.Join(arrays, ",") + "]"
	}

	jsonResult := fmt.Sprintf(`{"email-index":%d,"pool-name":"%s","pool-position":%d,"boundaries":%s}`, s.EmailIndex, s.PoolName, s.PoolPointerPosition, boundaries)
	return []byte(jsonResult), nil
}

func (s *Session) Init(poolName string, poolPointerPosition int64) {
	s.PoolName = poolName
	s.PoolPointerPosition = poolPointerPosition
	s.EmailIndex = 0
	s.Boundaries = make([][]uint8, 0)
}

func (s *Session) Reset(path string) error {
	var err error

	if err = s.Load(path); err != nil {
		return err
	}
	s.EmailIndex = 0
	return s.Save(path)
}

func (s *Session) AddBoundary(boundary []byte) {
	s.Boundaries = append(s.Boundaries, boundary)
}

func (s *Session) Load(path string) error {
	var err error
	var jsonBytes []byte

	if jsonBytes, err = os.ReadFile(path); err != nil {
		return err
	}
	if err = json.Unmarshal(jsonBytes, s); nil != err {
		return err
	}
	return nil
}

func (s *Session) Save(path string) error {
	var err error
	var jsonBytes []byte

	if jsonBytes, err = json.Marshal(s); nil != err {
		return err
	}
	if err = os.WriteFile(path, jsonBytes, 0644); err != nil {
		return err
	}
	return nil
}
