package protocol

import (
	"encoding/json"
	"time"
)

type PackageType uint8

const (
	PackageTypeRequest PackageType = iota
	PackageTypeResponse
	PackageTypeUpdates
)

type Package struct {
	Type PackageType     `json:"type"`
	Data json.RawMessage `json:"data"`
}

type PackageDataRequest struct {
	ID     string          `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

type ResponseCode int32

type PackageDataResponse struct {
	ID   string          `json:"id"`
	Code ResponseCode    `json:"code"`
	Msg  string          `json:"msg"`
	Data json.RawMessage `json:"data"`
}

type PackageDataUpdates struct {
	Updates []PackageDataUpdate `json:"updates"`
}

type PackageDataUpdate struct {
	Seq       uint32          `json:"seq"`
	Payload   json.RawMessage `json:"payload"`
	CreatedAt time.Time
}
