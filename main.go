package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/emersion/go-imap"
	"github.com/emersion/go-imap/client"
	"gopkg.in/yaml.v3"
)

const (
	appName = "imapstats"

	configName = "config.yaml"

	defaultDirPerms = 0700

	ttlInfinite time.Duration = -1

	imapTimeout = 10 * time.Second
)

var (
	appHomeDir string
	cacheDir   string

	// CLI args
	addrArg       = flag.String("addr", "imap.gmail.com:993", "IMAP user")
	userArg       = flag.String("user", "", "IMAP user")
	passwordArg   = flag.String("pass", "", "IMAP password")
	mboxArg       = flag.String("mailbox", "INBOX", "mailbox on the server")
	quietArg      = flag.Bool("q", false, "If set, does not output stats on stdin. Can be used in background jobs to update cache")
	writeCacheArg = flag.Bool("write-cache", false, "if true writes to cache")
	readCacheArg  = flag.Bool("read-cache", false, "if true reads from cache")
	ttlArg        = flag.String("ttl", "",
		"sets cache ttl. By default no ttl is set. Default unit is seconds, hours and minues are also supported e.g. 2h; 35m")
)

type stats map[string]int

type criteriaCfg struct {
	Seen    bool              `yaml:"seen"`
	Body    []string          `yaml:"body"`
	Headers map[string]string `yaml:"headers"`

	Or []criteriaCfg `yaml:"or"`
}

func (cr *criteriaCfg) toIMAP() *imap.SearchCriteria {
	res := imap.NewSearchCriteria()
	if !cr.Seen {
		res.WithoutFlags = []string{imap.SeenFlag}
	}
	res.Body = cr.Body
	for k, v := range cr.Headers {
		res.Header.Add(k, v)
	}
	mkORclause(res, cr.Or)

	return res
}

func mkORclause(sc *imap.SearchCriteria, or []criteriaCfg) {
	if len(or) == 0 {
		return
	}
	if len(or) == 1 {
		panic("OR criteria can't have 1 criterion")
	}
	if len(or) == 2 {
		sc.Or = append(sc.Or, [2]*imap.SearchCriteria{})
		sc.Or[0][0] = or[0].toIMAP()
		sc.Or[0][1] = or[1].toIMAP()
		return
	}
	sc.Or = append(sc.Or, [2]*imap.SearchCriteria{})
	sc.Or[0][0] = or[0].toIMAP()
	sc.Or[0][1] = imap.NewSearchCriteria()

	mkORclause(sc.Or[0][1], or[1:])
}

type statsConfig map[string]*criteriaCfg

type config struct {
	Accounts map[string]map[string]statsConfig `yaml:"accounts"`
}

func (c *config) getStatsCfg(user string, mailBox string) statsConfig {
	// unseen count added by default
	defaultCfg := statsConfig{"unseen_count": &criteriaCfg{}}

	mboxes := c.Accounts[user]
	if mboxes == nil {
		return defaultCfg
	}
	cfg := mboxes[mailBox]
	if cfg == nil {
		return defaultCfg
	}
	if cfg["unseen_count"] == nil {
		cfg["unseen_count"] = &criteriaCfg{}
	}
	return cfg
}

func init() {
	log.SetFlags(0)

	must(initPaths())
}

func dieOnNetworkTimeout(v ...interface{}) {
	for _, it := range v {
		err, ok := it.(*net.OpError)
		if ok && err.Timeout() {
			dieIf(err)
		}
	}
}

type nwTimeoutFatalLogger struct{}

func (l *nwTimeoutFatalLogger) Printf(format string, v ...interface{}) {
	dieOnNetworkTimeout(v...)
	log.Printf(format, v...)
}

func (l *nwTimeoutFatalLogger) Println(v ...interface{}) {
	dieOnNetworkTimeout(v...)
	log.Println(v...)
}

func initPaths() error {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	appHomeDir = filepath.Join(homeDir, "."+appName)
	cacheDir = filepath.Join(appHomeDir, "cache")

	for _, dir := range []string{appHomeDir, cacheDir} {
		if err := os.MkdirAll(dir, defaultDirPerms); err != nil {
			return err
		}
	}
	return nil
}

func dialAndLogin(passwd string) (*client.Client, error) {
	c, err := client.DialTLS(*addrArg, nil)
	if err != nil {
		return nil, err
	}

	c.Timeout = imapTimeout

	// HACK: go-imap tries to be smart and handle timeouts itself.
	// Wich does not work well for cli usecase.
	// However it reports such erros to custom logger. This logger simply
	// aborts on network timeouts for now.
	c.ErrorLog = &nwTimeoutFatalLogger{}

	if err := c.Login(*userArg, passwd); err != nil {
		return nil, err
	}

	if _, err = c.Select(*mboxArg, false); err != nil {
		return nil, err
	}
	return c, nil
}

func fetchStats(cfg *config) (stats, error) {
	passwd, err := readPassword()
	if err != nil {
		return nil, err
	}
	c, err := dialAndLogin(passwd)
	if err != nil {
		return nil, err
	}
	defer c.Logout()
	st := stats{}
	// TODO: explore a possibility to run in parallel - will be useful if many stats to be collected
	for k, cr := range cfg.getStatsCfg(*userArg, *mboxArg) {
		ids, err := c.Search(cr.toIMAP())
		if err != nil {
			return nil, err
		}
		st[k] = len(ids)
	}
	return st, nil
}

func fetchConfig(path string) (*config, error) {
	var cfg config
	b, err := ioutil.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &cfg, nil
		}
		return nil, err
	}
	if err := yaml.Unmarshal(b, &cfg); err != nil {
		return nil, err
	}
	for _, acc := range cfg.Accounts {
		for _, cfg := range acc {
			for _, cr := range cfg {
				if len(cr.Or) == 1 {
					return nil, fmt.Errorf("bad config: OR criteria must have 2 clauses")
				}
			}
		}
	}
	return &cfg, nil
}

func main() {
	flag.Parse()
	if *readCacheArg {
		must(readFromCache())
		return
	}

	cfg, err := fetchConfig(filepath.Join(appHomeDir, configName))
	dieIf(err)
	st, err := fetchStats(cfg)
	dieIf(err)

	must(writeStats(st))
}

func readPassword() (string, error) {
	b, err := ioutil.ReadFile(*passwordArg)
	if err != nil {
		return "", err
	}
	res := strings.TrimSpace(string(b))
	return res, nil
}

func readFromCache() error {
	filename := cacheFilename()
	info, err := os.Stat(filename)
	if err != nil {
		return err
	}
	age := time.Now().Sub(info.ModTime())
	if cacheTTL() != ttlInfinite && age > cacheTTL() {
		// TODO: the error message can be confusing
		return fmt.Errorf("%w: too old: %s", os.ErrNotExist, filename)
	}

	f, err := os.Open(filename)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = io.Copy(os.Stdout, f)
	return err
}

func writeStats(st stats) error {
	var w io.Writer = os.Stdout
	if *writeCacheArg {
		f, err := os.Create(cacheFilename())
		if err != nil {
			return err
		}
		defer f.Close()
		if *quietArg {
			w = f
		} else {
			w = io.MultiWriter(w, f)
		}
	}
	return json.NewEncoder(w).Encode(st)
}

func cacheFilename() string {
	return filepath.Join(cacheDir, *userArg+"."+*mboxArg)
}

func dieIf(err error) {
	if err != nil {
		log.Fatal("fatal: ", err)
	}
}

func must(err error) {
	dieIf(err)
}

func cacheTTL() time.Duration {
	units := map[string]time.Duration{
		"s": time.Second,
		"m": time.Minute,
		"h": time.Hour,
	}
	val := *ttlArg
	if val == "" {
		return ttlInfinite
	}
	unit := time.Second
	for k, v := range units {
		if strings.HasSuffix(val, k) {
			unit = v
			val = strings.TrimSuffix(val, k)
			break
		}
	}
	ttl, err := strconv.ParseInt(val, 10, 64)
	if err != nil {
		return ttlInfinite
	}
	return time.Duration(ttl) * unit
}
