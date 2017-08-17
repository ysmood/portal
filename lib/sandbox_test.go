package lib

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/valyala/fasthttp"
	"github.com/ysmood/umi"
)

func TestGisp(t *testing.T) {
	appCtx := NewAppContext()

	code := []byte(`["if",
		[">", 1, 2],
		"red",
		"blue"
	]`)

	file := newFile("", map[string]string{
		"Portm-Type": "Gisp",
	}, code)
	reqCtx := &fasthttp.RequestCtx{}
	body, _, _ := appCtx.runGisp(file, reqCtx, false)

	assert.Equal(t, "blue", string(body))
}

func TestGispComplex(t *testing.T) {
	appCtx := &AppContext{
		cache: umi.New(nil),
		cost:  newCostCache(),
		glob: &globCache{
			lock:      &sync.Mutex{},
			descCache: umi.New(nil),
			ascCache:  umi.New(nil),
		},
	}

	code := []byte(`[
		"do",
		[
			"def",
			"version",
			[
				"query",
				"query.version"
			]
		],
		[
			"def",
			"from",
			[
				"query",
				"query.from"
			]
		],
		[
			"def",
			"fileList",
			[
				"concat",
				[
					"glob",
					[
						"+",
						"^",
						"http://portal-portm.meituan.com/horn/",
						[
							"version"
						],
						"/public"
					]
				],
				[
					"glob",
					[
						"+",
						"^",
						"http://portal-portm.meituan.com/horn/",
						[
							"version"
						],
						"/modules/",
						[
							"from"
						]
					]
				]
			]
		],
		[
			"def",
			"analytics",
			["get", ["fileList"], "0"]
		],
		[
			"for",
			"i",
			"path",
			[
				"fileList"
			],
			[
				"if",
				[
					">",
					[
						"file",
						[
							"path"
						],
						"modifyTime"
					],
					[
						"file",
						[
							"analytics"
						],
						"modifyTime"
					]
				],
				[
					"redef",
					"analytics",
					[
						"path"
					]
				]
			]
		],
		[
			":",
			"docId",
			[
				"file",
				[
					"analytics"
				],
				"id"
			],
			"rootId",
			[
				"file",
				[
					"analytics"
				],
				"rootId"
			],
			"cacheDuration",
			10,
			"pollDuration",
			20,
			"pollPeriod",
			[
				"|",
				"10:00",
				"13:00",
				"16:00",
				"21:00"
			],
			"overTime",
			false
		]
	]`)

	file := newFile("", map[string]string{
		"Portm-Type": "Gisp",
	}, code)

	appCtx.cache.Set(
		"http://portal-portm.meituan.com/horn/a",
		newFile("", map[string]string{}, []byte{}),
	)
	appCtx.cache.Set(
		"http://portal-portm.meituan.com/horn/b",
		newFile("", map[string]string{}, []byte{}),
	)
	appCtx.cache.Set(
		"http://portal-portm.meituan.com/horn/c",
		newFile("", map[string]string{}, []byte{}),
	)
	appCtx.cache.Set(
		"http://portal-portm.meituan.com/horn/d",
		newFile("", map[string]string{}, []byte{}),
	)
	appCtx.glob.Set(
		true,
		"^http://portal-portm.meituan.com/horn/v1/public",
		[]interface{}{
			"http://portal-portm.meituan.com/horn/a",
			"http://portal-portm.meituan.com/horn/b",
		},
	)
	appCtx.glob.Set(
		true,
		"^http://portal-portm.meituan.com/horn/v1/modules/all",
		[]interface{}{
			"http://portal-portm.meituan.com/horn/c",
			"http://portal-portm.meituan.com/horn/d",
		},
	)

	reqCtx := &fasthttp.RequestCtx{}
	reqCtx.URI().SetQueryString("query.version=v1&query.from=all")
	body, _, err := appCtx.runGisp(file, reqCtx, true)

	if err != nil {
		panic(err)
	}

	assert.Equal(t, `{"cacheDuration":10,"docId":"","overTime":false,"pollDuration":20,"pollPeriod":["10:00","13:00","16:00","21:00"],"rootId":""}`, string(body))
}
