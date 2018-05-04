package lib

import (
	"hash/crc32"
	"io"
	"math/rand"
	"net/http"
	"os"
	"sort"
	"strconv"
	"sync"
	"sync/atomic"

	"time"

	"encoding/json"

	"strings"

	"fmt"

	"github.com/a8m/djson"
	uuid "github.com/satori/go.uuid"
	"github.com/valyala/fasthttp"
	"github.com/ysmood/gisp"
	gispLib "github.com/ysmood/gisp/lib"
	"github.com/ysmood/portal/lib/utils"
)

const maxHashValue = float64(0xFFFFFFFF)

var hostname, _ = os.Hostname()

var randNum = rand.New(
	rand.NewSource(
		time.Now().UnixNano() - int64(crc32.ChecksumIEEE([]byte(hostname))),
	),
)

var gispLock = &sync.Mutex{}

const maxRequestBody = 1024 * 1024 // 1MB

var httpClient = &http.Client{
	Timeout: 3 * time.Second,
}

func newSandbox() *gisp.Sandbox {
	return gisp.New(gisp.Box{

		"help": func(ctx *gisp.Context) interface{} {
			list := ctx.Sandbox.Names()
			sort.Strings(ctx.Sandbox.Names())
			return list
		},

		"log": func(ctx *gisp.Context) interface{} {
			env := ctx.ENV.(*gispEnv)
			arg := ctx.Arg(1)
			env.hasLog = true
			s, err := json.MarshalIndent(arg, "", "    ")

			if err == nil {
				env.log = append(env.log, s...)
			} else {
				env.log = append(env.log, []byte(err.Error())...)
			}

			env.log = append(env.log, '\n')

			return arg
		},

		"fileExists": func(ctx *gisp.Context) interface{} {
			uri := ctx.ArgStr(1)
			env := ctx.ENV.(*gispEnv)
			file := env.appCtx.getFile(uri)
			return file.Type != fileTypeNotFound
		},

		"file": func(ctx *gisp.Context) interface{} {
			uri := ctx.ArgStr(1)
			mode := ctx.Arg(2)

			if mode == nil {
				mode = "binary"
			}

			env := ctx.ENV.(*gispEnv)

			// Whether to use test file to replace the real one
			file := env.appCtx.getFile(uri)

			if file.Type == fileTypeNotFound {
				return nil
			}

			if file.dependents != nil {
				file.dependents.Add(env.file)
			}

			switch mode.(string) {
			case "json":
				if file.JSONBody == nil {
					var err error
					file.JSONBody, err = djson.Decode(file.Body)
					if err != nil {
						return nil
					}
				}
				// to prevent race write condition, clone the jsonBody
				return clone(file.JSONBody)

			case "code":
				if env.fileStackDepth > 7 {
					ctx.Error(fmt.Sprintln(
						`file execution stack exceeded the limit:`,
						file.URI,
					))
				}

				newEnv := *env
				_, query := utils.GetURIPath(uri)
				args := &fasthttp.Args{}
				args.Parse(query)
				newEnv.query = args
				newEnv.file = file
				newEnv.fileStackDepth = env.fileStackDepth + 1

				startTime := time.Now().UnixNano()

				ret := gisp.Run(&gisp.Context{
					AST:         file.Code,
					Sandbox:     ctx.Sandbox.Create(),
					ENV:         &newEnv,
					IsLiftPanic: ctx.IsLiftPanic,
					PreRun:      preRun,
				})

				atomic.AddUint64(&file.Cost, uint64(time.Now().UnixNano()-startTime))

				return ret

			case "type":
				return file.Type.String()
			case "id":
				return file.ID
			case "modifierId":
				return file.ModifierID
			case "rootId":
				return file.RootID
			case "modifyTime":
				return file.ModifyTime
			default:
				return file.Body
			}
		},

		"glob": func(ctx *gisp.Context) interface{} {
			env := ctx.ENV.(*gispEnv)
			pattern := ctx.ArgStr(1)
			isDesc := true
			if ctx.Len() > 2 {
				if ctx.ArgStr(2) == "asc" {
					isDesc = false
				}
			}

			list, has := env.appCtx.glob.Get(isDesc, pattern)

			if has {
				return clone(list)
			}

			atomic.AddInt32(&env.appCtx.glob.count, 1)
			defer atomic.AddInt32(&env.appCtx.glob.count, -1)

			if env.appCtx.glob.count > env.appCtx.glob.overload {
				return []interface{}{}
			}

			env.appCtx.glob.lock.Lock()
			defer env.appCtx.glob.lock.Unlock()

			list, has = env.appCtx.glob.Get(isDesc, pattern)

			if has {
				return clone(list)
			}

			order := "asc"

			if isDesc {
				order = "desc"
			}

			err := env.appCtx.rpc(&list, `["globFile", "`+pattern+`", "`+order+`"]`)

			if err != nil {
				fmt.Fprintln(os.Stderr, pattern+" glob connect error:\n"+err.Error())
				list = []interface{}{}
				env.appCtx.overloadMointer.action <- &overloadMessage{
					origin: overloadOriginGisp,
					uri:    pattern,
					desc:   isDesc,
				}
			} else if _, ok := list.([]interface{}); !ok {
				fmt.Fprintln(os.Stderr, pattern+" glob parse error")
				list = []interface{}{}
				env.appCtx.overloadMointer.action <- &overloadMessage{
					origin: overloadOriginGisp,
					uri:    pattern,
					desc:   isDesc,
				}
			}

			env.appCtx.glob.Set(isDesc, pattern, list)

			return clone(list)
		},

		"cache": func(ctx *gisp.Context) interface{} {
			env := ctx.ENV.(*gispEnv)
			key := ctx.ArgStr(1)

			value, has := env.appCtx.runtimeCache.get(env.file.URI, key)

			if has {
				return clone(value)
			}

			env.appCtx.runtimeCache.lock.Lock()
			defer env.appCtx.runtimeCache.lock.Unlock()

			value, has = env.appCtx.runtimeCache.get(env.file.URI, key)

			if has {
				return clone(value)
			}

			deps := ctx.ArgArr(2)

			newDeps := make([]string, len(deps))

			for i, dep := range deps {
				newDeps[i] = dep.(string)
			}

			value = ctx.Arg(3)

			env.appCtx.runtimeCache.set(env.file.URI, key, value, newDeps)

			return clone(value)
		},

		"request": func(ctx *gisp.Context) interface{} {
			method := ctx.ArgStr(1)
			url := ctx.ArgStr(2)
			headers := ctx.Arg(3)
			body := ctx.Arg(4)

			var bodyStr string
			if body != nil {
				bodyStr = body.(string)
			}

			req, err := http.NewRequest(method, url, strings.NewReader(bodyStr))

			if err != nil {
				ctx.Error(err.Error())
			}

			if headers != nil {
				for k, v := range headers.(map[string]interface{}) {
					req.Header.Set(k, v.(string))
				}
			}

			res, err := httpClient.Do(req)

			if err != nil {
				ctx.Error(err.Error())
			}

			defer res.Body.Close()

			resBody := []byte{}
			buf := make([]byte, 64)
			count := 0
			for {
				n, err := res.Body.Read(buf)
				count += n
				if count > maxRequestBody {
					ctx.Error(fmt.Sprintf("max request body %v byte exceeded", maxRequestBody))
				}
				if err == io.EOF {
					resBody = append(resBody, buf[0:n]...)
					break
				}
				if err != nil {
					ctx.Error(err.Error())
				}
				resBody = append(resBody, buf[0:n]...)
			}

			return resBody
		},

		"parse": func(ctx *gisp.Context) interface{} {
			data := ctx.Arg(1)

			var obj interface{}
			var err error

			switch data.(type) {
			case string:
				obj, err = djson.Decode([]byte(data.(string)))
			case StringBytes:
				obj, err = djson.Decode(data.(StringBytes))
			case []byte:
				obj, err = djson.Decode(data.([]byte))
			}

			if err != nil {
				ctx.Error(err.Error())
			}

			return obj
		},

		"jsonp": func(ctx *gisp.Context) interface{} {
			name := ctx.ArgStr(1)
			json, err := json.Marshal(ctx.Arg(2))

			if err != nil {
				ctx.Error(err.Error())
			}

			return name + "(" + string(json) + ")"
		},

		"stringify": func(ctx *gisp.Context) interface{} {
			json, err := json.Marshal(ctx.Arg(1))

			if err != nil {
				ctx.Error(err.Error())
			}

			return string(json)
		},

		"setResHeader": func(ctx *gisp.Context) interface{} {
			env := ctx.ENV.(*gispEnv)

			env.reqCtx.Response.Header.Set(
				str(ctx.Arg(1)),
				str(ctx.Arg(2)),
			)
			return nil
		},

		"setStatusCode": func(ctx *gisp.Context) interface{} {
			env := ctx.ENV.(*gispEnv)

			env.reqCtx.SetStatusCode(
				int(ctx.ArgNum(1)),
			)
			return nil
		},

		"uuid": func(ctx *gisp.Context) interface{} {
			return uuid.NewV4().String()
		},

		"hash": func(ctx *gisp.Context) interface{} {
			val := ctx.ArgStr(1)

			return float64(crc32.ChecksumIEEE([]byte(val))) / maxHashValue
		},

		"redirect": func(ctx *gisp.Context) interface{} {
			env := ctx.ENV.(*gispEnv)
			env.reqCtx.Response.Header.Set(
				"Location",
				str(ctx.Arg(1)),
			)
			env.reqCtx.SetStatusCode(
				int(ctx.ArgNum(2)),
			)
			return nil
		},

		"setReqHost": func(ctx *gisp.Context) interface{} {
			env := ctx.ENV.(*gispEnv)
			env.reqCtx.Request.SetHost(ctx.ArgStr(1))
			return nil
		},

		"setReqPath": func(ctx *gisp.Context) interface{} {
			env := ctx.ENV.(*gispEnv)
			env.reqCtx.Request.SetRequestURI(ctx.ArgStr(1))
			return nil
		},

		"setReqHeader": func(ctx *gisp.Context) interface{} {
			env := ctx.ENV.(*gispEnv)
			env.reqCtx.Request.Header.Set(ctx.ArgStr(1), ctx.ArgStr(2))
			return nil
		},

		"proxyToHost": func(ctx *gisp.Context) interface{} {
			env := ctx.ENV.(*gispEnv)
			env.proxyHost = ctx.ArgStr(1)

			forceHost := true
			if ctx.Len() > 2 {
				forceHost = ctx.ArgBool(2)
			}

			if forceHost {
				env.reqCtx.Request.SetHost(env.proxyHost)
			}
			return nil
		},

		"proxyToFile": func(ctx *gisp.Context) interface{} {
			env := ctx.ENV.(*gispEnv)
			env.proxyFile = ctx.ArgStr(1)
			return nil
		},

		"rand": func(ctx *gisp.Context) interface{} {
			return randNum.Float64()
		},

		"now": func(ctx *gisp.Context) interface{} {
			return time.Now().String()
		},

		"rawQuery": func(ctx *gisp.Context) interface{} {
			return ctx.ENV.(*gispEnv).query.String()
		},

		"query": func(ctx *gisp.Context) interface{} {
			val := ctx.ENV.(*gispEnv).query.Peek(ctx.ArgStr(1))

			mode := ctx.Arg(3)
			if mode == nil {
				mode = "string"
			}

			switch mode.(string) {
			case "float":
				if val == nil && ctx.Len() > 2 {
					return ctx.ArgNum(2)
				}
				num, _ := strconv.ParseFloat(string(val), 64)
				return num
			case "boolean":
				if val == nil && ctx.Len() > 2 {
					return ctx.ArgBool(2)
				}
				boolean, _ := strconv.ParseBool(string(val))
				return boolean
			default:
				if val == nil && ctx.Len() > 2 {
					return ctx.ArgStr(2)
				}
				return string(val)
			}
		},

		"queries": func(ctx *gisp.Context) interface{} {
			data := ctx.ENV.(*gispEnv).query.PeekMulti(
				ctx.ArgStr(1),
			)

			mode := ctx.Arg(2)
			if mode == nil {
				mode = "string"
			}

			list := make([]interface{}, len(data))

			for i, item := range data {
				switch mode.(string) {
				case "float":
					num, _ := strconv.ParseFloat(string(item), 64)
					list[i] = num
				case "boolean":
					boolean, _ := strconv.ParseBool(string(item))
					list[i] = boolean
				default:
					list[i] = string(item)
				}
			}

			return list
		},

		"rawBody": func(ctx *gisp.Context) interface{} {
			return string(ctx.ENV.(*gispEnv).reqCtx.PostBody())
		},

		"body": func(ctx *gisp.Context) interface{} {
			val := ctx.ENV.(*gispEnv).reqCtx.PostArgs().Peek(ctx.ArgStr(1))

			mode := ctx.Arg(3)
			if mode == nil {
				mode = "string"
			}

			switch mode.(string) {
			case "float":
				if val == nil && ctx.Len() > 2 {
					return ctx.ArgNum(2)
				}
				num, _ := strconv.ParseFloat(string(val), 64)
				return num
			case "boolean":
				if val == nil && ctx.Len() > 2 {
					return ctx.ArgBool(2)
				}
				boolean, _ := strconv.ParseBool(string(val))
				return boolean
			default:
				if val == nil && ctx.Len() > 2 {
					return ctx.ArgStr(2)
				}
				return string(val)
			}
		},

		"bodies": func(ctx *gisp.Context) interface{} {
			data := ctx.ENV.(*gispEnv).reqCtx.PostArgs().PeekMulti(
				ctx.ArgStr(1),
			)

			mode := ctx.Arg(2)
			if mode == nil {
				mode = "string"
			}

			list := make([]interface{}, len(data))

			for i, item := range data {
				switch mode.(string) {
				case "float":
					num, _ := strconv.ParseFloat(string(item), 64)
					list[i] = num
				case "boolean":
					boolean, _ := strconv.ParseBool(string(item))
					list[i] = boolean
				default:
					list[i] = string(item)
				}
			}

			return list
		},

		"header": func(ctx *gisp.Context) interface{} {
			val := ctx.ENV.(*gispEnv).reqCtx.Request.Header.Peek(ctx.ArgStr(1))
			if val == nil && ctx.Len() > 2 {
				return ctx.ArgStr(2)
			}
			return string(val)
		},

		"method": func(ctx *gisp.Context) interface{} {
			val := ctx.ENV.(*gispEnv).reqCtx.Method()
			return string(val)
		},

		"path": func(ctx *gisp.Context) interface{} {
			return string(ctx.ENV.(*gispEnv).reqCtx.Path())
		},

		"host": func(ctx *gisp.Context) interface{} {
			return string(ctx.ENV.(*gispEnv).reqCtx.Host())
		},

		"href": func(ctx *gisp.Context) interface{} {
			return "http://" + string(ctx.ENV.(*gispEnv).reqCtx.Host()) + string(ctx.ENV.(*gispEnv).reqCtx.Path())
		},

		"startsWith": func(ctx *gisp.Context) interface{} {
			return strings.HasPrefix(ctx.ArgStr(1), ctx.ArgStr(2))
		},

		"replace": func(ctx *gisp.Context) interface{} {
			n := 1
			if ctx.Len() == 5 {
				n = int(ctx.ArgNum(4))
			}

			return strings.Replace(
				ctx.ArgStr(1),
				ctx.ArgStr(2),
				ctx.ArgStr(3),
				n,
			)
		},

		"compareVersion": func(ctx *gisp.Context) interface{} {
			return float64(
				utils.CompareVersion(ctx.ArgStr(1), ctx.ArgStr(2)),
			)
		},

		"float": func(ctx *gisp.Context) interface{} {
			num, _ := strconv.ParseFloat(ctx.ArgStr(1), 64)
			return num
		},

		"boolean": func(ctx *gisp.Context) interface{} {
			boolean, _ := strconv.ParseBool(ctx.ArgStr(1))
			return boolean
		},

		"recover": func(ctx *gisp.Context) (val interface{}) {
			defer func() {
				exp := recover()
				if exp != nil {
					if ctx.Len() > 2 {
						val = ctx.Arg(2)
					} else {
						val = exp
					}
				}
			}()
			val = ctx.Arg(1)
			return
		},

		"str": func(ctx *gisp.Context) interface{} {
			val := ctx.Arg(1)
			switch val.(type) {
			case string:
				return val.(string)
			case float64:
				return strconv.FormatFloat(val.(float64), 'f', -1, 64)
			case StringBytes:
				return string(val.(StringBytes))
			case []byte:
				return string(val.([]byte))
			default:
				return fmt.Sprint(val)
			}
		},

		"$":        gispLib.Raw,
		"throw":    gispLib.Throw,
		"get":      gispLib.Get,
		"set":      gispLib.Set,
		"len":      gispLib.Len,
		"includes": gispLib.Includes,
		"|":        gispLib.Arr,
		":":        gispLib.Dict,
		"do":       gispLib.Do,
		"def":      gispLib.Def,
		"redef":    gispLib.Redef,
		"if":       gispLib.If,
		"+":        gispLib.Add,
		"-":        gispLib.Minus,
		"*":        gispLib.Multiply,
		"**":       gispLib.Power,
		"/":        gispLib.Divide,
		"%":        gispLib.Mod,
		"=":        gispLib.Eq,
		"==":       gispLib.Eq,
		"!=":       gispLib.Ne,
		"<":        gispLib.Lt,
		"<=":       gispLib.Le,
		">":        gispLib.Gt,
		">=":       gispLib.Ge,
		"!":        gispLib.Not,
		"&&":       gispLib.And,
		"||":       gispLib.Or,
		"switch":   gispLib.Switch,
		"for":      gispLib.For,
		"fn":       gispLib.Fn,
		"concat":   gispLib.Concat,
		"append":   gispLib.Append,
		"split":    gispLib.Split,
		"slice":    gispLib.Slice,
		"indexOf":  gispLib.IndexOf,
	})
}
