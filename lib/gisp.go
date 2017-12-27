package lib

import (
	"bytes"
	"encoding/json"

	"github.com/valyala/fasthttp"
	"github.com/ysmood/gisp"
)

type gispEnv struct {
	reqCtx         *fasthttp.RequestCtx
	log            []byte
	hasLog         bool
	file           *File
	appCtx         *AppContext
	fileStackDepth int
	query          *fasthttp.Args // hack: when file import and execute another file, it will be the arguments
	proxyHost      string
	proxyFile      string
	fnRunCount     *int
}

const maxFnRunCount = 1e5

func preRun(ctx *gisp.Context) {
	env := ctx.ENV.(*gispEnv)
	*env.fnRunCount++

	if *env.fnRunCount > maxFnRunCount {
		ctx.Error("max function run count exceeded")
	}
}

func (appCtx *AppContext) runGisp(
	file *File,
	reqCtx *fasthttp.RequestCtx,
	isLiftErr bool,
) (body []byte, env *gispEnv, err interface{}) {

	defer func() {
		err = recover()
		if err != nil {
			gispErr, ok := err.(gisp.Error)
			if ok {
				if isLiftErr {
					stack, _ := json.Marshal(gispErr.Stack)
					err = err.(gisp.Error).Message + "\nstack: " + string(stack)
				} else {
					err = err.(gisp.Error).Message
				}
			}
		}
	}()

	sandbox := newSandbox()
	fnRunCount := 0

	env = &gispEnv{
		hasLog:         false,
		reqCtx:         reqCtx,
		file:           file,
		appCtx:         appCtx,
		fileStackDepth: 0,
		query:          reqCtx.QueryArgs(),
		fnRunCount:     &fnRunCount,
	}

	ret := gisp.Run(&gisp.Context{
		AST:         file.Code,
		Sandbox:     sandbox,
		ENV:         env,
		IsLiftPanic: isLiftErr,
		PreRun:      preRun,
	})

	switch ret.(type) {
	case StringBytes:
		body = ret.(StringBytes)
	case []byte:
		body = ret.([]byte)
	case string:
		body = []byte(ret.(string))
	default:
		bin, err := json.Marshal(ret)
		if err == nil {
			body = bin
		}
	}

	if env.hasLog {
		body = bytes.Join(
			[][]byte{
				[]byte("gisp log:"),
				env.log,
				[]byte("gisp value:"),
				body,
			},
			[]byte("\n"),
		)
	}

	return
}
