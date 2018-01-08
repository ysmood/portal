package lib

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"regexp"

	"github.com/valyala/fasthttp"
	"github.com/ysmood/portal/lib/utils"
)

const (
	statusOK               = 200
	statusNotModified      = 304
	statusForbiden         = 403
	statusNotFound         = 404
	statusTooManyRequests  = 429
	statusScriptError      = 500
	statusPassThroughCache = 600

	gzipMinSize = 256
	gzip        = "gzip"
)

func (appCtx *AppContext) getFileFromCache(uri string) (file *File) {
	cache, exists := appCtx.cache.Get(uri)

	if exists {
		file = cache.(*File)
		atomic.AddUint64(&file.Count, 1)
	} else {
		file = nil
	}

	return
}

func (appCtx *AppContext) requestFile(uri string) *File {
	res, err := http.Get((&url.URL{
		Scheme:   "http",
		Host:     appCtx.fileServiceAddr,
		Path:     "/api/file",
		RawQuery: fmt.Sprintf("uri=%s", url.QueryEscape(uri)),
	}).String())

	if err != nil {
		fmt.Fprintln(os.Stderr, uri+" connect error:\n"+err.Error())
		appCtx.overloadMointer.action <- &overloadMessage{
			origin: overloadOriginFile,
			uri:    uri,
		}
		return &File{
			Body: []byte("file service error"),
		}
	}

	defer res.Body.Close()

	body, err := ioutil.ReadAll(res.Body)

	if err != nil {
		fmt.Fprintln(os.Stderr, uri+" read error:\n"+err.Error())
		appCtx.overloadMointer.action <- &overloadMessage{
			origin: overloadOriginFile,
			uri:    uri,
		}
		return &File{
			Body: []byte("read file service error"),
		}
	}

	if res.StatusCode != 200 {
		fmt.Fprintln(os.Stderr, uri+":"+strconv.Itoa(res.StatusCode)+"\n"+string(body))
		appCtx.overloadMointer.action <- &overloadMessage{
			origin: overloadOriginFile,
			uri:    uri,
		}
		return &File{
			Body: []byte("file service error"),
		}
	}

	header := map[string]string{}
	for k, v := range res.Header {
		header[k] = v[0]
	}

	return newFile(uri, header, body)
}

func (appCtx *AppContext) getFile(uri string) (file *File) {
	uri, _ = utils.GetURIPath(uri)
	file = appCtx.getFileFromCache(uri)
	if file != nil {
		return
	}

	atomic.AddInt32(&appCtx.workingCount, 1)
	defer atomic.AddInt32(&appCtx.workingCount, -1)

	if appCtx.workingCount > appCtx.overload {
		return overloadFile
	}

	appCtx.workingLock.Lock()
	defer appCtx.workingLock.Unlock()

	file = appCtx.getFileFromCache(uri)
	if file != nil {
		return
	}

	appCtx.reqCount.chStatusCode <- statusPassThroughCache

	file = appCtx.requestFile(uri)

	appCtx.cache.Set(uri, file)

	return
}

var cleanURIReg = regexp.MustCompile(`\bquery\.[^.=]+=[^\&]*`)

func (appCtx *AppContext) setHeaders(ctx *fasthttp.RequestCtx, file *File) {
	l := len(file.Headers) - 1
	ctx.Response.Header.SetContentType(file.ContentType)
	for i := 0; i < l; i += 2 {
		ctx.Response.Header.AddBytesKV(
			file.Headers[i],
			file.Headers[i+1],
		)
	}
}

func (appCtx *AppContext) handleProxy(uri string, ctx *fasthttp.RequestCtx) {
	if rule, has := appCtx.proxyMap.Get(uri); has {
		file := rule.(*File)

		startTime := time.Now().UnixNano()
		if appCtx.cost.many(file.URI, file.Quota, file.Concurrent) {
			appCtx.reqCount.chStatusCode <- statusTooManyRequests
			ctx.SetStatusCode(statusTooManyRequests)
			ctx.Write(overloadFile.Body)
			return
		}

		_, env, err := appCtx.runGisp(file, ctx, true)

		timer := uint64(time.Now().UnixNano() - startTime)

		atomic.AddUint64(&file.Cost, timer)

		appCtx.cost.chAdd <- &costMessage{
			uri:  file.URI,
			cost: timer,
		}

		if err != nil {
			appCtx.reqCount.chStatusCode <- statusScriptError
			msg := fmt.Sprint("nisp proxy error: ", err)
			appCtx.log.http(string(ctx.URI().FullURI()), statusScriptError, msg)
			ctx.Error(msg, statusScriptError)
			return
		}

		if env.proxyHost != "" {
			c := fasthttp.HostClient{
				Addr: env.proxyHost,
			}

			err := c.Do(&ctx.Request, &ctx.Response)

			if err != nil {
				ctx.Error(err.Error(), statusScriptError)
			}
		} else if env.proxyFile != "" {
			ctx.URI().Update(env.proxyFile)
			appCtx.handleFile(env.proxyFile, ctx)
		}

		return
	}

	appCtx.handleFile(uri, ctx)
}

func (appCtx *AppContext) handleFile(uri string, ctx *fasthttp.RequestCtx) {
	file := appCtx.getFile(uri)

	if file == nil {
		appCtx.reqCount.chStatusCode <- statusNotFound
		ctx.NotFound()
		return
	}

	if file.Type == fileTypeOverload {
		ctx.SetStatusCode(statusTooManyRequests)
		ctx.Write(file.Body)
		return
	}

	if file.Type == fileTypeNotFound {
		ctx.NotFound()
		return
	}

	// Set headers
	// it could be overwrite by gisp
	appCtx.setHeaders(ctx, file)

	var body []byte
	if file.Code == nil {
		// Check ETag
		if file.ETag != nil && bytes.Equal(
			ctx.Request.Header.Peek(ifNoneMatch), file.ETag,
		) {
			appCtx.reqCount.chStatusCode <- statusNotModified
			ctx.NotModified()
			return
		}

		ctx.Response.Header.SetBytesKV(
			[]byte(eTag),
			file.ETag,
		)

		if file.GzippedBody != nil && ctx.Request.Header.HasAcceptEncodingBytes([]byte(gzip)) {
			ctx.Response.Header.SetBytesKV([]byte("Content-Encoding"), []byte(gzip))
			body = file.GzippedBody
		} else {
			body = file.Body
		}
	} else {
		startTime := time.Now().UnixNano()
		if appCtx.cost.many(file.URI, file.Quota, file.Concurrent) {
			appCtx.reqCount.chStatusCode <- statusTooManyRequests
			ctx.SetStatusCode(statusTooManyRequests)
			ctx.Write(overloadFile.Body)
			return
		}

		var err interface{}
		body, _, err = appCtx.runGisp(file, ctx, false)

		timer := uint64(time.Now().UnixNano() - startTime)
		atomic.AddUint64(&file.Cost, timer)

		appCtx.cost.chAdd <- &costMessage{
			uri:  file.URI,
			cost: timer,
		}

		if err != nil {
			appCtx.reqCount.chStatusCode <- statusScriptError
			msg := fmt.Sprint("gisp error: ", err)
			appCtx.log.http(string(ctx.URI().FullURI()), statusScriptError, msg)
			ctx.Error(msg, statusScriptError)
			return
		}

		// Check ETag
		if body != nil {
			etag := utils.ETag(body)
			if bytes.Equal(ctx.Request.Header.Peek(ifNoneMatch), etag) {
				appCtx.reqCount.chStatusCode <- statusNotModified
				ctx.NotModified()
				return
			}

			ctx.Response.Header.SetBytesKV(
				[]byte(eTag),
				etag,
			)
		}
	}

	ctx.Write(body)

	appCtx.reqCount.chStatusCode <- ctx.Response.StatusCode()
}

// FileService ...
func (appCtx *AppContext) FileService() func() {
	listener, _ := net.Listen("tcp", appCtx.addr)

	server := &fasthttp.Server{
		ReadBufferSize: 1024 * 8,
		Handler: func(ctx *fasthttp.RequestCtx) {
			ctx.Response.Header.DisableNormalizing()

			uri := ctx.URI()
			uriStr := string(uri.Scheme()) + "://" + string(uri.Host()) + string(uri.Path())

			if len(appCtx.blacklist[0]) != 0 {
				for _, prefix := range appCtx.blacklist {
					if strings.HasPrefix(uriStr, prefix) {
						ctx.SetStatusCode(statusTooManyRequests)
						ctx.WriteString(`"Forbidden"`)
						return
					}
				}
			}

			appCtx.handleProxy(uriStr, ctx)
		},
	}

	go server.Serve(listener)

	fmt.Printf("file service listen on %s\n", appCtx.addr)

	return func() {
		listener.Close()
	}
}
