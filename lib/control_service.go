package lib

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"runtime"
	"strconv"

	"time"

	"github.com/a8m/djson"
	"github.com/valyala/fasthttp"
	"github.com/ysmood/portal/lib/utils"
)

func (appCtx *AppContext) updateProxyCache(uri string) {
	// update the proxy
	cache := appCtx.requestFile(uri)

	if _, has := appCtx.proxyMap.Get(uri); has {
		delete(appCtx.proxyMap, uri)
		fmt.Println("delete proxy rule:", cache.URI)
	}

	if cache.Type == fileTypeProxy {
		appCtx.proxyMap[uri] = cache
		fmt.Println("update proxy rule:", cache.URI)
	}
}

func (appCtx *AppContext) clearDependents(uri string) {
	value, has := appCtx.cache.Get(uri)

	if !has {
		return
	}

	file := value.(*File)

	for _, v := range appCtx.cache.Values() {
		f := v.(*File)

		f.dependents.Del(file)
	}
}

// curl 127.0.0.1:7070/?action=update&uri=test.com
func (appCtx *AppContext) updateFile(ctx *fasthttp.RequestCtx) {
	action := string(ctx.QueryArgs().Peek("action"))
	uri := string(ctx.QueryArgs().Peek("uri"))

	appCtx.updateProxyCache(uri)

	switch action {
	case "create":
		appCtx.glob.UpdateToList(uri)
		file := appCtx.requestFile(uri)
		appCtx.cache.Set(uri, file)

	case "update":
		appCtx.glob.UpdateToList(uri)
		file := appCtx.requestFile(uri)
		appCtx.clearDependents(uri)
		appCtx.cache.Set(uri, file)

	case "delete":
		appCtx.glob.DelFromList(uri)
		appCtx.clearDependents(uri)
		appCtx.cache.Del(uri)

	default:
		ctx.Error("bad action", 400)
	}
}

func (appCtx *AppContext) status(ctx *fasthttp.RequestCtx) {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	data, err := json.Marshal(map[string]interface{}{
		"cache":        appCtx.cache.Size(),
		"count":        appCtx.reqCount.getStatusCodes(),
		"total":        appCtx.reqCount.statusCodeLastTotal,
		"qps":          uint32(appCtx.reqCount.statusCodeQPS),
		"time":         time.Now().UnixNano() / 1000 / 1000,
		"qpsTime":      appCtx.reqCount.statusCodeLastTime.UnixNano() / 1000 / 1000,
		"workingCount": appCtx.workingCount,
		"mem":          m.Sys / 1024,
	})
	if err != nil {
		data = []byte(err.Error())
	}

	ctx.SetContentType("application/json; charset=utf-8")
	ctx.Write(data)
}

func (appCtx *AppContext) testQuery(ctx *fasthttp.RequestCtx) {
	data, err := djson.Decode(ctx.PostBody())
	if err != nil {
		ctx.Error(err.Error(), 400)
		return
	}

	file := &File{Code: data, Quota: maxQuota}

	body, _, gispErr := appCtx.runGisp(file, ctx, true)

	if gispErr != nil {
		body = []byte(fmt.Sprint("gisp error: ", gispErr))
		ctx.SetStatusCode(500)
	}
	ctx.Write(body)
}

func (appCtx *AppContext) logList(ctx *fasthttp.RequestCtx) {
	offset, _ := ctx.QueryArgs().GetUint("offset")
	limit, _ := ctx.QueryArgs().GetUint("limit")
	count := appCtx.log.cache.Count()
	right := 0
	offset, right = utils.Slicer(offset, limit, count, 200)

	list := []interface{}{}

	items := appCtx.log.cache.Slice(offset, right)
	for _, item := range items {
		list = append(list, item.Value())
	}

	data, err := json.Marshal(map[string]interface{}{
		"total": count,
		"list":  list,
	})

	if err != nil {
		ctx.Error(err.Error(), 500)
		return
	}

	ctx.SetContentType("application/json; charset=utf-8")
	ctx.Write(data)
}

func (appCtx *AppContext) cacheList(ctx *fasthttp.RequestCtx) {
	offset, _ := ctx.QueryArgs().GetUint("offset")
	limit, _ := ctx.QueryArgs().GetUint("limit")
	count := appCtx.cache.Count()
	right := 0
	offset, right = utils.Slicer(offset, limit, count, 200)

	list := []File{}
	costList := make(map[string]string)

	items := appCtx.cache.Slice(offset, right)
	for _, item := range items {
		f := *item.Value().(*File)
		list = append(list, f)
		costList[f.URI] = strconv.FormatUint(appCtx.cost.get(f.URI), 10)
	}

	data, err := json.Marshal(map[string]interface{}{
		"total": count,
		"list":  list,
		"cost":  costList,
	})

	if err != nil {
		ctx.Error(err.Error(), 500)
		return
	}

	ctx.SetContentType("application/json; charset=utf-8")
	ctx.Write(data)
}

func (appCtx *AppContext) getDeps(deps map[*File]bool, file *File) {
	if file.dependents == nil {
		return
	}

	for _, f := range file.dependents.List() {
		if _, has := deps[f]; has {
			continue
		}

		deps[f] = true
		appCtx.getDeps(deps, f)
	}
}

func (appCtx *AppContext) queryDeps(ctx *fasthttp.RequestCtx) {
	uri := string(ctx.QueryArgs().Peek("uri"))
	deps := map[*File]bool{}

	value, _ := appCtx.cache.Get(uri)

	if value != nil {
		file := value.(*File)
		appCtx.getDeps(deps, file)
		deps[file] = true
	}

	list := []string{}
	for dep := range deps {
		list = append(list, dep.URI)
	}

	data, err := json.Marshal(list)

	if err != nil {
		ctx.Error(err.Error(), 500)
		return
	}

	ctx.SetContentType("application/json; charset=utf-8")
	ctx.Write(data)
}

func (appCtx *AppContext) boundaryQuotaList(ctx *fasthttp.RequestCtx) {
	boundary, _ := ctx.QueryArgs().GetUfloat("boundary")

	items := appCtx.cache.Items()

	type listItem struct {
		URI   string
		Cost  uint64
		Quota uint64
	}

	list := []listItem{}

	for _, item := range items {
		file := *item.Value().(*File)
		cost := appCtx.cost.get(file.URI)
		quota := file.Quota

		a := float64(cost / 1e9)
		b := float64(quota / 1e9)

		if quota > 0 && a/b < boundary {
			continue
		}

		list = append(list, listItem{
			URI:   file.URI,
			Cost:  cost,
			Quota: quota,
		})
	}

	data, _ := json.Marshal(list)

	ctx.SetContentType("application/json; charset=utf-8")
	ctx.Write(data)
}

func (appCtx *AppContext) getProxyMap() {
	var list []string
	err := appCtx.rpc(&list, `
		[
			"map",
			[
				"run",
				[
					"limit",
					[
						"filter",
						[
							"listFile"
						],
						[
							":",
							"type",
							"Proxy"
						]
					],
					0,
					1000
				]
			],
			[
				"fn",
				[
					"el"
				],
				[
					"get",
					[
						"el"
					],
					"uri"
				]
			]
		]
	`)

	if err != nil {
		os.Stderr.WriteString(err.Error())
		return
	}

	for _, uri := range list {
		appCtx.proxyMap[uri] = appCtx.requestFile(uri)
	}

	fmt.Println("proxy rules got:", list)
}

func (appCtx *AppContext) purge(ctx *fasthttp.RequestCtx) {
	appCtx.cache.Purge()

	appCtx.getProxyMap()

	fmt.Println("purged")
}

// ControlService ...
func (appCtx *AppContext) ControlService() func() {
	listener, err := net.Listen("tcp", appCtx.ctrlServiceAddr)

	if err != nil {
		panic(err)
	}

	server := &fasthttp.Server{
		ReadBufferSize: 1024 * 8,
		Handler: func(ctx *fasthttp.RequestCtx) {
			switch string(ctx.Path()) {
			case "/file":
				appCtx.updateFile(ctx)

			case "/purge":
				appCtx.purge(ctx)

			case "/purge-req-count":
				appCtx.reqCount.chStatusCode <- reqStatusCodeActionClear
				fmt.Println("purged")

			case "/status":
				appCtx.status(ctx)

			case "/test-query":
				appCtx.testQuery(ctx)

			case "/cache-list":
				appCtx.cacheList(ctx)

			case "/log-list":
				appCtx.logList(ctx)

			case "/query-deps":
				appCtx.queryDeps(ctx)

			case "/boundary-quota-list":
				appCtx.boundaryQuotaList(ctx)

			default:
				ctx.NotFound()
			}
		},
	}

	appCtx.getProxyMap()

	go server.Serve(listener)

	fmt.Printf("control service listen on %s\n", appCtx.ctrlServiceAddr)

	return func() {
		listener.Close()
	}
}
