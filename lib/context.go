package lib

import (
	"encoding/json"
	"flag"
	"io/ioutil"
	"net/http"
	"net/url"
	"os/user"
	"path"
	"strings"
	"sync"

	"time"

	"github.com/ysmood/portal/lib/utils"
	"github.com/ysmood/umi"
)

// AppContext ...
type AppContext struct {
	cache           *umi.Cache
	glob            *globCache
	log             *logCache
	cost            *costCache
	addr            string
	ctrlServiceAddr string
	fileServiceAddr string
	dbPath          string
	overload        int32
	proxyMap        utils.PrefixMap
	reqCount        *reqCount
	queryPrefix     []byte
	globLock        *sync.Mutex
	workingLock     *sync.Mutex
	workingCount    int32
	blacklist       []string
}

// NewAppContext ...
func NewAppContext() *AppContext {
	var addr, ctrlServiceAddr, fileServiceAddr, dbPath, blacklist string
	var cacheSize, globCacheSize, overload int

	usr, err := user.Current()
	if err != nil {
		panic(err)
	}

	flag.StringVar(&addr, "addr", utils.LookupStrEnv("portalAddr", ":7070"), "file service address")
	flag.StringVar(&ctrlServiceAddr, "fileAddr", utils.LookupStrEnv("portalFileAddr", "127.0.0.1:7071"), "backend file service address")
	flag.StringVar(&fileServiceAddr, "ctrlAddr", utils.LookupStrEnv("portalCtrlAddr", "127.0.0.1:7000"), "control file service address")
	flag.IntVar(&cacheSize, "cacheSize", utils.LookupIntEnv("portalCacheSize", 2*1024*1024*1024), "cache size, default 2GB")
	flag.IntVar(&globCacheSize, "globCacheSize", utils.LookupIntEnv("portalGlobCacheSize", 3*1024*1024), "cache size, default 300MB")
	flag.StringVar(&dbPath, "dbPath", utils.LookupStrEnv("portalDbPath", path.Join(usr.HomeDir, ".portm-portal.db")), "path of the database file")
	flag.IntVar(&overload, "overload", utils.LookupIntEnv("portalOverload", 300), "cache overload number")
	flag.StringVar(&blacklist, "blackList", utils.LookupStrEnv("portalBlacklist", ""), "uri prefix black list")

	flag.Parse()

	initDb(dbPath)

	rc := newReqCount()

	go rc.worker()

	return &AppContext{
		cache: umi.New(&umi.Options{
			MaxMemSize:  uint64(cacheSize),
			PromoteRate: -1,
			TTL:         10 * time.Minute,
		}),
		glob: &globCache{
			lock: &sync.Mutex{},
			descCache: umi.New(&umi.Options{
				MaxMemSize:  uint64(globCacheSize),
				PromoteRate: -1,
				TTL:         10 * time.Minute,
			}),
			ascCache: umi.New(&umi.Options{
				MaxMemSize:  uint64(globCacheSize),
				PromoteRate: -1,
				TTL:         10 * time.Minute,
			}),
		},
		log: &logCache{
			cache: umi.New(&umi.Options{
				MaxMemSize:  10 * 1024 * 1024, // 10MB
				PromoteRate: -1,
				GCSize:      -1,
			}),
		},
		cost:            newCostCache(),
		addr:            addr,
		ctrlServiceAddr: ctrlServiceAddr,
		fileServiceAddr: fileServiceAddr,
		dbPath:          dbPath,
		overload:        int32(overload),
		proxyMap:        utils.PrefixMap{},
		reqCount:        rc,
		queryPrefix:     []byte("query."),
		workingLock:     &sync.Mutex{},
		workingCount:    0,
		blacklist:       strings.Split(blacklist, ","),
	}
}

func (appCtx *AppContext) rpc(result interface{}, nisp string) error {
	res, err := http.Post((&url.URL{
		Scheme: "http",
		Host:   appCtx.fileServiceAddr,
		Path:   "/api/nisp",
	}).String(), "", strings.NewReader(nisp))

	if err != nil {
		return err
	}

	defer res.Body.Close()

	body, err := ioutil.ReadAll(res.Body)

	json.Unmarshal(body, result)

	return nil
}
