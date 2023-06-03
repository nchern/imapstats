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
)

const (
	appName = "imapstats"

	defaultDirPerms = 0700

	ttlInfinite time.Duration = -1

	imapTimeout = 10 * time.Second
)

var (
	addrArg = flag.String("addr", "imap.gmail.com:993", "IMAP user")

	userArg     = flag.String("user", "", "IMAP user")
	passwordArg = flag.String("pass", "", "IMAP password")

	mboxArg = flag.String("mailbox", "INBOX", "mailbox on the server")

	quietArg = flag.Bool("q", false, "If set, does not output stats on stdin. Can be used in background jobs to update cache")

	writeCacheArg = flag.Bool("write-cache", false, "if true writes to cache")
	readCacheArg  = flag.Bool("read-cache", false, "if true reads from cache")
	ttlArg        = flag.String(
		"ttl",
		"",
		"sets cache ttl. By default no ttl is set. Default unit is seconds, hours and minues are also supported e.g. 2h; 35m")

	appHomeDir string
	cacheDir   string
)

type stats struct {
	UnseenCount int `json:"unseen_count"`
}

func init() {
	log.SetFlags(0)
	flag.Parse()

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

func fetchStats() (*stats, error) {
	passwd, err := readPassword()
	if err != nil {
		return nil, err
	}
	c, err := client.DialTLS(*addrArg, nil)
	if err != nil {
		return nil, err
	}
	defer c.Logout()

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

	criteria := imap.NewSearchCriteria()
	criteria.WithoutFlags = []string{imap.SeenFlag}
	ids, err := c.Search(criteria)
	if err != nil {
		return nil, err
	}
	return &stats{UnseenCount: len(ids)}, nil
}

func main() {
	if *readCacheArg {
		must(readFromCache())
		return
	}

	st, err := fetchStats()
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

func writeStats(st *stats) error {
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
	units := map[byte]time.Duration{
		's': time.Second,
		'm': time.Minute,
		'h': time.Hour,
	}
	val := *ttlArg
	if val == "" {
		return ttlInfinite
	}
	l := len(val)
	unit := time.Second
	for k, v := range units {
		if val[l-1] == k {
			unit = v
			val = val[0 : l-1]
			break
		}
	}
	ttl, err := strconv.ParseInt(val, 10, 64)
	if err != nil {
		return ttlInfinite
	}

	return time.Duration(ttl) * unit
}
