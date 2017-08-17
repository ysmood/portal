package lib

import (
	"hash/crc32"
	"io"
	"math/rand"
	"net/http"
	"os"
	"sort"
	"sync"

	"time"

	"encoding/json"

	"strings"

	"fmt"

	"github.com/a8m/djson"
	"github.com/ysmood/portal/lib/utils"
	uuid "github.com/satori/go.uuid"
	"github.com/valyala/fasthttp"
	"github.com/ysmood/gisp"
	gispLib "github.com/ysmood/gisp/lib"
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

			file.dependents.Add(env.file)

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

				return gisp.Run(&gisp.Context{
					AST:         file.Code,
					Sandbox:     ctx.Sandbox.Create(),
					ENV:         &newEnv,
					IsLiftPanic: ctx.IsLiftPanic,
					PreRun:      preRun,
				})

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

			err := env.appCtx.rpc(&list, `
				["map",
					["globFile", "`+pattern+`", "`+order+`"],
					["iteratee", "uri"]
				]
			`)

			if err != nil {
				ctx.Error(err.Error())
			}

			env.appCtx.glob.Set(isDesc, pattern, list)

			return clone(list)
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
			return ctx.ENV.(*gispEnv).reqCtx.QueryArgs().String()
		},

		"query": func(ctx *gisp.Context) interface{} {
			val := ctx.ENV.(*gispEnv).query.Peek(ctx.ArgStr(1))
			if val == nil && ctx.Len() > 2 {
				return ctx.ArgStr(2)
			}
			return string(val)
		},

		"queries": func(ctx *gisp.Context) interface{} {
			data := ctx.ENV.(*gispEnv).query.PeekMulti(
				ctx.ArgStr(1),
			)

			list := make([]interface{}, len(data))

			for i, item := range data {
				list[i] = string(item)
			}

			return list
		},

		"path": func(ctx *gisp.Context) interface{} {
			return string(ctx.ENV.(*gispEnv).reqCtx.Path())
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

		"$":        gispLib.Raw,
		"throw":    gispLib.Throw,
		"get":      gispLib.Get,
		"set":      gispLib.Set,
		"str":      gispLib.Str,
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