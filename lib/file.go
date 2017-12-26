package lib

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"sync"

	"github.com/ysmood/portal/lib/utils"
)

// StringBytes ...
type StringBytes []byte

const maxQuota = uint64(18446744073709551615)
const maxGispFileSize = 512 * 1024
const maxConcurrent = uint64(10000000)

// MarshalJSON ...
func (b StringBytes) MarshalJSON() ([]byte, error) {
	return json.Marshal(string(b))
}

// File ...
type File struct {
	ID          string      `json:"id"`
	URI         string      `json:"uri"`
	Type        FileType    `json:"type"`
	ModifierID  string      `json:"modifierId"`
	RootID      string      `json:"rootId"`
	ModifyTime  string      `json:"modifyTime"`
	Headers     [][]byte    `json:"-"`
	ETag        StringBytes `json:"etag,string"`
	Body        StringBytes `json:"body,string"`
	GzippedBody []byte      `json:"-"`
	Code        interface{} `json:"code"`
	JSONBody    interface{} `json:"-"` // used for gisp cache
	ContentType string      `json:"-"` // TODO: hack the double set of fasthttp Content-Type header
	Quota       uint64      `json:"quota"`
	Cost        uint64      `json:"cost"`
	Concurrent  uint32      `json:"concurrent"`
	Count       uint64      `json:"count"`
	dependents  *dependentSet
}

// FileType ...
type FileType int

// String ...
func (t FileType) String() string {
	switch t {
	case 0:
		return "Json"
	case 1:
		return "Text"
	case 2:
		return "Gisp"
	case 3:
		return "Proxy"
	case 4:
		return "Binary"
	case 5:
		return "Overload"
	case 6:
		return "NotFound"
	default:
		return "Unknown"
	}
}

// MarshalJSON ...
func (t FileType) MarshalJSON() ([]byte, error) {
	return []byte(`"` + t.String() + `"`), nil
}

const (
	fileTypeJSON     = 0
	fileTypeText     = 1
	fileTypeGisp     = 2
	fileTypeProxy    = 3
	fileTypeBinary   = 4
	fileTypeOverload = 5
	fileTypeNotFound = 6

	eTag        = "ETag"
	ifNoneMatch = "If-None-Match"
)

type dependentSet struct {
	dict map[*File]bool
	lock *sync.RWMutex
}

var overloadFile = &File{
	Type: fileTypeOverload,
	Body: []byte(http.StatusText(statusTooManyRequests)),
}

func newDependentSet() *dependentSet {
	return &dependentSet{
		dict: map[*File]bool{},
		lock: &sync.RWMutex{},
	}
}

func (dp *dependentSet) Add(f *File) {
	dp.lock.RLock()
	_, has := dp.dict[f]
	dp.lock.RUnlock()

	if !has {
		dp.lock.Lock()
		dp.dict[f] = true
		dp.lock.Unlock()
	}
}

func (dp *dependentSet) Del(f *File) {
	dp.lock.RLock()
	_, has := dp.dict[f]
	dp.lock.RUnlock()

	if !has {
		dp.lock.Lock()
		delete(dp.dict, f)
		dp.lock.Unlock()
	}
}

func (dp *dependentSet) List() []*File {
	dp.lock.RLock()

	list := make([]*File, len(dp.dict))
	i := 0
	for f := range dp.dict {
		list[i] = f
		i++
	}

	dp.lock.RUnlock()
	return list
}

func isTextFile(fileType FileType) bool {
	return fileType == fileTypeJSON || fileType == fileTypeText
}

// header should contain the const variable above
func newFile(uri string, header map[string]string, body []byte) *File {
	headers := make([][]byte, 0)
	var gisp interface{}
	var etag StringBytes
	fileType := FileType(0)
	var id string
	var modifierID string
	var rootID string
	var modifyTime string
	var contentType string
	var gzippedBody []byte
	quota := maxQuota
	concurrent := maxConcurrent

	for k, v := range header {
		switch k {
		case "Portm-Id":
			id = v
			continue
		case "Portm-Modifier-Id":
			modifierID = v
			continue
		case "Portm-Root-Id":
			rootID = v
			continue
		case "Portm-Quota":
			quota, _ = strconv.ParseUint(v, 10, 64)
			continue
		case "Portm-Concurrent":
			concurrent, _ = strconv.ParseUint(v, 10, 32)
			continue
		case "Portm-Modify-Time":
			modifyTime = v
			continue
		case "Portm-Type":
			switch v {
			case "Json":
				fileType = 0
			case "Text":
				fileType = 1
			case "Binary":
				fileType = 4
			case "Proxy":
				fallthrough
			case "Gisp":
				fileType = 2

				if v == "Proxy" {
					fileType = 3
				}

				if len(body) > maxGispFileSize {
					fileType = 4
					body = []byte(fmt.Sprintf("gisp file exceeded max size %vB", maxGispFileSize))
					continue
				}

				err := json.Unmarshal(body, &gisp)
				if err != nil {
					fileType = 4
					fmt.Fprintln(os.Stderr, "newFile:", string(uri), err.Error())
					body = []byte(err.Error())
				}
			}
			continue
		case "Portm-Not-Found":
			fileType = 6
			continue
		case "Content-Type":
			contentType = v
			continue
		case "Content-Length":
			continue
		}

		headers = append(headers, []byte(k), []byte(v))
	}

	if len(body) > gzipMinSize && isTextFile(fileType) {
		gzippedBody = utils.Gzip(body)
	}

	if fileType == fileTypeBinary && utils.IsTextMIME(contentType) {
		gzippedBody = utils.Gzip(body)
	}

	if gisp == nil && body != nil {
		etag = utils.ETag(body)
	}

	return &File{
		Type:        fileType,
		ModifierID:  modifierID,
		RootID:      rootID,
		ModifyTime:  modifyTime,
		ID:          id,
		URI:         uri,
		Headers:     headers,
		Code:        gisp,
		Body:        body,
		ETag:        etag,
		Count:       1,
		GzippedBody: gzippedBody,
		ContentType: contentType,
		dependents:  newDependentSet(),
		Quota:       quota,
		Cost:        0,
		Concurrent:  uint32(concurrent),
	}
}
